// Package parsers contiene parsers de documentos AWS embebidos en el grafo.
//
// Los parsers convierten strings (typically JSON) a structs Go tipados que
// los insights pueden evaluar. Son funciones puras: input string, output
// struct, sin side effects.
package parsers

import (
	"encoding/json"
	"fmt"
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
