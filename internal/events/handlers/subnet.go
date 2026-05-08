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

type SubnetHandler struct {
	crawler *crawler.SubnetCrawler
}

func NewSubnetHandler(crawler *crawler.SubnetCrawler) *SubnetHandler {
	return &SubnetHandler{crawler: crawler}
}

func (h *SubnetHandler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	subnetId, err := extractSubnetId(event)
	if err != nil {
		return fmt.Errorf("extracting subnet id: %w", err)
	}

	log.Printf("[%s] re-fetching subnet: %s", event.Detail.EventName, subnetId)

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	if err := h.crawler.IngestSingle(ctx, session, subnetId); err != nil {
		return fmt.Errorf("ingesting subnet %s: %w", subnetId, err)
	}

	if err := h.crawler.RebuildRelations(ctx, session, subnetId); err != nil {
		log.Printf("rebuilding relations for %s (non-fatal): %v", subnetId, err)
	}

	return nil
}

func (h *SubnetHandler) HandleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	arn := event.Detail.PrimaryResourceARN()
	if arn == "" {
		return fmt.Errorf("DeleteSubnet event has no resource ARN")
	}

	log.Printf("[%s] removing subnet: %s", event.Detail.EventName, arn)

	if err := h.crawler.DeleteByArn(ctx, driver, arn); err != nil {
		return fmt.Errorf("deleting subnet %s: %w", arn, err)
	}
	return nil
}

// extractSubnetId saca el SubnetId del payload.
//
// CreateSubnet → responseElements.subnet.subnetId
// DeleteSubnet → requestParameters.subnetId
func extractSubnetId(event *events.EventBridgeMessage) (string, error) {
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			Subnet struct {
				SubnetId string `json:"subnetId"`
			} `json:"subnet"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.Subnet.SubnetId != "" {
			return resp.Subnet.SubnetId, nil
		}
	}

	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			SubnetId string `json:"subnetId"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.SubnetId != "" {
			return req.SubnetId, nil
		}
	}

	return "", fmt.Errorf("could not extract subnet id from event payload")
}
