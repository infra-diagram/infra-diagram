package crawler

import (
	"context"
	"fmt"
	"infra-diagram/internal/models"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type QueueCrawler struct {
	client *sqs.Client
}

func NewQueueCrawler(client *sqs.Client) *QueueCrawler {
	return &QueueCrawler{client: client}
}

func (c *QueueCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := sqs.NewListQueuesPaginator(c.client, &sqs.ListQueuesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)

		if err != nil {
			return fmt.Errorf("Listing sqs queues %w", err)
		}

		reqAttr := []types.QueueAttributeName{types.QueueAttributeNameQueueArn}

		for _, queueUrl := range page.QueueUrls {
			res, err := c.client.GetQueueAttributes(ctx,
				&sqs.GetQueueAttributesInput{QueueUrl: &queueUrl, AttributeNames: reqAttr})

			if err != nil {
				log.Printf("getting attributes for %s: %v", queueUrl, err)
				continue
			}

			attr := res.Attributes

			arn := attr[string(types.QueueAttributeNameQueueArn)]
			accountId, err := GetAccountIdFromArn(arn)

			if err != nil {
				log.Printf("skipping %s: %v", queueUrl, err)
				continue
			}

			region, err := GetRegionFromArn(arn)

			if err != nil {
				log.Printf("skipping %s: %v", queueUrl, err)
				continue
			}

			queueName, err := GetResourceNameFromArn(arn)

			if err != nil {
				log.Printf("skipping %s: %v", queueUrl, err)
				continue
			}

			node := models.Queue{
				AWSResource: models.AWSResource{
					Arn:       arn,
					AccountId: accountId,
				},
				Name:   queueName,
				Url:    queueUrl,
				Region: region,
			}

			_, err = session.Run(ctx,
				`MERGE (f:Queue:AWSResource {arn: $arn})
				 SET f.name      = $name,
				 		 f.url    	 = $url,
				     f.region    = $region,
				     f.accountId = $accountId`,
				map[string]any{
					"arn":       node.Arn,
					"name":      node.Name,
					"url":       node.Url,
					"region":    node.Region,
					"accountId": node.AccountId,
				},
			)

			if err != nil {
				log.Printf("neo4j error for %s: %v", node.Name, err)
			}
		}
	}
	return nil
}

func (c *QueueCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	// Hay 3 relaciones que se necesitan mapear
	// TRIGGERS -> lambda (Ya lo hace el Lambda Crawler no hace falta esta implementacion)
	// BELONGS_TO -> VPC (Solo si es una cola con VPC endpoint) fuera del MVP
	// RECEIVES_FROM -> SNS (sns.ListSubscriptionsByTopic) fuera del MVP

	return nil
}
