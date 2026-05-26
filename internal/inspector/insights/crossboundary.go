package insights

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/cypherutil"
	"infra-diagram/internal/inspector/queries"
)

// CrossBoundaryInsight detecta data flows que cruzan boundaries de VPC.
//
// Para MVP, el boundary se define por VPC: un edge TRIGGERS o NOTIFIES
// es cross-boundary si source y target están en VPCs distintas (o uno
// no tiene VPC).
//
// Future work tracked in backlog: permission-based detection, role-sharing
// detection, additional crossing types (account, region).
type CrossBoundaryInsight struct{}

func NewCrossBoundaryInsight() *CrossBoundaryInsight {
	return &CrossBoundaryInsight{}
}

func (c *CrossBoundaryInsight) ID() string {
	return "network.crossboundary"
}

func (c *CrossBoundaryInsight) Version() string {
	return "1.0.0"
}

func (c *CrossBoundaryInsight) Metadata() inspector.Metadata {
	return inspector.Metadata{
		Title:       "Cross-boundary data flows",
		Description: "Detects data flows (TRIGGERS, NOTIFIES) that cross VPC boundaries. May indicate unintended communication between isolated network zones.",
		Category:    "network",
		Severity:    inspector.SeverityMedium,
		Parameters:  nil,
	}
}

// crossingType clasifica el tipo de boundary que se cruza.
type crossingType string

const (
	crossingVPCToVPC    crossingType = "vpc_to_vpc"
	crossingVPCToPublic crossingType = "vpc_to_public"
	crossingPublicToVPC crossingType = "public_to_vpc"
)

// classifyCrossing determina el tipo de crossing basado en las VPCs de
// source y target.
func classifyCrossing(sourceVpc, targetVpc string) crossingType {
	switch {
	case sourceVpc != "" && targetVpc != "":
		// Ambos en VPCs distintas (la query ya filtró iguales)
		return crossingVPCToVPC
	case sourceVpc != "" && targetVpc == "":
		// Recurso en VPC habla con recurso público
		return crossingVPCToPublic
	default:
		// Recurso público habla con recurso en VPC
		return crossingPublicToVPC
	}
}

// severityForCrossing calibra la severidad según el tipo de crossing.
//
// VPC-to-VPC es el más relevante (boundary técnico real).
// VPC-to-public es informativo (acceso a SaaS / servicios públicos).
// Public-to-VPC es informativo (entrada desde fuentes públicas, esperado).
func severityForCrossing(ct crossingType) inspector.Severity {
	switch ct {
	case crossingVPCToVPC:
		return inspector.SeverityMedium
	case crossingVPCToPublic, crossingPublicToVPC:
		return inspector.SeverityInfo
	default:
		return inspector.SeverityLow
	}
}

func (c *CrossBoundaryInsight) Execute(
	ctx context.Context,
	session neo4j.SessionWithContext,
	filters inspector.StandardFilters,
	params map[string]any,
) (*inspector.Result, error) {
	result := &inspector.Result{
		Findings: []inspector.Finding{},
		Subgraph: inspector.Subgraph{
			Nodes: []inspector.GraphNode{},
			Edges: []inspector.GraphEdge{},
		},
	}

	cypherResult, err := session.Run(ctx, queries.CrossBoundary, map[string]any{
		"accountId": filters.AccountID,
	})
	if err != nil {
		return nil, fmt.Errorf("running query: %w", err)
	}

	// Tracking de nodos ya agregados al subgrafo para no duplicar
	seenNodes := make(map[string]bool)

	for cypherResult.Next(ctx) {
		record := cypherResult.Record()

		sourceArn := cypherutil.GetString(record, "sourceArn")
		targetArn := cypherutil.GetString(record, "targetArn")
		if sourceArn == "" || targetArn == "" {
			continue
		}

		sourceVpc := cypherutil.GetString(record, "sourceVpc")
		targetVpc := cypherutil.GetString(record, "targetVpc")
		sourceName := cypherutil.GetString(record, "sourceName")
		targetName := cypherutil.GetString(record, "targetName")
		sourceRegion := cypherutil.GetString(record, "sourceRegion")
		targetRegion := cypherutil.GetString(record, "targetRegion")
		sourceLabels := cypherutil.GetStringSlice(record, "sourceLabels")
		targetLabels := cypherutil.GetStringSlice(record, "targetLabels")
		edgeType := cypherutil.GetString(record, "edgeType")

		sourceType := cypherutil.PrimaryLabel(sourceLabels)
		targetType := cypherutil.PrimaryLabel(targetLabels)

		ct := classifyCrossing(sourceVpc, targetVpc)
		severity := severityForCrossing(ct)

		// Filtrar por resource type si aplica
		if filters.ResourceType != "" &&
			filters.ResourceType != sourceType &&
			filters.ResourceType != targetType {
			continue
		}

		finding := c.buildFinding(
			sourceArn, sourceName, sourceType, sourceVpc, sourceRegion,
			targetArn, targetName, targetType, targetVpc, targetRegion,
			edgeType, ct, severity,
		)
		result.Findings = append(result.Findings, finding)

		// Agregar nodos al subgrafo (deduplicados)
		if !seenNodes[sourceArn] {
			result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
				ARN:    sourceArn,
				Labels: sourceLabels,
				Properties: map[string]any{
					"name":   sourceName,
					"vpcId":  sourceVpc,
					"region": sourceRegion,
				},
			})
			seenNodes[sourceArn] = true
		}
		if !seenNodes[targetArn] {
			result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
				ARN:    targetArn,
				Labels: targetLabels,
				Properties: map[string]any{
					"name":   targetName,
					"vpcId":  targetVpc,
					"region": targetRegion,
				},
			})
			seenNodes[targetArn] = true
		}

		// Edge siempre nuevo (un edge problemático aparece una sola vez)
		result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
			FromARN: sourceArn,
			ToARN:   targetArn,
			Type:    edgeType,
			Properties: map[string]any{
				"crossing_type": string(ct),
			},
		})
	}

	if err := cypherResult.Err(); err != nil {
		return nil, fmt.Errorf("iterating results: %w", err)
	}

	return result, nil
}

