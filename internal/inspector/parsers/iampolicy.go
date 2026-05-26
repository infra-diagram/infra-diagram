// Package parsers contiene parsers de documentos AWS embebidos en el grafo.
//
// Los parsers convierten strings (typically JSON) a structs Go tipados que
// los insights pueden evaluar. Son funciones puras: input string, output
// struct, sin side effects.
package parsers

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PolicyDocument representa un documento de política IAM parseado.
//
// AWS permite que Action, Resource, NotAction, NotResource sean string O
// array de strings. Los normalizamos a slice siempre para simplificar
// la evaluación downstream.
type PolicyDocument struct {
	Version    string            `json:"Version"`
	ID         string            `json:"Id,omitempty"`
	Statements []PolicyStatement `json:"Statement"`
}

type PolicyStatement struct {
	SID          string         `json:"Sid,omitempty"`
	Effect       string         `json:"Effect"` // "Allow" | "Deny"
	Actions      []string       `json:"Action,omitempty"`
	NotActions   []string       `json:"NotAction,omitempty"`
	Resources    []string       `json:"Resource,omitempty"`
	NotResources []string       `json:"NotResource,omitempty"`
	Principals   map[string]any `json:"Principal,omitempty"`
	Conditions   map[string]any `json:"Condition,omitempty"`
}

// ParsePolicyDocument deserializa el JSON de una policy IAM.
//
// Maneja la peculiaridad de AWS: Action/Resource pueden ser string o []string.
// Después del parse, siempre son []string (uno o N elementos).
func ParsePolicyDocument(jsonStr string) (*PolicyDocument, error) {
	if jsonStr == "" {
		return nil, fmt.Errorf("empty policy document")
	}

	// Primero parseamos a un struct intermedio con json.RawMessage para los
	// campos polimórficos, después los normalizamos.
	var raw struct {
		Version    string          `json:"Version"`
		ID         string          `json:"Id,omitempty"`
		Statements json.RawMessage `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("unmarshaling policy: %w", err)
	}

	// Statement puede ser un objeto único o un array
	statements, err := parseStatements(raw.Statements)
	if err != nil {
		return nil, fmt.Errorf("parsing statements: %w", err)
	}

	return &PolicyDocument{
		Version:    raw.Version,
		ID:         raw.ID,
		Statements: statements,
	}, nil
}

// parseStatements maneja Statement como objeto único o array.
func parseStatements(raw json.RawMessage) ([]PolicyStatement, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Intentar array primero
	var array []rawStatement
	if err := json.Unmarshal(raw, &array); err == nil {
		out := make([]PolicyStatement, len(array))
		for i, rs := range array {
			out[i] = rs.normalize()
		}
		return out, nil
	}

	// Si no es array, intentar objeto único
	var single rawStatement
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("statement is neither array nor object: %w", err)
	}
	return []PolicyStatement{single.normalize()}, nil
}

// rawStatement es el shape literal del JSON, donde Action/Resource pueden
// ser string o []string. Lo normalizamos a []string en normalize().
type rawStatement struct {
	SID         string          `json:"Sid,omitempty"`
	Effect      string          `json:"Effect"`
	Action      json.RawMessage `json:"Action,omitempty"`
	NotAction   json.RawMessage `json:"NotAction,omitempty"`
	Resource    json.RawMessage `json:"Resource,omitempty"`
	NotResource json.RawMessage `json:"NotResource,omitempty"`
	Principal   map[string]any  `json:"Principal,omitempty"`
	Condition   map[string]any  `json:"Condition,omitempty"`
}

func (rs rawStatement) normalize() PolicyStatement {
	return PolicyStatement{
		SID:          rs.SID,
		Effect:       rs.Effect,
		Actions:      normalizeStringOrArray(rs.Action),
		NotActions:   normalizeStringOrArray(rs.NotAction),
		Resources:    normalizeStringOrArray(rs.Resource),
		NotResources: normalizeStringOrArray(rs.NotResource),
		Principals:   rs.Principal,
		Conditions:   rs.Condition,
	}
}

// normalizeStringOrArray convierte un campo JSON que puede ser string o
// []string a []string. Si el campo está vacío, retorna nil.
func normalizeStringOrArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	// Intentar array
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}

	// Intentar string
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return []string{str}
	}

	return nil
}

// AccessGrant representa un permiso individual: una action específica
// sobre un resource específico. Una statement de policy puede generar
// múltiples AccessGrants (producto cartesiano de Actions × Resources).
type AccessGrant struct {
	Action   string // ej. "s3:GetObject"
	Resource string // ej. "arn:aws:s3:::my-bucket/*"
}

// ExtractAccessGrants walks the policy and returns all (action, resource)
// combinations from Allow statements.
//
// MVP limitations:
//   - Ignores Conditions (e.g., MFA required, source VPC)
//   - Does not apply Deny overrides
//   - Skips NotAction / NotResource statements
//   - Returns "*" actions and resources as-is (caller decides how to handle)
func ExtractAccessGrants(doc *PolicyDocument) []AccessGrant {
	var grants []AccessGrant
	for _, stmt := range doc.Statements {
		if stmt.Effect != "Allow" {
			continue
		}
		if len(stmt.NotActions) > 0 || len(stmt.NotResources) > 0 {
			// Negation patterns require complex evaluation, skip for MVP
			continue
		}
		for _, action := range stmt.Actions {
			for _, resource := range stmt.Resources {
				if action == "" || resource == "" {
					continue
				}
				grants = append(grants, AccessGrant{
					Action:   action,
					Resource: resource,
				})
			}
		}
	}
	return grants
}

// IsDataPlaneAction returns true if the action operates on data within a
// resource (read/write operations) rather than control plane (CRUD on the
// resource itself).
//
// This is the heuristic used by the access_patterns insight to filter
// noise: a role with `iam:ListRoles` is not "accessing data", but a role
// with `s3:GetObject` is.
//
// Conservative list — extend as needed.
func IsDataPlaneAction(action string) bool {
	// Wildcards always pass — let downstream decide
	if action == "*" {
		return true
	}

	parts := strings.SplitN(action, ":", 2)
	if len(parts) != 2 {
		return false
	}
	service, op := parts[0], parts[1]

	// Service-wide wildcard (e.g. "s3:*") includes data plane
	if op == "*" {
		return true
	}

	switch service {
	case "s3":
		return strings.HasPrefix(op, "Get") ||
			strings.HasPrefix(op, "Put") ||
			strings.HasPrefix(op, "Delete") ||
			strings.HasPrefix(op, "List") ||
			strings.HasPrefix(op, "Copy") ||
			strings.HasPrefix(op, "Restore")
	case "sqs":
		return op == "SendMessage" ||
			op == "ReceiveMessage" ||
			op == "DeleteMessage" ||
			op == "SendMessageBatch" ||
			op == "DeleteMessageBatch" ||
			op == "PurgeQueue"
	case "lambda":
		return op == "InvokeFunction" ||
			op == "InvokeAsync"
	case "dynamodb":
		return strings.HasPrefix(op, "Get") ||
			strings.HasPrefix(op, "Put") ||
			strings.HasPrefix(op, "Update") ||
			strings.HasPrefix(op, "Delete") ||
			strings.HasPrefix(op, "Query") ||
			strings.HasPrefix(op, "Scan") ||
			strings.HasPrefix(op, "BatchGet") ||
			strings.HasPrefix(op, "BatchWrite")
	}
	return false
}
