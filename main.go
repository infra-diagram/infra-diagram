package main

// event bridgde rule ARN: arn:aws:events:us-east-1:110727845776:rule/DiagramInfraUpdates

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

var cfg aws.Config

func ingest() {
	ctx := context.TODO()

	// 1. Configurar conexión a AWS
	// Go leerá automáticamente tus credenciales locales (~/.aws/credentials)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Error cargando configuración de AWS: %v", err)
	}

	sqsClient := sqs.NewFromConfig(cfg)
	lambdaClient := lambda.NewFromConfig(cfg)

	// 2. Configurar conexión a Neo4j (Ajusta con tus credenciales locales)
	dbUri := "bolt://localhost:7687"
	dbUser := "neo4j"
	dbPassword := "password"

	driver, err := neo4j.NewDriverWithContext(dbUri, neo4j.BasicAuth(dbUser, dbPassword, ""))
	if err != nil {
		log.Fatalf("Error conectando a Neo4j: %v", err)
	}
	defer driver.Close(ctx)

	// Iniciamos la sesión de base de datos
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// 3. Ejecutar Discovery Concurrente
	var wg sync.WaitGroup
	wg.Add(2) // Esperaremos a 2 procesos concurrentes

	// Escanear SQS en una Goroutine
	go func() {
		defer wg.Done()
		fmt.Println("Iniciando escaneo de SQS...")
		ingestSQS(ctx, sqsClient, session)
	}()

	// Escanear Lambdas en otra Goroutine
	go func() {
		defer wg.Done()
		fmt.Println("Iniciando escaneo de Lambdas...")
		ingestLambdas(ctx, lambdaClient, session)
	}()

	// Esperar a que los nodos base existan antes de mapear las relaciones
	wg.Wait()

	// 4. Mapear Relaciones (Event Source Mappings)
	fmt.Println("Iniciando mapeo de relaciones (SQS -> Lambda)...")
	ingestEventSourceMappings(ctx, lambdaClient, session)

	fmt.Println("¡Ingesta inicial completada con éxito!")
}

// --- Funciones de Ingesta ---

func ingestSQS(ctx context.Context, client *sqs.Client, session neo4j.SessionWithContext) {
	// Obtenemos las URLs de las colas
	resp, err := client.ListQueues(ctx, &sqs.ListQueuesInput{})
	if err != nil {
		log.Printf("Error listando SQS: %v", err)
		return
	}

	for _, qUrl := range resp.QueueUrls {
		// SQS requiere una llamada extra para obtener el ARN
		attrResp, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       &qUrl,
			AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
		})
		if err != nil {
			continue
		}

		arn := attrResp.Attributes[string(types.QueueAttributeNameQueueArn)]

		// Insertar en Neo4j usando MERGE por idempotencia
		query := "MERGE (q:Queue {arn: $arn})"
		_, err = session.Run(ctx, query, map[string]any{"arn": arn})
		if err != nil {
			log.Printf("Error guardando SQS en Neo4j: %v", err)
		}
	}
}

func ingestLambdas(ctx context.Context, client *lambda.Client, session neo4j.SessionWithContext) {
	resp, err := client.ListFunctions(ctx, &lambda.ListFunctionsInput{})
	if err != nil {
		log.Printf("Error listando Lambdas: %v", err)
		return
	}

	for _, fn := range resp.Functions {
		fmt.Printf("Ingestando Lambda: %s\n", *fn.FunctionName)
		query := "MERGE (f:Function {arn: $arn, name: $name})"
		params := map[string]any{
			"arn":  *fn.FunctionArn,
			"name": *fn.FunctionName,
		}

		_, err = session.Run(ctx, query, params)
		if err != nil {
			log.Printf("Error guardando Lambda %s en Neo4j: %v", fn.FunctionName, err)
		}
	}
}

func ingestEventSourceMappings(ctx context.Context, client *lambda.Client, session neo4j.SessionWithContext) {
	resp, err := client.ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{})
	if err != nil {
		log.Printf("Error listando mapeos: %v", err)
		return
	}

	for _, mapping := range resp.EventSourceMappings {
		// Solo nos interesan los mapeos que vienen de SQS para este MVP
		// Validamos el estado para no diagramar conexiones rotas o eliminadas
		if *mapping.State != "Enabled" && *mapping.State != "Creating" {
			continue
		}

		query := `
			MATCH (q:Queue {arn: $sqsArn})
			MATCH (f:Function {arn: $lambdaArn})
			MERGE (q)-[:TRIGGERS]->(f)
		`
		params := map[string]any{
			"sqsArn":    *mapping.EventSourceArn,
			"lambdaArn": *mapping.FunctionArn,
		}

		_, err = session.Run(ctx, query, params)
		if err != nil {
			log.Printf("Error creando relación en Neo4j: %v", err)
		}
	}
}
