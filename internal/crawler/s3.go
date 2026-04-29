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

type bucketCache struct {
	buckets []s3types.Bucket
	ownerId string
}

type S3Crawler struct {
	client *s3.Client

	bucketsOnce  sync.Once
	s3BucketList bucketCache
	bucketErr    error
}

func NewS3Crawler(client *s3.Client) *S3Crawler {
	return &S3Crawler{client: client}
}

// normalizeS3Region maneja el caso especial de us-east-1, donde la API
// devuelve un string vacío en LocationConstraint en lugar del nombre real.
func normalizeS3Region(loc s3types.BucketLocationConstraint) string {
	if loc == "" {
		return "us-east-1"
	}
	return string(loc)
}

func (c *S3Crawler) loadBuckets(ctx context.Context) (bucketCache, error) {
	c.bucketsOnce.Do(func() {
		// ListBuckets no pagina (a diferencia de Lambda)
		resp, err := c.client.ListBuckets(ctx, &s3.ListBucketsInput{})

		if err != nil {
			c.bucketErr = fmt.Errorf("listing s3 buckets: %w", err)
			return
		}

		// El accountId no viene en ListBuckets — lo extraemos del Owner de la respuesta.
		// Owner.ID es el canonical ID, no el accountId numérico, así que lo dejamos vacío
		// por ahora. Si lo necesitas, se puede obtener con sts:GetCallerIdentity una vez
		// al inicio del crawler. Para el MVP lo omitimos.
		c.s3BucketList.ownerId = ""
		if resp.Owner != nil && resp.Owner.ID != nil {
			// Nota: esto es el canonical user ID, no el accountId numérico.
			// Para diagramación es suficiente como identificador único de cuenta.
			c.s3BucketList.ownerId = *resp.Owner.ID
		}

		c.s3BucketList.buckets = append(c.s3BucketList.buckets, resp.Buckets...)
	})

	return c.s3BucketList, c.bucketErr
}

func (c *S3Crawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	bucketResponse, err := c.loadBuckets(ctx)

	if err != nil {
		return err
	}

	accountId := bucketResponse.ownerId
	bucketList := bucketResponse.buckets

	for _, b := range bucketList {
		bucketName := aws.ToString(b.Name)

		// Cada bucket vive en una región específica — hay que preguntarla.
		locResp, err := c.client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
			Bucket: b.Name,
		})
		if err != nil {
			log.Printf("getting location for bucket %s: %v", bucketName, err)
			continue
		}

		region := normalizeS3Region(locResp.LocationConstraint)
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

// ingestNotifications mapea las relaciones (S3Bucket)-[:NOTIFIES]->(Lambda)
// que se configuran a nivel de bucket en S3 Event Notifications.
func (c *S3Crawler) ingestNotifications(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	bucketResponse, err := c.loadBuckets(ctx)

	if err != nil {
		return err
	}
	bucketList := bucketResponse.buckets

	for _, b := range bucketList {
		bucketName := aws.ToString(b.Name)
		bucketArn := BuildS3Arn(bucketName)

		notifResp, err := c.client.GetBucketNotificationConfiguration(ctx,
			&s3.GetBucketNotificationConfigurationInput{Bucket: b.Name},
		)
		if err != nil {
			log.Printf("getting notification config for %s: %v", bucketName, err)
			continue
		}

		// Si el bucket no tiene notificaciones configuradas, los slices vienen vacíos
		// (no nil necesariamente, pero len == 0). El range se salta solo en ese caso.
		for _, lambdaConfig := range notifResp.LambdaFunctionConfigurations {
			if lambdaConfig.LambdaFunctionArn == nil {
				continue
			}

			// Convertimos los Events del SDK ([]s3types.Event) a []string
			// porque Neo4j no entiende tipos custom del SDK.
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
