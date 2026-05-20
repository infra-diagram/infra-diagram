package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/insights"
)

func main() {
	ctx := context.Background()

	neo4jURI := getEnv("NEO4J_URI", "bolt://localhost:7687")
	neo4jUser := getEnv("NEO4J_USER", "neo4j")
	neo4jPass := getEnv("NEO4J_PASSWORD", "password")

	driver, err := neo4jdriver.NewDriverWithContext(
		neo4jURI,
		neo4jdriver.BasicAuth(neo4jUser, neo4jPass, ""),
	)
	if err != nil {
		log.Fatalf("connecting to neo4j: %v", err)
	}
	defer driver.Close(ctx)

	if err := driver.VerifyConnectivity(ctx); err != nil {
		log.Fatalf("neo4j connectivity check failed: %v", err)
	}

	// Setup registry
	registry := inspector.NewRegistry()
	registry.Register(insights.NewOrphansInsight())
	registry.Register(insights.NewOverpermissiveInsight()) // ← nuevo
	// Aquí se registran los demás insights conforme se implementan

	runner := inspector.NewRunner(driver)

	// Correr todos los insights, sin filtros, sin parámetros
	results := make([]*inspector.Result, 0, len(registry.List()))
	for _, insight := range registry.List() {
		result, err := runner.Run(ctx, insight, inspector.StandardFilters{}, nil)
		if err != nil {
			log.Printf("error running %s: %v", insight.ID(), err)
			continue
		}
		results = append(results, result)
	}

	// Emitir como JSON a stdout
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		log.Fatalf("encoding results: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
