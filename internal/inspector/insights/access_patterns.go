package insights

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/cypherutil"
	"infra-diagram/internal/inspector/queries"
)

// Default thresholds. Calibrated based on typical AWS accounts:
//   - Roles with 10+ distinct accessible resources usually indicate
//     wildcards in their policies (e.g. s3:GetObject on multiple buckets)
//   - Resources accessed by 5+ roles often signal shared infrastructure
//     (logs, audit buckets) — sometimes intentional, sometimes leaks
const (
	defaultOverprivilegedThreshold = 10
	defaultOversharedThreshold     = 5
)

// AccessPatternsInsight reports two complementary problems detectable
// only when CAN_ACCESS relations are materialized in the graph:
//
//  1. Overprivileged principals: roles that can access many distinct resources
//  2. Overshared resources: resources accessible from many distinct roles
//
// Both signal potential policy bloat or unintended access exposure.
type AccessPatternsInsight struct{}

func NewAccessPatternsInsight() *AccessPatternsInsight {
	return &AccessPatternsInsight{}
}

func (a *AccessPatternsInsight) ID() string {
	return "data.access_patterns"
}

func (a *AccessPatternsInsight) Version() string {
	return "1.0.0"
}

func (a *AccessPatternsInsight) Metadata() inspector.Metadata {
	return inspector.Metadata{
		Title:       "Data access patterns",
		Description: "Detects roles with access to many resources (overprivileged) and resources accessed by many roles (overshared). Uses materialized CAN_ACCESS relations.",
		Category:    "data",
		Severity:    inspector.SeverityMedium,
		Parameters: []inspector.ParameterSpec{
			{
				Name:        "overprivileged_threshold",
				Type:        "int",
				Required:    false,
				Description: fmt.Sprintf("Min distinct resources a role must access to be flagged (default %d)", defaultOverprivilegedThreshold),
			},
			{
				Name:        "overshared_threshold",
				Type:        "int",
				Required:    false,
				Description: fmt.Sprintf("Min distinct roles that must access a resource to be flagged (default %d)", defaultOversharedThreshold),
			},
		},
	}
}

func (a *AccessPatternsInsight) Execute(
	ctx context.Context,
	session neo4j.SessionWithContext,
	filters inspector.StandardFilters,
	params map[string]any,
) (*inspector.Result, error) {
	overprivThreshold := intParam(params, "overprivileged_threshold", defaultOverprivilegedThreshold)
	oversharedThreshold := intParam(params, "overshared_threshold", defaultOversharedThreshold)

	result := &inspector.Result{
		Findings: []inspector.Finding{},
		Subgraph: inspector.Subgraph{
			Nodes: []inspector.GraphNode{},
			Edges: []inspector.GraphEdge{},
		},
	}

	seenNodes := make(map[string]bool)

	// === Pattern A: overprivileged principals ===
	if err := a.runOverprivileged(ctx, session, filters, overprivThreshold, result, seenNodes); err != nil {
		return nil, fmt.Errorf("overprivileged check: %w", err)
	}

	// === Pattern B: overshared resources ===
	if err := a.runOvershared(ctx, session, filters, oversharedThreshold, result, seenNodes); err != nil {
		return nil, fmt.Errorf("overshared check: %w", err)
	}

	return result, nil
}

func (a *AccessPatternsInsight) runOverprivileged(
	ctx context.Context,
	session neo4j.SessionWithContext,
	filters inspector.StandardFilters,
	threshold int,
	result *inspector.Result,
	seenNodes map[string]bool,
) error {
	res, err := session.Run(ctx, queries.AccessPatternsOverprivilegedQuery, map[string]any{
		"accountId": filters.AccountID,
		"threshold": threshold,
	})
	if err != nil {
		return err
	}

	for res.Next(ctx) {
		record := res.Record()
		roleArn := cypherutil.GetString(record, "roleArn")
		roleName := cypherutil.GetString(record, "roleName")
		accountId := cypherutil.GetString(record, "accountId")

		var resourceCount int64
		if v, ok := record.Get("resourceCount"); ok {
			resourceCount, _ = v.(int64)
		}

		targets := parseTargetsList(record, "targets")

		related := make([]inspector.ResourceRef, 0, len(targets))
		for _, t := range targets {
			related = append(related, inspector.ResourceRef{
				ARN:  t.arn,
				Type: cypherutil.PrimaryLabel(t.labels),
				Name: t.name,
			})
		}

		finding := inspector.Finding{
			ID: fmt.Sprintf("data.access_patterns/overprivileged/%s", roleArn),
			Resource: inspector.ResourceRef{
				ARN:  roleArn,
				Type: "IAMRole",
				Name: roleName,
			},
			Severity: severityForBreadth(int(resourceCount), threshold),
			Title:    fmt.Sprintf("Role %s can access %d distinct resources", roleName, resourceCount),
			Description: fmt.Sprintf(
				"This role has materialized CAN_ACCESS to %d distinct resources, exceeding the threshold of %d. May indicate policies with overly broad Resource specifications.",
				resourceCount, threshold,
			),
			Evidence: map[string]any{
				"pattern":        "overprivileged_principal",
				"resource_count": resourceCount,
				"threshold":      threshold,
				"account_id":     accountId,
			},
			Remediation:      "Review the policies attached to this role. Consider splitting into multiple roles with narrower resource scopes.",
			RelatedResources: related,
		}
		result.Findings = append(result.Findings, finding)

		if !seenNodes[roleArn] {
			result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
				ARN:    roleArn,
				Labels: []string{"IAMRole", "AWSResource"},
				Properties: map[string]any{
					"roleName": roleName,
				},
			})
			seenNodes[roleArn] = true
		}
		for _, t := range targets {
			if !seenNodes[t.arn] {
				result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
					ARN:    t.arn,
					Labels: t.labels,
					Properties: map[string]any{
						"name": t.name,
					},
				})
				seenNodes[t.arn] = true
			}
			result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
				FromARN: roleArn,
				ToARN:   t.arn,
				Type:    "CAN_ACCESS",
				Properties: map[string]any{
					"action": t.action,
				},
			})
		}
	}
	return res.Err()
}

