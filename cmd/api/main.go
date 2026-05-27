package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/api"
	"infra-diagram/internal/api/service"
	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/insights/data"
	"infra-diagram/internal/inspector/insights/iam"
	"infra-diagram/internal/inspector/insights/network"
	"infra-diagram/internal/inspector/insights/structural"
	"infra-diagram/internal/store/postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// === Configuration ===
	addr := getEnv("API_ADDR", ":8080")
	neo4jURI := getEnv("NEO4J_URI", "bolt://localhost:7687")
	neo4jUser := getEnv("NEO4J_USER", "neo4j")
	neo4jPass := getEnv("NEO4J_PASSWORD", "password")
	postgresURL := getEnv("POSTGRES_URL", "postgres://infra_diagram:password@localhost:5432/infra_diagram")

	// === Neo4j ===
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
	log.Println("Connected to Neo4j")

	// === Postgres ===
	pgStore, err := postgres.New(ctx, postgresURL)
	if err != nil {
		log.Fatalf("connecting to postgres: %v", err)
	}
	defer pgStore.Close()
	log.Println("Connected to Postgres")

	// === Inspector registry ===
	registry := inspector.NewRegistry()
	registry.Register(structural.NewOrphansInsight())
	registry.Register(iam.NewOverpermissiveInsight())
	registry.Register(network.NewCrossBoundaryInsight())
	registry.Register(iam.NewBlastRadiusInsight())
	registry.Register(data.NewAccessPatternsInsight())

	// === Service layer ===
	svc := service.New(registry, driver, pgStore)

	// === HTTP server ===
	server := api.New(addr, svc)

	// Run in background so we can handle shutdown
	errCh := make(chan error, 1)
	go func() {
		log.Printf("API listening on %s", addr)
		errCh <- server.Start()
	}()

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("API shut down cleanly")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
