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

type IAMHandler struct {
	crawler *crawler.IAMCrawler
}

func NewIAMHandler(c *crawler.IAMCrawler) *IAMHandler {
	return &IAMHandler{crawler: c}
}

// HandleRoleChange procesa CreateRole, UpdateRole, UpdateAssumeRolePolicy,
// AttachRolePolicy, DetachRolePolicy, PutRolePolicy.
func (h *IAMHandler) HandleRoleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	roleName, err := extractRoleName(event)
	if err != nil {
		return fmt.Errorf("extracting role name: %w", err)
	}

	log.Printf("[%s] re-fetching iam role: %s", event.Detail.EventName, roleName)

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	if err := h.crawler.IngestSingleRole(ctx, session, roleName); err != nil {
		return fmt.Errorf("ingesting iam role %s: %w", roleName, err)
	}

	if err := h.crawler.RebuildRelations(ctx, session, roleName); err != nil {
		log.Printf("rebuilding relations for role %s (non-fatal): %v", roleName, err)
	}

	return nil
}

// HandleRoleDelete procesa DeleteRole.
// El ARN se construye desde requestParameters.roleName + event.Account
// porque DeleteRole en CloudTrail no siempre puebla resources[].
func (h *IAMHandler) HandleRoleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	var roleName string
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			RoleName string `json:"roleName"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil {
			roleName = req.RoleName
		}
	}
	if roleName == "" {
		return fmt.Errorf("DeleteRole event has no roleName in requestParameters")
	}

	arn := fmt.Sprintf("arn:aws:iam::%s:role/%s", event.Account, roleName)
	log.Printf("[%s] removing iam role: %s", event.Detail.EventName, arn)

	if err := h.crawler.DeleteRoleByArn(ctx, driver, arn); err != nil {
		return fmt.Errorf("deleting iam role %s: %w", arn, err)
	}
	return nil
}

// HandlePolicyChange procesa CreatePolicy, CreatePolicyVersion, SetDefaultPolicyVersion.
// No llama RebuildRelations — ATTACHED_POLICY va del Role, no de la Policy.
func (h *IAMHandler) HandlePolicyChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	policyArn, err := extractPolicyArn(event)
	if err != nil {
		return fmt.Errorf("extracting policy arn: %w", err)
	}

	log.Printf("[%s] re-fetching iam policy: %s", event.Detail.EventName, policyArn)

	if err := h.crawler.IngestSinglePolicyWithDriver(ctx, driver, policyArn); err != nil {
		return fmt.Errorf("ingesting iam policy %s: %w", policyArn, err)
	}
	return nil
}

// HandlePolicyDelete procesa DeletePolicy.
// El ARN viene directo en requestParameters.policyArn.
func (h *IAMHandler) HandlePolicyDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	var policyArn string
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			PolicyArn string `json:"policyArn"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil {
			policyArn = req.PolicyArn
		}
	}
	if policyArn == "" {
		return fmt.Errorf("DeletePolicy event has no policyArn in requestParameters")
	}

	log.Printf("[%s] removing iam policy: %s", event.Detail.EventName, policyArn)

	if err := h.crawler.DeletePolicyByArn(ctx, driver, policyArn); err != nil {
		return fmt.Errorf("deleting iam policy %s: %w", policyArn, err)
	}
	return nil
}

// extractRoleName extrae el nombre del rol del payload.
// Estrategia: requestParameters.roleName primero (Update/Attach/Detach);
// si vacío, responseElements.role.roleName (Create).
func extractRoleName(event *events.EventBridgeMessage) (string, error) {
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			RoleName string `json:"roleName"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.RoleName != "" {
			return req.RoleName, nil
		}
	}

	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			Role struct {
				RoleName string `json:"roleName"`
			} `json:"role"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.Role.RoleName != "" {
			return resp.Role.RoleName, nil
		}
	}

	return "", fmt.Errorf("could not extract role name from event payload")
}

// extractPolicyArn extrae el ARN de la policy del payload.
// Estrategia: responseElements.policy.arn primero (Create);
// si vacío, requestParameters.policyArn (Update/SetDefault).
func extractPolicyArn(event *events.EventBridgeMessage) (string, error) {
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			Policy struct {
				Arn string `json:"arn"`
			} `json:"policy"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.Policy.Arn != "" {
			return resp.Policy.Arn, nil
		}
	}

	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			PolicyArn string `json:"policyArn"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.PolicyArn != "" {
			return req.PolicyArn, nil
		}
	}

	return "", fmt.Errorf("could not extract policy ARN from event payload")
}
