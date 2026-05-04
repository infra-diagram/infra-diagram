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

// Client expone el cliente SQS subyacente. Útil para handlers del worker
// que necesitan hacer llamadas adicionales a SQS más allá de IngestSingle.
func (c *QueueCrawler) Client() *sqs.Client {
	return c.client
}

func (c *QueueCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := sqs.NewListQueuesPaginator(c.client, &sqs.ListQueuesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing sqs queues: %w", err)
		}

		for _, queueUrl := range page.QueueUrls {
			if err := c.IngestSingle(ctx, session, queueUrl); err != nil {
				log.Printf("ingesting queue %s: %v", queueUrl, err)
				continue
			}
		}
	}
	return nil
}

// IngestSingle ingiere una sola queue por su URL.
//
// Usada por:
//   - IngestNodes (en bulk, dentro del paginador)
//   - El worker (puntual, al recibir un evento CreateQueue/SetQueueAttributes)
//
// Recibe una sesión activa (no el driver) porque el caller normalmente ya
// tiene una sesión abierta — evitamos crear una nueva por cada queue cuando
// llamamos esto en bulk.
func (c *QueueCrawler) IngestSingle(ctx context.Context, session neo4j.SessionWithContext, queueUrl string) error {
	res, err := c.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       &queueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return fmt.Errorf("getting attributes for %s: %w", queueUrl, err)
	}

	arn := res.Attributes[string(types.QueueAttributeNameQueueArn)]

	accountId, err := GetAccountIdFromArn(arn)
	if err != nil {
		return fmt.Errorf("parsing accountId from arn %s: %w", arn, err)
	}

	region, err := GetRegionFromArn(arn)
	if err != nil {
		return fmt.Errorf("parsing region from arn %s: %w", arn, err)
	}

	queueName, err := GetResourceNameFromArn(arn)
	if err != nil {
		return fmt.Errorf("parsing name from arn %s: %w", arn, err)
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
		`MERGE (q:Queue:AWSResource {arn: $arn})
		 SET q.name      = $name,
		     q.url       = $url,
		     q.region    = $region,
		     q.accountId = $accountId`,
		map[string]any{
			"arn":       node.Arn,
			"name":      node.Name,
			"url":       node.Url,
			"region":    node.Region,
			"accountId": node.AccountId,
		},
	)
	if err != nil {
		return fmt.Errorf("merging queue %s in neo4j: %w", queueName, err)
	}

	return nil
}

// IngestSingleWithDriver es una variante de IngestSingle que toma un driver
// y abre su propia sesión. Conveniente cuando el caller no tiene una sesión
// activa — típicamente desde el worker.
func (c *QueueCrawler) IngestSingleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, queueUrl string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	return c.IngestSingle(ctx, session, queueUrl)
}

// DeleteByArn remueve un nodo Queue del grafo junto con todas sus relaciones.
// Usado por el worker cuando llega un evento DeleteQueue.
//
// Nota: usamos DETACH DELETE para que Neo4j elimine las relaciones entrantes
// y salientes en una sola operación. Sin DETACH, Neo4j da error si el nodo
// tiene relaciones — DELETE plain solo funciona en nodos huérfanos.
func (c *QueueCrawler) DeleteByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (q:Queue:AWSResource {arn: $arn})
		 DETACH DELETE q`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting queue %s from neo4j: %w", arn, err)
	}

	return nil
}

func (c *QueueCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	// Para el MVP las relaciones de Queue las maneja LambdaCrawler
	// (Queue -[:TRIGGERS]-> Function via ListEventSourceMappings)
	return nil
}
