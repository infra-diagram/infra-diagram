package crawler

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/models"
)

type S3Crawler struct {
	baseCfg aws.Config

	clientsMu sync.Mutex
	clients   map[string]*s3.Client

	bucketsOnce sync.Once
	buckets     []s3types.Bucket
	accountId   string
	bucketsErr  error

	regionsMu sync.Mutex
	regions   map[string]string
}

func NewS3Crawler(cfg aws.Config) *S3Crawler {
	return &S3Crawler{
		baseCfg: cfg,
		clients: make(map[string]*s3.Client),
		regions: make(map[string]string),
	}
}

func (c *S3Crawler) clientForRegion(region string) *s3.Client {
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()

	if cli, ok := c.clients[region]; ok {
		return cli
	}
	cli := s3.NewFromConfig(c.baseCfg, func(o *s3.Options) {
		o.Region = region
	})
	c.clients[region] = cli
	return cli
}

func (c *S3Crawler) loadBuckets(ctx context.Context) ([]s3types.Bucket, string, error) {
	c.bucketsOnce.Do(func() {
		client := c.clientForRegion(c.baseCfg.Region)
		resp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			c.bucketsErr = fmt.Errorf("listing s3 buckets: %w", err)
			return
		}
		c.buckets = resp.Buckets
		if resp.Owner != nil && resp.Owner.ID != nil {
			c.accountId = *resp.Owner.ID
		}
	})
	return c.buckets, c.accountId, c.bucketsErr
}

func (c *S3Crawler) regionForBucket(ctx context.Context, bucketName string) (string, error) {
	c.regionsMu.Lock()
	if region, ok := c.regions[bucketName]; ok {
		c.regionsMu.Unlock()
		return region, nil
	}
	c.regionsMu.Unlock()

	client := c.clientForRegion(c.baseCfg.Region)
	locResp, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: &bucketName,
	})
	if err != nil {
		return "", fmt.Errorf("getting location for %s: %w", bucketName, err)
	}
	region := normalizeS3Region(locResp.LocationConstraint)

	c.regionsMu.Lock()
	c.regions[bucketName] = region
	c.regionsMu.Unlock()

	return region, nil
}

func normalizeS3Region(loc s3types.BucketLocationConstraint) string {
	if loc == "" {
		return "us-east-1"
	}
	return string(loc)
}

// =============================================================================
// FASE 1 — Nodos
// =============================================================================

func (c *S3Crawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	buckets, accountId, err := c.loadBuckets(ctx)
	if err != nil {
		return err
	}

	for _, b := range buckets {
		bucketName := aws.ToString(b.Name)
		if err := c.upsertNodeFromBucket(ctx, session, bucketName, accountId); err != nil {
			log.Printf("ingesting bucket: %v", err)
			continue
		}
	}
	return nil
}

// IngestSingle ingiere SOLO el nodo de un bucket por su nombre.
// No toca relaciones — para eso usa RebuildRelations.
func (c *S3Crawler) IngestSingle(ctx context.Context, session neo4j.SessionWithContext, bucketName string) error {
	// Para single, el accountId no lo tenemos cacheado garantizado, así que
	// lo obtenemos de loadBuckets si está disponible. Si no, va vacío y se
	// llenará en el siguiente discovery.
	_, accountId, err := c.loadBuckets(ctx)
	if err != nil {
		// No falla — podemos ingestar sin accountId
		log.Printf("could not load accountId for bucket %s: %v", bucketName, err)
	}
	return c.upsertNodeFromBucket(ctx, session, bucketName, accountId)
}

func (c *S3Crawler) IngestSingleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, bucketName string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSingle(ctx, session, bucketName)
}

