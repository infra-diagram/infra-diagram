package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/crawler"
	"infra-diagram/internal/events"
	"infra-diagram/internal/events/handlers"
)

func main() {
	// Context que se cancela con Ctrl+C — para shutdown limpio
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("loading aws config: %v", err)
	}
	log.Printf("AWS region: %s", awsCfg.Region)

	// 2. SQS queue URL — viene del Terraform output
	queueURL := os.Getenv("WORKER_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("WORKER_QUEUE_URL environment variable is required")
	}

	// 3. Neo4j driver
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
	log.Println("Connected to Neo4j")

	// 4. Crawlers que los handlers van a usar
	sqsClient := sqs.NewFromConfig(awsCfg)
	queueCrawler := crawler.NewQueueCrawler(sqsClient)

	lambdaClient := lambda.NewFromConfig(awsCfg)
	lambdaCrawler := crawler.NewLambdaCrawler(lambdaClient)

	// 5. Handlers concretos
	sqsHandler := handlers.NewSQSHandler(queueCrawler)
	lambdaHandler := handlers.NewLambdaHandler(lambdaCrawler)

	// 6. Router con todos los handlers registrados
	router := events.NewRouter()

	// SQS
	router.Register("CreateQueue", events.HandlerFunc(sqsHandler.HandleChange))
	router.Register("SetQueueAttributes", events.HandlerFunc(sqsHandler.HandleChange))
	router.Register("TagQueue", events.HandlerFunc(sqsHandler.HandleChange))
	router.Register("DeleteQueue", events.HandlerFunc(sqsHandler.HandleDelete))

	// Lambda
	// 20150331 y v2 son las versiones de la API de lambda. AWS conserva los nombres antiguos
	// por compatibilidad.
	router.Register("CreateFunction20150331", events.HandlerFunc(lambdaHandler.HandleChange))
	router.Register("UpdateFunctionConfiguration20150331v2", events.HandlerFunc(lambdaHandler.HandleChange))
	router.Register("UpdateFunctionCode20150331v2", events.HandlerFunc(lambdaHandler.HandleChange))
	router.Register("DeleteFunction20150331", events.HandlerFunc(lambdaHandler.HandleDelete))

	// 7. Worker y arranque del loop
	worker := events.NewWorker(sqsClient, queueURL, driver, router)
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("worker stopped with error: %v", err)
	}

	log.Println("worker shut down cleanly")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
