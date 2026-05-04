package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/crawler"
	"infra-diagram/internal/events"
)

// SQSHandler maneja eventos de CloudTrail relacionados con SQS.
//
// Una sola struct con dos métodos (Change y Delete) en lugar de dos handlers
// independientes — comparten el mismo crawler subyacente y va junto en el
// mismo archivo.
type SQSHandler struct {
	crawler *crawler.QueueCrawler
}

func NewSQSHandler(crawler *crawler.QueueCrawler) *SQSHandler {
	return &SQSHandler{crawler: crawler}
}

// HandleChange procesa eventos que crean o modifican una queue (CreateQueue,
// SetQueueAttributes, TagQueue, etc.). Re-fetchea el estado actual de la
// queue desde AWS y hace upsert en Neo4j.
//
// El payload de CloudTrail nos da la URL de la queue — todo lo demás
// (atributos, ARN) lo obtenemos del re-fetch via QueueCrawler.IngestSingle.
func (h *SQSHandler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	queueUrl, err := extractQueueUrl(event)
	if err != nil {
		return fmt.Errorf("extracting queue url from %s event: %w", event.Detail.EventName, err)
	}

	log.Printf("[%s] re-fetching queue: %s", event.Detail.EventName, queueUrl)

	if err := h.crawler.IngestSingleWithDriver(ctx, driver, queueUrl); err != nil {
		return fmt.Errorf("ingesting queue %s: %w", queueUrl, err)
	}

	return nil
}

// HandleDelete procesa el evento DeleteQueue. Al contrario de HandleChange,
// NO podemos hacer re-fetch — la queue ya no existe en AWS. Tomamos el ARN
// del payload del evento y removemos el nodo del grafo.
func (h *SQSHandler) HandleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	arn := event.Detail.PrimaryResourceARN()
	if arn == "" {
		return fmt.Errorf("DeleteQueue event has no resource ARN")
	}

	log.Printf("[%s] removing queue: %s", event.Detail.EventName, arn)

	if err := h.crawler.DeleteByArn(ctx, driver, arn); err != nil {
		return fmt.Errorf("deleting queue %s: %w", arn, err)
	}

	return nil
}

// extractQueueUrl saca la URL de la queue del payload del evento.
//
// Para CreateQueue, viene en responseElements.queueUrl.
// Para SetQueueAttributes/TagQueue/etc., viene en requestParameters.queueUrl.
//
// Como los dos campos tienen forma distinta segun el evento, intentamos
// ambos lugares en orden.
func extractQueueUrl(event *events.EventBridgeMessage) (string, error) {
	// 1. Intentar responseElements.queueUrl (caso CreateQueue)
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			QueueURL string `json:"queueUrl"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.QueueURL != "" {
			return resp.QueueURL, nil
		}
	}

	// 2. Intentar requestParameters.queueUrl (caso SetQueueAttributes, etc.)
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			QueueURL string `json:"queueUrl"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.QueueURL != "" {
			return req.QueueURL, nil
		}
	}

	// 3. Fallback: construir desde el ARN si está disponible
	arn := event.Detail.PrimaryResourceARN()
	if arn != "" {
		// arn:aws:sqs:us-east-1:110727845776:queue-name
		// → https://sqs.us-east-1.amazonaws.com/110727845776/queue-name
		// Esto es resistente pero no lo necesitamos en práctica con los eventos
		// del MVP. Lo dejamos como TODO si algún día encontramos un evento que
		// no tenga ni request ni response elements.
	}

	return "", fmt.Errorf("could not extract queue URL from event payload")
}
