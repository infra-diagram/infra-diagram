package iam

import (
	"strings"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/parsers"
)

// PermissionFinding describe una statement problemática dentro de una policy.
//
// Es el output del evaluador, no del insight directamente. El insight lo
// convierte después a inspector.Finding agregando el contexto del recurso.
type PermissionFinding struct {
	StatementIndex int // posición de la statement dentro de la policy (0-indexed)
	StatementSID   string
	Severity       inspector.Severity
	Pattern        string // "admin_total" | "service_admin" | "resource_admin" | "dangerous_action"
	Reason         string // human-readable explanation
	Evidence       map[string]any
}

// Lista de acciones que permiten privilege escalation. Cuando aparecen con
// Resource: "*" generan un finding MEDIUM.
//
// Esta lista es deliberadamente conservadora — solo las acciones más
// conocidas. Existen análisis más completos (cloudsplaining, aws-iam-actions)
// que listan ~100 acciones peligrosas. Para MVP estas 6 cubren los casos
// más comunes.
var dangerousActions = map[string]string{
	"iam:PassRole":               "Allows passing any role to AWS services — privilege escalation via Lambda/EC2",
	"iam:CreateAccessKey":        "Allows creating credentials for any user",
	"iam:AttachRolePolicy":       "Allows attaching policies to roles — direct privilege escalation",
	"iam:AttachUserPolicy":       "Allows attaching policies to users — direct privilege escalation",
	"iam:PutRolePolicy":          "Allows creating inline policies on roles",
	"iam:UpdateAssumeRolePolicy": "Allows modifying trust policies — privilege escalation",
	"sts:AssumeRole":             "Combined with a broad resource, allows assuming any role",
}

// EvaluateStatement analiza una statement y retorna findings si hay patrones
// problemáticos. Una statement puede generar 0, 1 o más findings.
//
// Solo evalúa statements con Effect: "Allow". Las Deny statements son siempre
// seguras (restricciones, no permisos).
func EvaluateStatement(stmt parsers.PolicyStatement, index int) []PermissionFinding {
	// Deny statements son siempre seguras
	if stmt.Effect != "Allow" {
		return nil
	}

	// Statements con NotAction o NotResource son inherentemente sospechosas
	// pero requieren análisis más complejo. Las marcamos para una versión
	// futura del insight.
	if len(stmt.NotActions) > 0 || len(stmt.NotResources) > 0 {
		return nil // TODO: análisis de NotAction/NotResource
	}

	hasFullResource := containsExactly(stmt.Resources, "*")

	var findings []PermissionFinding

	// Patrón 1 + 3: Action: "*"
	if containsExactly(stmt.Actions, "*") {
		if hasFullResource {
			// Patrón 1: admin total
			findings = append(findings, PermissionFinding{
				StatementIndex: index,
				StatementSID:   stmt.SID,
				Severity:       inspector.SeverityCritical,
				Pattern:        "admin_total",
				Reason:         "Statement allows all actions on all resources (Action: *, Resource: *). Equivalent to full administrator privileges.",
				Evidence: map[string]any{
					"actions":   stmt.Actions,
					"resources": stmt.Resources,
				},
			})
		} else {
			// Patrón 3: admin sobre recursos específicos
			findings = append(findings, PermissionFinding{
				StatementIndex: index,
				StatementSID:   stmt.SID,
				Severity:       inspector.SeverityHigh,
				Pattern:        "resource_admin",
				Reason:         "Statement allows all actions on specific resources (Action: *). Any listed resource is fully exposed.",
				Evidence: map[string]any{
					"actions":   stmt.Actions,
					"resources": stmt.Resources,
				},
			})
		}
		// Ya emitimos el finding más grave para esta statement, no seguimos analizando
		return findings
	}

	// Patrón 2: service:* con Resource: "*"
	if hasFullResource {
		serviceWildcards := extractServiceWildcards(stmt.Actions)
		if len(serviceWildcards) > 0 {
			findings = append(findings, PermissionFinding{
				StatementIndex: index,
				StatementSID:   stmt.SID,
				Severity:       inspector.SeverityHigh,
				Pattern:        "service_admin",
				Reason:         "Statement allows all actions for one or more services on all resources (service:* + Resource: *). Grants full admin over those services.",
				Evidence: map[string]any{
					"services":  serviceWildcards,
					"actions":   stmt.Actions,
					"resources": stmt.Resources,
				},
			})
			return findings
		}
	}

	// Patrón 4: acciones peligrosas con Resource: "*"
	if hasFullResource {
		var dangerous []string
		var reasons []string
		for _, action := range stmt.Actions {
			if reason, ok := dangerousActions[action]; ok {
				dangerous = append(dangerous, action)
				reasons = append(reasons, reason)
			}
		}
		if len(dangerous) > 0 {
			findings = append(findings, PermissionFinding{
				StatementIndex: index,
				StatementSID:   stmt.SID,
				Severity:       inspector.SeverityMedium,
				Pattern:        "dangerous_action",
				Reason:         "Statement allows dangerous actions on all resources. The listed actions enable privilege escalation.",
				Evidence: map[string]any{
					"dangerous_actions": dangerous,
					"reasons":           reasons,
					"resources":         stmt.Resources,
				},
			})
		}
	}

	return findings
}

// containsExactly retorna true si la lista contiene exactamente el valor dado.
// Útil para detectar wildcards literales "*".
func containsExactly(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// extractServiceWildcards encuentra acciones del tipo "service:*" en la lista.
// Retorna los nombres de servicios (sin el ":*").
func extractServiceWildcards(actions []string) []string {
	var services []string
	for _, action := range actions {
		if strings.HasSuffix(action, ":*") && !strings.Contains(action[:len(action)-2], "*") {
			service := action[:len(action)-2]
			services = append(services, service)
		}
	}
	return services
}