// buildFinding construye un Finding para un edge cross-boundary específico.
//
// El "subject" del finding es el source — es típicamente el que tomó la
// decisión de hablar con el target (productor del mensaje, propietario
// del bucket que notifica).
func (c *CrossBoundaryInsight) buildFinding(
	sourceArn, sourceName, sourceType, sourceVpc, sourceRegion string,
	targetArn, targetName, targetType, targetVpc, targetRegion string,
	edgeType string,
	ct crossingType,
	severity inspector.Severity,
) inspector.Finding {
	title := fmt.Sprintf("%s %s crosses boundary to %s %s", sourceType, sourceName, targetType, targetName)

	description := descriptionForCrossing(ct, sourceType, sourceName, sourceVpc, targetType, targetName, targetVpc)
	remediation := remediationForCrossing(ct)

	return inspector.Finding{
		ID: fmt.Sprintf("network.crossboundary/%s/%s", sourceArn, targetArn),
		Resource: inspector.ResourceRef{
			ARN:    sourceArn,
			Type:   sourceType,
			Name:   sourceName,
			Region: sourceRegion,
		},
		Severity:    severity,
		Title:       title,
		Description: description,
		Evidence: map[string]any{
			"crossing_type": string(ct),
			"edge_type":     edgeType,
			"source_vpc":    sourceVpc,
			"target_vpc":    targetVpc,
			"source_region": sourceRegion,
			"target_region": targetRegion,
		},
		Remediation: remediation,
		RelatedResources: []inspector.ResourceRef{
			{
				ARN:    targetArn,
				Type:   targetType,
				Name:   targetName,
				Region: targetRegion,
			},
		},
	}
}

func descriptionForCrossing(ct crossingType, sourceType, sourceName, sourceVpc, targetType, targetName, targetVpc string) string {
	switch ct {
	case crossingVPCToVPC:
		return fmt.Sprintf(
			"%s '%s' in VPC %s communicates with %s '%s' in VPC %s. Communication crosses VPC boundaries, which may bypass intended network isolation.",
			sourceType, sourceName, sourceVpc, targetType, targetName, targetVpc,
		)
	case crossingVPCToPublic:
		return fmt.Sprintf(
			"%s '%s' in VPC %s communicates with %s '%s' (no VPC). This is normal when accessing public AWS services (S3, SQS) but worth verifying.",
			sourceType, sourceName, sourceVpc, targetType, targetName,
		)
	case crossingPublicToVPC:
		return fmt.Sprintf(
			"%s '%s' (no VPC) triggers %s '%s' in VPC %s. Inbound flow from public sources into a VPC-isolated resource.",
			sourceType, sourceName, targetType, targetName, targetVpc,
		)
	}
	return ""
}

func remediationForCrossing(ct crossingType) string {
	switch ct {
	case crossingVPCToVPC:
		return "Verify that this cross-VPC communication is intentional. If isolation between VPCs is required, consider using VPC peering or AWS Transit Gateway with strict routing rules, or eliminate the dependency."
	case crossingVPCToPublic:
		return "Verify that access to the public resource is intentional and that egress is via VPC endpoints when possible (to avoid traffic via internet gateway)."
	case crossingPublicToVPC:
		return "Verify that the public source is trusted. If the inbound flow is unexpected, review trigger configurations or notification settings."
	}
	return ""
}

// === Helpers para extraer valores del Record ===
