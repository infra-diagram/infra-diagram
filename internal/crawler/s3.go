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

	// Cache de clientes S3 por región. S3 requiere endpoint regional para
	// algunas operaciones (GetBucketNotificationConfiguration, etc.) aunque
	// ListBuckets sea global.
	clientsMu sync.Mutex
	clients   map[string]*s3.Client

	// Cache de buckets + accountId — listBuckets es la primera llamada.
	bucketsOnce sync.Once
	buckets     []s3types.Bucket
	accountId   string
	bucketsErr  error

	// Cache de region por bucket — evita llamar GetBucketLocation dos veces
	// (una en IngestNodes, otra en ingestNotifications).
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

// clientForRegion devuelve un cliente S3 configurado para una región
// específica. Se crea perezosamente la primera vez que se necesita.
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

// loadBuckets lista todos los buckets de la cuenta y guarda el resultado.
// ListBuckets es global — funciona desde cualquier región.
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

// regionForBucket devuelve la región donde vive un bucket. Cachea el
// resultado para evitar llamadas duplicadas.
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
		return "", err
	}
	region := normalizeS3Region(locResp.LocationConstraint)

	c.regionsMu.Lock()
	c.regions[bucketName] = region
	c.regionsMu.Unlock()

	return region, nil
}

// normalizeS3Region maneja el caso especial de us-east-1, donde la API
// devuelve string vacío en lugar del nombre de la región.
func normalizeS3Region(loc s3types.BucketLocationConstraint) string {
	if loc == "" {
		return "us-east-1"
	}
	return string(loc)
}

func (c *S3Crawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	buckets, accountId, err := c.loadBuckets(ctx)
	if err != nil {
		return err
	}

	for _, b := range buckets {
		bucketName := aws.ToString(b.Name)

		region, err := c.regionForBucket(ctx, bucketName)
		if err != nil {
			log.Printf("getting location for bucket %s: %v", bucketName, err)
			continue
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
			log.Printf("neo4j error for bucket %s: %v", bucketName, err)
		}
	}

	return nil
}

func (c *S3Crawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	if err := c.ingestNotifications(ctx, driver); err != nil {
		return err
	}
	return nil
}

func (c *S3Crawler) ingestNotifications(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	buckets, _, err := c.loadBuckets(ctx)
	if err != nil {
		return err
	}

	for _, b := range buckets {
		bucketName := aws.ToString(b.Name)
		bucketArn := BuildS3Arn(bucketName)

		// La región ya fue cacheada en IngestNodes — esto es un lookup,
		// no una llamada de red.
		region, err := c.regionForBucket(ctx, bucketName)
		if err != nil {
			log.Printf("getting region for %s: %v", bucketName, err)
			continue
		}

		// Cliente regional para la operación que requiere endpoint específico.
		regionalClient := c.clientForRegion(region)
		notifResp, err := regionalClient.GetBucketNotificationConfiguration(ctx,
			&s3.GetBucketNotificationConfigurationInput{Bucket: b.Name},
		)
		if err != nil {
			log.Printf("getting notification config for %s (%s): %v", bucketName, region, err)
			continue
		}

		for _, lambdaConfig := range notifResp.LambdaFunctionConfigurations {
			if lambdaConfig.LambdaFunctionArn == nil {
				continue
			}

			events := make([]string, 0, len(lambdaConfig.Events))
			for _, e := range lambdaConfig.Events {
				events = append(events, string(e))
			}

			rel := models.NotifiesRelation{
				SourceArn: bucketArn,
				TargetArn: *lambdaConfig.LambdaFunctionArn,
				Events:    events,
			}

			_, err := session.Run(ctx,
				`MATCH (src:S3Bucket:AWSResource {arn: $sourceArn})
				 MATCH (fn:Function:AWSResource {arn: $targetArn})
				 MERGE (src)-[:NOTIFIES {events: $events}]->(fn)`,
				map[string]any{
					"sourceArn": rel.SourceArn,
					"targetArn": rel.TargetArn,
					"events":    rel.Events,
				},
			)
			if err != nil {
				log.Printf("NOTIFIES relation error for %s -> %s: %v",
					bucketName, *lambdaConfig.LambdaFunctionArn, err)
			}
		}
	}

	return nil
}
