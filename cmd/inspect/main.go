package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/insights"
)

func main() {
	// === Flags ===
	insightID := flag.String("insight", "", "specific insight to run (e.g. iam.blast_radius). If empty, runs all insights without required parameters.")
	roleArn := flag.String("role-arn", "", "IAM role ARN, used by insights that require it (e.g. iam.blast_radius)")
	accountID := flag.String("account-id", "", "filter findings to a specific AWS account")
	region := flag.String("region", "", "filter findings to a specific AWS region")
	resourceType := flag.String("resource-type", "", "filter findings to a specific resource type (e.g. Queue, IAMRole)")
	flag.Parse()

	ctx := context.Background()

	// === Setup ===
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

	// === Register insights ===
	registry := inspector.NewRegistry()
	registry.Register(insights.NewOrphansInsight())
	registry.Register(insights.NewOverpermissiveInsight())
	registry.Register(insights.NewCrossBoundaryInsight())
	registry.Register(insights.NewBlastRadiusInsight())

	runner := inspector.NewRunner(driver)
	filters := inspector.StandardFilters{
		AccountID:    *accountID,
		Region:       *region,
		ResourceType: *resourceType,
	}

	// === Execution mode ===
	if *insightID != "" {
		// Run a specific insight
		runSpecific(ctx, runner, registry, *insightID, *roleArn, filters)
		return
	}

	// Default: run all insights that don't require parameters
	runAllWithoutParams(ctx, runner, registry, filters)
}

// runSpecific runs a single insight by ID, building params from CLI flags.
func runSpecific(
	ctx context.Context,
	runner *inspector.Runner,
	registry *inspector.Registry,
	insightID, roleArn string,
	filters inspector.StandardFilters,
) {
	insight, ok := registry.Get(insightID)
	if !ok {
		listAvailableInsights(registry)
		log.Fatalf("insight not found: %s", insightID)
	}

	// Build params from flags based on insight requirements
	params := map[string]any{}
	if roleArn != "" {
		params["role_arn"] = roleArn
	}

	// Validate required params
	missingParams := []string{}
	for _, p := range insight.Metadata().Parameters {
		if !p.Required {
			continue
		}
		if _, ok := params[p.Name]; !ok {
			missingParams = append(missingParams, p.Name)
		}
	}

	if len(missingParams) > 0 {
		fmt.Fprintf(os.Stderr, "\nInsight '%s' requires the following parameters:\n", insightID)
		for _, p := range insight.Metadata().Parameters {
			required := ""
			if p.Required {
				required = " (required)"
			}
			fmt.Fprintf(os.Stderr, "  --%s  [%s]%s\n    %s\n",
				paramToFlag(p.Name), p.Type, required, p.Description)
		}
		fmt.Fprintf(os.Stderr, "\nExample:\n  go run ./cmd/inspect --insight=%s", insightID)
		for _, name := range missingParams {
			fmt.Fprintf(os.Stderr, " --%s=<value>", paramToFlag(name))
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	result, err := runner.Run(ctx, insight, filters, params)
	if err != nil {
		log.Fatalf("running insight %s: %v", insightID, err)
	}

	emitJSON([]*inspector.Result{result})
}

// runAllWithoutParams runs every insight that has no required parameters.
func runAllWithoutParams(
	ctx context.Context,
	runner *inspector.Runner,
	registry *inspector.Registry,
	filters inspector.StandardFilters,
) {
	var results []*inspector.Result
	for _, insight := range registry.List() {
		if insight.Metadata().HasRequiredParameters() {
			log.Printf("skipping %s (requires parameters; run with --insight=%s and required flags)", insight.ID(), insight.ID())
			continue
		}
		result, err := runner.Run(ctx, insight, filters, nil)
		if err != nil {
			log.Printf("error running %s: %v", insight.ID(), err)
			continue
		}
		results = append(results, result)
	}
	emitJSON(results)
}

func listAvailableInsights(registry *inspector.Registry) {
	fmt.Fprintln(os.Stderr, "\nAvailable insights:")
	for _, i := range registry.List() {
		meta := i.Metadata()
		reqMarker := ""
		if meta.HasRequiredParameters() {
			reqMarker = " (requires parameters)"
		}
		fmt.Fprintf(os.Stderr, "  %s%s — %s\n", i.ID(), reqMarker, meta.Title)
	}
}

func emitJSON(results []*inspector.Result) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		log.Fatalf("encoding results: %v", err)
	}
}

// paramToFlag converts a parameter name like "role_arn" to "role-arn" for CLI flags.
func paramToFlag(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