func (a *AccessPatternsInsight) runOvershared(
	ctx context.Context,
	session neo4j.SessionWithContext,
	filters inspector.StandardFilters,
	threshold int,
	result *inspector.Result,
	seenNodes map[string]bool,
) error {
	res, err := session.Run(ctx, queries.AccessPatternsOversharedQuery, map[string]any{
		"accountId": filters.AccountID,
		"threshold": threshold,
	})
	if err != nil {
		return err
	}

	for res.Next(ctx) {
		record := res.Record()
		targetArn := cypherutil.GetString(record, "targetArn")
		targetName := cypherutil.GetString(record, "targetName")
		targetRegion := cypherutil.GetString(record, "targetRegion")
		targetLabels := cypherutil.GetStringSlice(record, "targetLabels")
		accountId := cypherutil.GetString(record, "accountId")

		var roleCount int64
		if v, ok := record.Get("roleCount"); ok {
			roleCount, _ = v.(int64)
		}

		accessingRoles := parseAccessingRolesList(record, "accessingRoles")
		targetType := cypherutil.PrimaryLabel(targetLabels)

		related := make([]inspector.ResourceRef, 0, len(accessingRoles))
		for _, ar := range accessingRoles {
			related = append(related, inspector.ResourceRef{
				ARN:  ar.arn,
				Type: "IAMRole",
				Name: ar.roleName,
			})
		}

		finding := inspector.Finding{
			ID: fmt.Sprintf("data.access_patterns/overshared/%s", targetArn),
			Resource: inspector.ResourceRef{
				ARN:    targetArn,
				Type:   targetType,
				Name:   targetName,
				Region: targetRegion,
			},
			Severity: severityForBreadth(int(roleCount), threshold),
			Title:    fmt.Sprintf("%s %s is accessible by %d distinct roles", targetType, targetName, roleCount),
			Description: fmt.Sprintf(
				"This resource has %d distinct roles with CAN_ACCESS to it, exceeding the threshold of %d. May indicate over-shared infrastructure (legitimate for log buckets, risky for data stores).",
				roleCount, threshold,
			),
			Evidence: map[string]any{
				"pattern":    "overshared_resource",
				"role_count": roleCount,
				"threshold":  threshold,
				"account_id": accountId,
			},
			Remediation:      "Audit the accessing roles to verify each one legitimately needs access. Consider whether this resource should be isolated.",
			RelatedResources: related,
		}
		result.Findings = append(result.Findings, finding)

		if !seenNodes[targetArn] {
			result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
				ARN:    targetArn,
				Labels: targetLabels,
				Properties: map[string]any{
					"name":   targetName,
					"region": targetRegion,
				},
			})
			seenNodes[targetArn] = true
		}
		for _, ar := range accessingRoles {
			if !seenNodes[ar.arn] {
				result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
					ARN:    ar.arn,
					Labels: []string{"IAMRole", "AWSResource"},
					Properties: map[string]any{
						"roleName": ar.roleName,
					},
				})
				seenNodes[ar.arn] = true
			}
			result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
				FromARN: ar.arn,
				ToARN:   targetArn,
				Type:    "CAN_ACCESS",
				Properties: map[string]any{
					"action": ar.action,
				},
			})
		}
	}
	return res.Err()
}

// severityForBreadth scales severity based on how far above threshold the count is.
func severityForBreadth(count, threshold int) inspector.Severity {
	ratio := float64(count) / float64(threshold)
	switch {
	case ratio >= 3.0:
		return inspector.SeverityHigh
	case ratio >= 2.0:
		return inspector.SeverityMedium
	default:
		return inspector.SeverityLow
	}
}

// === Local types and helpers ===

type accessTarget struct {
	arn    string
	labels []string
	name   string
	action string
}

type accessingRole struct {
	arn      string
	roleName string
	action   string
}

func parseTargetsList(record *neo4j.Record, key string) []accessTarget {
	val, ok := record.Get(key)
	if !ok {
		return nil
	}
	items, ok := val.([]any)
	if !ok {
		return nil
	}
	out := make([]accessTarget, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		arnStr, _ := m["arn"].(string)
		if arnStr == "" {
			continue
		}
		nameStr, _ := m["name"].(string)
		actionStr, _ := m["action"].(string)
		var labels []string
		if rawLabels, ok := m["labels"].([]any); ok {
			for _, l := range rawLabels {
				if s, ok := l.(string); ok {
					labels = append(labels, s)
				}
			}
		}
		out = append(out, accessTarget{
			arn:    arnStr,
			labels: labels,
			name:   nameStr,
			action: actionStr,
		})
	}
	return out
}

func parseAccessingRolesList(record *neo4j.Record, key string) []accessingRole {
	val, ok := record.Get(key)
	if !ok {
		return nil
	}
	items, ok := val.([]any)
	if !ok {
		return nil
	}
	out := make([]accessingRole, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		arnStr, _ := m["arn"].(string)
		if arnStr == "" {
			continue
		}
		nameStr, _ := m["roleName"].(string)
		actionStr, _ := m["action"].(string)
		out = append(out, accessingRole{
			arn:      arnStr,
			roleName: nameStr,
			action:   actionStr,
		})
	}
	return out
}

func intParam(params map[string]any, key string, defaultValue int) int {
	if params == nil {
		return defaultValue
	}
	val, ok := params[key]
	if !ok {
		return defaultValue
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}
