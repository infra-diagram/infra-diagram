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

type SQSHandler struct {
	crawler *crawler.QueueCrawler
}

func NewSQSHandler(crawler *crawler.QueueCrawler) *SQSHandler {
	return &SQSHandler{crawler: crawler}
}

// HandleChange procesa CreateQueue, SetQueueAttributes, TagQueue, etc.
//
// Re-fetchea la queue desde AWS y hace upsert. Las queues no tienen
// relaciones salientes en el MVP, así que no llamamos RebuildRelations.
func (h *SQSHandler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	queueUrl, err := extractQueueUrl(event)
	if err != nil {
		return fmt.Errorf("extracting queue url: %w", err)
	}

	log.Printf("[%s] re-fetching queue: %s", event.Detail.EventName, queueUrl)

	if err := h.crawler.IngestSingleWithDriver(ctx, driver, queueUrl); err != nil {
		return fmt.Errorf("ingesting queue %s: %w", queueUrl, err)
	}
	return nil
}

// HandleDelete procesa DeleteQueue. La queue ya no existe en AWS, así que
// solo extraemos el ARN del payload y borramos del grafo.
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

// extractQueueUrl saca la URL del payload. CreateQueue la pone en
// responseElements.queueUrl, eventos de modificación en requestParameters.
func extractQueueUrl(event *events.EventBridgeMessage) (string, error) {
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			QueueURL string `json:"queueUrl"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.QueueURL != "" {
			return resp.QueueURL, nil
		}
	}

	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			QueueURL string `json:"queueUrl"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.QueueURL != "" {
			return req.QueueURL, nil
		}
	}

	return "", fmt.Errorf("could not extract queue URL from event payload")
}
