package insights

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/parsers"
	"infra-diagram/internal/inspector/queries"
)

// OverpermissiveInsight detecta policies con permisos excesivos.
//
// Cruza la detección con si la policy está adjunta a roles activos (no
// huérfanos) para calibrar la severidad: una policy con Action: * adjunta
// a un role en uso es más urgente que la misma adjunta solo a roles huérfanos.
type OverpermissiveInsight struct{}

func NewOverpermissiveInsight() *OverpermissiveInsight {
	return &OverpermissiveInsight{}
}

func (o *OverpermissiveInsight) ID() string {
	return "iam.overpermissive_policies"
}

func (o *OverpermissiveInsight) Version() string {
	return "1.0.0"
}

func (o *OverpermissiveInsight) Metadata() inspector.Metadata {
	return inspector.Metadata{
		Title:       "Overpermissive policies",
		Description: "Policies with overly broad statements: Action: *, Resource: *, or dangerous actions. Severity calibrated based on whether the policy is in use by active roles.",
		Category:    "iam",
		Severity:    inspector.SeverityHigh,
		Parameters:  nil,
	}
}

func (o *OverpermissiveInsight) Execute(
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

	// Si el filtro restringe el tipo, solo aplica si es IAMPolicy
	if filters.ResourceType != "" && filters.ResourceType != "IAMPolicy" {
		return result, nil
	}

	cypherResult, err := session.Run(ctx, queries.OverPermissive, map[string]any{
		"accountId": filters.AccountID,
	})
	if err != nil {
		return nil, fmt.Errorf("running query: %w", err)
	}

	for cypherResult.Next(ctx) {
		record := cypherResult.Record()

		policyArn, _ := record.Get("policyArn")
		policyArnStr, _ := policyArn.(string)
		if policyArnStr == "" {
			continue
		}

		policyName, _ := record.Get("policyName")
		policyNameStr, _ := policyName.(string)

		document, _ := record.Get("document")
		documentStr, _ := document.(string)

		accountId, _ := record.Get("accountId")
		accountIdStr, _ := accountId.(string)

		activeRolesRaw, _ := record.Get("activeRoles")
		activeRoles := parseActiveRoles(activeRolesRaw)

		// Parsear el documento
		parsed, err := parsers.ParsePolicyDocument(documentStr)
		if err != nil {
			// Documento malformado — no podemos analizar pero lo loggeamos
			// (en práctica con un logger, no fmt)
			continue
		}

		// Evaluar cada statement
		for idx, stmt := range parsed.Statements {
			permFindings := EvaluateStatement(stmt, idx)
			for _, pf := range permFindings {
				finding := buildFinding(
					policyArnStr, policyNameStr, accountIdStr,
					pf, activeRoles,
				)
				result.Findings = append(result.Findings, finding)
			}
		}

		// Si el policy generó al menos un finding, lo agregamos al subgrafo
		if hasFindings(result.Findings, policyArnStr) {
			result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
				ARN:    policyArnStr,
				Labels: []string{"IAMPolicy", "AWSResource"},
				Properties: map[string]any{
					"policyName": policyNameStr,
					"accountId":  accountIdStr,
				},
			})
			// Agregar también los roles activos como nodos contextuales
			for _, role := range activeRoles {
				result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
					ARN:    role.ARN,
					Labels: []string{"IAMRole", "AWSResource"},
					Properties: map[string]any{
						"roleName": role.Name,
					},
				})
				result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
					FromARN: role.ARN,
					ToARN:   policyArnStr,
					Type:    "ATTACHED_POLICY",
				})
			}
		}
	}

	if err := cypherResult.Err(); err != nil {
		return nil, fmt.Errorf("iterating results: %w", err)
	}

	return result, nil
}

// roleRef es la representación local de un role activo desde la query.
type roleRef struct {
	ARN  string
	Name string
}

