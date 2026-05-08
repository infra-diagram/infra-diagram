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

type EC2Handler struct {
	crawler *crawler.EC2Crawler
}

func NewEC2Handler(crawler *crawler.EC2Crawler) *EC2Handler {
	return &EC2Handler{crawler: crawler}
}

// HandleChange procesa RunInstances (que crea N instancias en una llamada).
//
// A diferencia de los demás servicios, RunInstances puede crear MÚLTIPLES
// instancias en un solo evento. Iteramos por todas las que vinieron.
func (h *EC2Handler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	instanceIds, err := extractInstanceIds(event)
	if err != nil {
		return fmt.Errorf("extracting instance ids: %w", err)
	}
	if len(instanceIds) == 0 {
		return fmt.Errorf("no instance ids found in event")
	}

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	for _, instanceId := range instanceIds {
		log.Printf("[%s] re-fetching instance: %s", event.Detail.EventName, instanceId)

		if err := h.crawler.IngestSingle(ctx, session, instanceId); err != nil {
			log.Printf("ingesting instance %s: %v", instanceId, err)
			continue
		}

		if err := h.crawler.RebuildRelations(ctx, session, instanceId); err != nil {
			log.Printf("rebuilding relations for %s (non-fatal): %v", instanceId, err)
		}
	}

	return nil
}

// HandleDelete procesa TerminateInstances. También puede afectar múltiples
// instancias, así que iteramos.
func (h *EC2Handler) HandleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	instanceIds, err := extractInstanceIds(event)
	if err != nil {
		return fmt.Errorf("extracting instance ids: %w", err)
	}
	if len(instanceIds) == 0 {
		return fmt.Errorf("no instance ids found in event")
	}

	for _, instanceId := range instanceIds {
		// Para TerminateInstances no tenemos el ARN directo, lo construimos.
		arn := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s",
			event.Detail.AwsRegion, event.Account, instanceId)

		log.Printf("[%s] removing instance: %s", event.Detail.EventName, arn)

		if err := h.crawler.DeleteByArn(ctx, driver, arn); err != nil {
			log.Printf("deleting instance %s: %v", arn, err)
			continue
		}
	}

	return nil
}

// extractInstanceIds saca los IDs de instancias del payload.
//
// EC2 tiene una estructura particular: instancesSet.items[].instanceId
// Tanto en RunInstances como en TerminateInstances.
func extractInstanceIds(event *events.EventBridgeMessage) ([]string, error) {
	// Para RunInstances → responseElements.instancesSet.items[].instanceId
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			InstancesSet struct {
				Items []struct {
					InstanceId string `json:"instanceId"`
				} `json:"items"`
			} `json:"instancesSet"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil {
			ids := make([]string, 0, len(resp.InstancesSet.Items))
			for _, item := range resp.InstancesSet.Items {
				if item.InstanceId != "" {
					ids = append(ids, item.InstanceId)
				}
			}
			if len(ids) > 0 {
				return ids, nil
			}
		}
	}

	// Para TerminateInstances → requestParameters.instancesSet.items[].instanceId
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			InstancesSet struct {
				Items []struct {
					InstanceId string `json:"instanceId"`
				} `json:"items"`
			} `json:"instancesSet"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil {
			ids := make([]string, 0, len(req.InstancesSet.Items))
			for _, item := range req.InstancesSet.Items {
				if item.InstanceId != "" {
					ids = append(ids, item.InstanceId)
				}
			}
			if len(ids) > 0 {
				return ids, nil
			}
		}
	}

	return nil, fmt.Errorf("could not extract instance ids from event payload")
}