// upsertNodeFromBucket hace solo el MERGE del nodo. Comparte lógica entre
// IngestNodes (bulk) e IngestSingle (puntual).
func (c *S3Crawler) upsertNodeFromBucket(ctx context.Context, session neo4j.SessionWithContext, bucketName, accountId string) error {
	region, err := c.regionForBucket(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("resolving region for %s: %w", bucketName, err)
	}

	arn := BuildS3Arn(bucketName)

	node := models.S3Bucket{
		AWSResource: models.AWSResource{
			Arn:       arn,
			AccountId: accountId,
		},
		Name:   bucketName,
		Region: region,
	}

	_, err = session.Run(ctx,
		`MERGE (b:S3Bucket:AWSResource {arn: $arn})
		 SET b.name      = $name,
		     b.region    = $region,
		     b.accountId = $accountId`,
		map[string]any{
			"arn":       node.Arn,
			"name":      node.Name,
			"region":    node.Region,
			"accountId": node.AccountId,
		},
	)
	if err != nil {
		return fmt.Errorf("merging bucket %s: %w", bucketName, err)
	}
	return nil
}

// =============================================================================
// FASE 2 — Relaciones
// =============================================================================

func (c *S3Crawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	buckets, _, err := c.loadBuckets(ctx)
	if err != nil {
		return err
	}

	for _, b := range buckets {
		bucketName := aws.ToString(b.Name)
		if err := c.rebuildNotificationsFromBucket(ctx, session, bucketName); err != nil {
			log.Printf("rebuilding s3 notifications for %s: %v", bucketName, err)
			continue
		}
	}
	return nil
}

// RebuildRelations recrea TODAS las notificaciones salientes del bucket.
// PutBucketNotificationConfiguration reemplaza la config completa, así que
// el rebuild puntual es correcto: borrar todas las viejas, crear las nuevas.
func (c *S3Crawler) RebuildRelations(ctx context.Context, session neo4j.SessionWithContext, bucketName string) error {
	return c.rebuildNotificationsFromBucket(ctx, session, bucketName)
}

func (c *S3Crawler) RebuildRelationsWithDriver(ctx context.Context, driver neo4j.DriverWithContext, bucketName string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.RebuildRelations(ctx, session, bucketName)
}

func (c *S3Crawler) rebuildNotificationsFromBucket(ctx context.Context, session neo4j.SessionWithContext, bucketName string) error {
	bucketArn := BuildS3Arn(bucketName)

	region, err := c.regionForBucket(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("resolving region: %w", err)
	}

	regionalClient := c.clientForRegion(region)
	notifResp, err := regionalClient.GetBucketNotificationConfiguration(ctx,
		&s3.GetBucketNotificationConfigurationInput{Bucket: &bucketName},
	)
	if err != nil {
		return fmt.Errorf("getting notification config: %w", err)
	}

	// Borrar todas las relaciones NOTIFIES previas del bucket — independiente
	// de los targets. Esto es correcto porque PutBucketNotificationConfiguration
	// reemplaza el set completo.
	_, err = session.Run(ctx,
		`MATCH (b:S3Bucket:AWSResource {arn: $arn})-[r:NOTIFIES]->()
		 DELETE r`,
		map[string]any{"arn": bucketArn},
	)
	if err != nil {
		return fmt.Errorf("clearing old NOTIFIES: %w", err)
	}

	// Crear las nuevas (solo Lambda en MVP — Queue/SNS quedan en backlog).
	for _, lambdaConfig := range notifResp.LambdaFunctionConfigurations {
		if lambdaConfig.LambdaFunctionArn == nil {
			continue
		}

		events := make([]string, 0, len(lambdaConfig.Events))
		for _, e := range lambdaConfig.Events {
			events = append(events, string(e))
		}

		_, err := session.Run(ctx,
			`MATCH (src:S3Bucket:AWSResource {arn: $sourceArn})
			 MATCH (fn:Function:AWSResource {arn: $targetArn})
			 MERGE (src)-[:NOTIFIES {events: $events}]->(fn)`,
			map[string]any{
				"sourceArn": bucketArn,
				"targetArn": *lambdaConfig.LambdaFunctionArn,
				"events":    events,
			},
		)
		if err != nil {
			log.Printf("NOTIFIES relation error %s -> %s: %v",
				bucketName, *lambdaConfig.LambdaFunctionArn, err)
		}
	}

	return nil
}

// =============================================================================
// DELETE
// =============================================================================

func (c *S3Crawler) DeleteByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (b:S3Bucket:AWSResource {arn: $arn})
		 DETACH DELETE b`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting bucket %s: %w", arn, err)
	}
	return nil
}