// parseActiveRoles convierte el array de mapas del Cypher a structs Go.
// Filtra entries con ARN vacío (resultado de OPTIONAL MATCH sin matches).
func parseActiveRoles(raw any) []roleRef {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]roleRef, 0, len(items))
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
		out = append(out, roleRef{ARN: arnStr, Name: nameStr})
	}
	return out
}

// buildFinding construye un Finding completo a partir de una PermissionFinding
// y el contexto de la policy + roles activos.
//
// La severidad se eleva si hay roles activos usando esta policy.
func buildFinding(
	policyArn, policyName, accountId string,
	pf PermissionFinding,
	activeRoles []roleRef,
) inspector.Finding {
	severity := pf.Severity

	// Si hay roles activos, mantener la severidad. Si no hay roles activos,
	// bajar un nivel (la policy es problemática pero no está en uso).
	if len(activeRoles) == 0 {
		severity = downgradeSeverity(severity)
	}

	// Construir lista de roles afectados para RelatedResources
	relatedResources := make([]inspector.ResourceRef, 0, len(activeRoles))
	for _, role := range activeRoles {
		relatedResources = append(relatedResources, inspector.ResourceRef{
			ARN:  role.ARN,
			Type: "IAMRole",
			Name: role.Name,
		})
	}

	evidence := map[string]any{
		"pattern":         pf.Pattern,
		"statementIndex":  pf.StatementIndex,
		"statementSID":    pf.StatementSID,
		"activeRoleCount": len(activeRoles),
	}
	// Merge la evidencia específica de la statement
	for k, v := range pf.Evidence {
		evidence[k] = v
	}

	title := fmt.Sprintf("Overpermissive policy: %s", policyName)
	if pf.StatementSID != "" {
		title = fmt.Sprintf("Overpermissive policy: %s [SID: %s]", policyName, pf.StatementSID)
	}

	return inspector.Finding{
		ID: fmt.Sprintf("iam.overpermissive_policies/%s/stmt-%d", policyArn, pf.StatementIndex),
		Resource: inspector.ResourceRef{
			ARN:  policyArn,
			Type: "IAMPolicy",
			Name: policyName,
		},
		Severity:         severity,
		Title:            title,
		Description:      pf.Reason,
		Evidence:         evidence,
		Remediation:      remediationFor(pf.Pattern),
		RelatedResources: relatedResources,
	}
}

// downgradeSeverity baja un nivel de severidad para findings de policies
// no en uso por roles activos.
func downgradeSeverity(s inspector.Severity) inspector.Severity {
	switch s {
	case inspector.SeverityCritical:
		return inspector.SeverityHigh
	case inspector.SeverityHigh:
		return inspector.SeverityMedium
	case inspector.SeverityMedium:
		return inspector.SeverityLow
	default:
		return inspector.SeverityInfo
	}
}

// remediationFor retorna el texto de remediación según el patrón detectado.
func remediationFor(pattern string) string {
	switch pattern {
	case "admin_total":
		return "Replace Action: \"*\" and Resource: \"*\" with specific permissions. If the role genuinely needs admin, consider using the AWS managed policy AdministratorAccess and document the risk. Ideally, split the policy into multiple roles with lower privileges."
	case "service_admin":
		return "Change service:* to the specific actions the role needs. For example, instead of s3:* use s3:GetObject, s3:PutObject based on the actual use case."
	case "resource_admin":
		return "List specific actions instead of \"*\". If all actions are genuinely needed, restrict Resource to specific ARNs."
	case "dangerous_action":
		return "Restrict Resource to specific ARNs. IAM actions like PassRole and AssumeRole must use Resource with concrete ARNs, not \"*\"."
	default:
		return "Review the statement and apply the principle of least privilege."
	}
}

// hasFindings verifica si ya hay al menos un finding para el policy dado.
// Útil para decidir si agregamos el nodo al subgrafo (solo si tiene findings).
func hasFindings(findings []inspector.Finding, policyArn string) bool {
	for _, f := range findings {
		if f.Resource.ARN == policyArn {
			return true
		}
	}
	return false
}
