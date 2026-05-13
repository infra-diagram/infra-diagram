package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/crawler"
	"infra-diagram/internal/discovery"
	"infra-diagram/internal/graph"
)

func main() {
	ctx := context.Background()

	// 1. Cargar configuración de AWS desde el entorno (~/.aws/credentials, env vars, etc.)
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("loading aws config: %v", err)
	}
	region := awsCfg.Region
	log.Printf("AWS region: %s", region)

	// 2. Conectar a Neo4j — los valores vienen de variables de entorno
	//    con fallbacks razonables para desarrollo local.
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

	// Verificar la conexión antes de continuar — el driver no se conecta
	// hasta la primera query, así que VerifyConnectivity nos da feedback temprano.
	if err := driver.VerifyConnectivity(ctx); err != nil {
		log.Fatalf("neo4j connectivity check failed: %v", err)
	}
	log.Println("Connected to Neo4j")

	// 3. Crear índices ANTES de cualquier ingesta. Los crawlers asumen que
	//    los índices existen para que los MATCH de la fase 2 sean rápidos.
	if err := graph.CreateIndexes(ctx, driver); err != nil {
		log.Fatalf("creating indexes: %v", err)
	}
	log.Println("Indexes ready")

	ec2Client := ec2.NewFromConfig(awsCfg)

	// 4. Construir los crawlers concretos. Esta es la única lista que cambia
	//    cuando agregas un nuevo servicio AWS al MVP — todo lo demás es genérico.
	// IAM es global: iam.NewFromConfig no necesita región.
	crawlers := []crawler.Crawler{
		crawler.NewVPCCrawler(ec2Client, region),
		crawler.NewSubnetCrawler(ec2Client),
		crawler.NewEC2Crawler(ec2Client, region),
		crawler.NewLambdaCrawler(lambda.NewFromConfig(awsCfg)),
		crawler.NewQueueCrawler(sqs.NewFromConfig(awsCfg)),
		crawler.NewS3Crawler(awsCfg),
		crawler.NewIAMCrawler(iam.NewFromConfig(awsCfg)),
	}
	log.Printf("Initialized %d crawlers", len(crawlers))

	// 5. Inyectar todo en el Engine y correr el discovery (fase 1 + fase 2).
	engine := discovery.NewEngine(driver, crawlers)
	if err := engine.Run(ctx); err != nil {
		log.Fatalf("discovery failed: %v", err)
	}
}

// getEnv lee una variable de entorno con un valor por defecto.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
