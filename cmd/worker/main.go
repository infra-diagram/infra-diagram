package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/crawler"
	"infra-diagram/internal/events"
	"infra-diagram/internal/events/handlers"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("loading aws config: %v", err)
	}
	region := awsCfg.Region
	log.Printf("AWS region: %s", region)

	// Queue URL
	queueURL := os.Getenv("WORKER_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("WORKER_QUEUE_URL environment variable is required")
	}

	// Neo4j
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

	// Clientes AWS — un EC2 client compartido entre VPC, Subnet, EC2
	sqsClient := sqs.NewFromConfig(awsCfg)
	lambdaClient := lambda.NewFromConfig(awsCfg)
	ec2Client := ec2.NewFromConfig(awsCfg)

	// Crawlers
	queueCrawler := crawler.NewQueueCrawler(sqsClient)
	lambdaCrawler := crawler.NewLambdaCrawler(lambdaClient)
	s3Crawler := crawler.NewS3Crawler(awsCfg)
	vpcCrawler := crawler.NewVPCCrawler(ec2Client, region)
	subnetCrawler := crawler.NewSubnetCrawler(ec2Client)
	ec2Crawler := crawler.NewEC2Crawler(ec2Client, region)

	// Handlers
	sqsHandler := handlers.NewSQSHandler(queueCrawler)
	lambdaHandler := handlers.NewLambdaHandler(lambdaCrawler)
	s3Handler := handlers.NewS3Handler(s3Crawler)
	vpcHandler := handlers.NewVPCHandler(vpcCrawler)
	subnetHandler := handlers.NewSubnetHandler(subnetCrawler)
	ec2Handler := handlers.NewEC2Handler(ec2Crawler)

	// Router con todos los registros
	router := events.NewRouter()

	// SQS
	router.Register("CreateQueue", events.HandlerFunc(sqsHandler.HandleChange))
	router.Register("SetQueueAttributes", events.HandlerFunc(sqsHandler.HandleChange))
	router.Register("TagQueue", events.HandlerFunc(sqsHandler.HandleChange))
	router.Register("DeleteQueue", events.HandlerFunc(sqsHandler.HandleDelete))

	// Lambda
	router.Register("CreateFunction20150331", events.HandlerFunc(lambdaHandler.HandleChange))
	router.Register("UpdateFunctionConfiguration20150331v2", events.HandlerFunc(lambdaHandler.HandleChange))
	router.Register("UpdateFunctionCode20150331v2", events.HandlerFunc(lambdaHandler.HandleChange))
	router.Register("DeleteFunction20150331", events.HandlerFunc(lambdaHandler.HandleDelete))

	// S3
	router.Register("CreateBucket", events.HandlerFunc(s3Handler.HandleChange))
	router.Register("PutBucketNotificationConfiguration", events.HandlerFunc(s3Handler.HandleChange))
	router.Register("DeleteBucket", events.HandlerFunc(s3Handler.HandleDelete))

	// VPC
	router.Register("CreateVpc", events.HandlerFunc(vpcHandler.HandleChange))
	router.Register("DeleteVpc", events.HandlerFunc(vpcHandler.HandleDelete))

	// Subnet
	router.Register("CreateSubnet", events.HandlerFunc(subnetHandler.HandleChange))
	router.Register("DeleteSubnet", events.HandlerFunc(subnetHandler.HandleDelete))

	// EC2
	router.Register("RunInstances", events.HandlerFunc(ec2Handler.HandleChange))
	router.Register("TerminateInstances", events.HandlerFunc(ec2Handler.HandleDelete))

	// Worker loop
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
