package insights

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/queries"
)

// OrphansInsight detecta recursos sin la conectividad mínima esperada.
//
// Esto no es un bug del grafo — es una señal de potencial waste, configuración
// olvidada, o recursos abandonados que deberían eliminarse.
type OrphansInsight struct{}

func NewOrphansInsight() *OrphansInsight {
	return &OrphansInsight{}
}

func (o *OrphansInsight) ID() string {
	return "structural.orphans"
}

func (o *OrphansInsight) Version() string {
	return "1.0.0"
}

func (o *OrphansInsight) Metadata() inspector.Metadata {
	return inspector.Metadata{
		Title:       "Orphaned resources",
		Description: "Resources without the expected minimum connectivity: queues with no triggers, uninvoked lambdas, unused roles, empty subnets.",
		Category:    "structural",
		Severity:    inspector.SeverityLow,
		Parameters:  nil, // sin parámetros específicos
	}
}

// orphanCheck encapsula una de las queries de huérfanos.
// Cada tipo de recurso (Queue, Lambda, Role, Subnet) es un check.
type orphanCheck struct {
	query        string
	resourceType string
	title        string
	description  string
	remediation  string
	severity     inspector.Severity
}

func (o *OrphansInsight) Execute(
	ctx context.Context,
	session neo4j.SessionWithContext,
	filters inspector.StandardFilters,
	params map[string]any,
) (*inspector.Result, error) {
	checks := []orphanCheck{
		{
			query:        queries.OrphansQueue,
			resourceType: "Queue",
			title:        "Queue with no producers",
			description:  "This SQS queue does not receive messages from any known resource. It may be legacy configuration or a forgotten resource.",
			remediation:  "Check whether the queue is in use outside the graph scope (e.g. external producers). If not, consider deleting it.",
			severity:     inspector.SeverityLow,
		},
		{
			query:        queries.OrphansLambda,
			resourceType: "Function",
			title:        "Lambda with unknown invokers",
			description:  "This Lambda is not invoked by any known resource in the graph (event source mapping, S3 notification, etc.).",
			remediation:  "Check whether the Lambda is invoked via API Gateway, EventBridge schedule, or another unmodeled mechanism. If unused, consider deleting it to reduce cost and attack surface.",
			severity:     inspector.SeverityMedium,
		},
		{
			query:        queries.OrphansRole,
			resourceType: "IAMRole",
			title:        "IAM role with unknown usage",
			description:  "This role is not assigned to any EC2 instance or Lambda function in the graph.",
			remediation:  "If the role was created for a deleted resource, it should be removed. Unused roles are a latent security risk.",
			severity:     inspector.SeverityInfo,
		},
		{
			query:        queries.OrphansSubnet,
			resourceType: "Subnet",
			title:        "Empty subnet",
			description:  "This subnet has no known resources assigned (EC2 instances, Lambdas).",
			remediation:  "If the subnet will not be used, consider deleting it. If it hosts unmodeled resources (RDS, ElastiCache), ignore this finding.",
			severity:     inspector.SeverityInfo,
		},
	}

	result := &inspector.Result{
		Findings: []inspector.Finding{},
		Subgraph: inspector.Subgraph{
			Nodes: []inspector.GraphNode{},
			Edges: []inspector.GraphEdge{}, // orphans no tienen edges por definición
		},
	}

	cypherParams := map[string]any{
		"accountId": filters.AccountID,
		"region":    filters.Region,
	}

	for _, check := range checks {
		// Saltar si el filtro excluye este tipo de recurso
		if filters.ResourceType != "" && filters.ResourceType != check.resourceType {
			continue
		}

		findings, nodes, err := o.runCheck(ctx, session, check, cypherParams)
		if err != nil {
			return nil, fmt.Errorf("checking orphan %s: %w", check.resourceType, err)
		}
		result.Findings = append(result.Findings, findings...)
		result.Subgraph.Nodes = append(result.Subgraph.Nodes, nodes...)
	}

	return result, nil
}

// runCheck ejecuta una query de huérfanos y construye findings + nodos.
func (o *OrphansInsight) runCheck(
	ctx context.Context,
	session neo4j.SessionWithContext,
	check orphanCheck,
	cypherParams map[string]any,
) ([]inspector.Finding, []inspector.GraphNode, error) {
	cypherResult, err := session.Run(ctx, check.query, cypherParams)
	if err != nil {
		return nil, nil, fmt.Errorf("running query: %w", err)
	}

	findings := []inspector.Finding{}
	nodes := []inspector.GraphNode{}

	for cypherResult.Next(ctx) {
		record := cypherResult.Record()

		arn, _ := record.Get("arn")
		arnStr, _ := arn.(string)
		if arnStr == "" {
			continue
		}

		name, _ := record.Get("name")
		nameStr, _ := name.(string)

		region, _ := record.Get("region")
		regionStr, _ := region.(string)

		accountId, _ := record.Get("accountId")
		accountIdStr, _ := accountId.(string)

		properties, _ := record.Get("properties")
		propMap, _ := properties.(map[string]any)

		resourceRef := inspector.ResourceRef{
			ARN:    arnStr,
			Type:   check.resourceType,
			Name:   nameStr,
			Region: regionStr,
		}

		finding := inspector.Finding{
			ID:          fmt.Sprintf("%s/%s", "structural.orphans", arnStr),
			Resource:    resourceRef,
			Severity:    check.severity,
			Title:       check.title,
			Description: check.description,
			Remediation: check.remediation,
			Evidence: map[string]any{
				"accountId": accountIdStr,
			},
		}
		findings = append(findings, finding)

		nodes = append(nodes, inspector.GraphNode{
			ARN:        arnStr,
			Labels:     []string{check.resourceType, "AWSResource"},
			Properties: propMap,
		})
	}

	if err := cypherResult.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating results: %w", err)
	}

	return findings, nodes, nil
}
