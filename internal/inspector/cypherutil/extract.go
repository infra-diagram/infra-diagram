// Package cypherutil contiene helpers para extraer valores de neo4j.Record
// de forma defensiva. Usados por todos los insights para evitar duplicación.
package cypherutil

import "github.com/neo4j/neo4j-go-driver/v5/neo4j"

// GetString extrae un string de un record por key. Retorna "" si no existe
// o no es string.
func GetString(record *neo4j.Record, key string) string {
	val, ok := record.Get(key)
	if !ok {
		return ""
	}
	s, _ := val.(string)
	return s
}

// GetStringSlice extrae un []string de un record. Maneja el caso donde
// Neo4j retorna []any que necesita coerción.
func GetStringSlice(record *neo4j.Record, key string) []string {
	val, ok := record.Get(key)
	if !ok {
		return nil
	}
	items, ok := val.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// GetMap extrae un map[string]any de un record. Útil para properties(n)
// que retorna todos los atributos de un nodo.
func GetMap(record *neo4j.Record, key string) map[string]any {
	val, ok := record.Get(key)
	if !ok {
		return nil
	}
	m, _ := val.(map[string]any)
	return m
}

// PrimaryLabel retorna el primer label que no sea "AWSResource".
// Todos los nodos en el proyecto llevan doble label (ej. "Queue", "AWSResource")
// y el primero es el tipo concreto.
func PrimaryLabel(labels []string) string {
	for _, l := range labels {
		if l != "AWSResource" {
			return l
		}
	}
	if len(labels) > 0 {
		return labels[0]
	}
	return "Unknown"
}
