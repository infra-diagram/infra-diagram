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

type VPCHandler struct {
	crawler *crawler.VPCCrawler
}

func NewVPCHandler(crawler *crawler.VPCCrawler) *VPCHandler {
	return &VPCHandler{crawler: crawler}
}

// HandleChange procesa CreateVpc. VPCs no tienen ModifyVpc tradicional —
// se modifican vía CreateVpcAttribute, AssociateVpcCidrBlock, etc., que
// dejamos fuera del MVP. CreateVpc es lo único que registramos.
func (h *VPCHandler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	vpcId, err := extractVpcId(event)
	if err != nil {
		return fmt.Errorf("extracting vpc id: %w", err)
	}

	log.Printf("[%s] re-fetching vpc: %s", event.Detail.EventName, vpcId)

	// VPC no tiene relaciones salientes en el MVP, solo IngestSingle.
	if err := h.crawler.IngestSingleWithDriver(ctx, driver, vpcId); err != nil {
		return fmt.Errorf("ingesting vpc %s: %w", vpcId, err)
	}
	return nil
}

func (h *VPCHandler) HandleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	arn := event.Detail.PrimaryResourceARN()
	if arn == "" {
		return fmt.Errorf("DeleteVpc event has no resource ARN")
	}

	log.Printf("[%s] removing vpc: %s", event.Detail.EventName, arn)

	if err := h.crawler.DeleteByArn(ctx, driver, arn); err != nil {
		return fmt.Errorf("deleting vpc %s: %w", arn, err)
	}
	return nil
}

// extractVpcId saca el VpcId del payload.
//
// CreateVpc → responseElements.vpc.vpcId
// DeleteVpc → requestParameters.vpcId
func extractVpcId(event *events.EventBridgeMessage) (string, error) {
	// 1. responseElements.vpc.vpcId (caso CreateVpc)
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			Vpc struct {
				VpcId string `json:"vpcId"`
			} `json:"vpc"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.Vpc.VpcId != "" {
			return resp.Vpc.VpcId, nil
		}
	}

	// 2. requestParameters.vpcId (caso DeleteVpc)
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			VpcId string `json:"vpcId"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.VpcId != "" {
			return req.VpcId, nil
		}
	}

	return "", fmt.Errorf("could not extract vpc id from event payload")
}
