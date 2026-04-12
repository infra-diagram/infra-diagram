package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Estructura para decodificar el JSON que EventBridge envía a SQS
type EventBridgeMsg struct {
	Source  string `json:"source"`
	Region  string `json:"region"`
	Account string `json:"account"`
	Detail  struct {
		EventName         string                 `json:"eventName"`
		RequestParameters map[string]interface{} `json:"requestParameters"`
		ResponseElements  map[string]interface{} `json:"responseElements"`
	} `json:"detail"`
}

const DB_URL string = "neo4j://127.0.0.1:7687"
const QUEUE_URL string = "https://sqs.us-east-1.amazonaws.com/110727845776/diagram-events-queue"

func main() {
	ctx := context.TODO()

	// 1. Conexiones
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Error AWS: %v", err)
	}
	sqsClient := sqs.NewFromConfig(cfg)

	driver, err := neo4j.NewDriverWithContext(DB_URL, neo4j.BasicAuth("neo4j", "password", ""))
	if err != nil {
		log.Fatalf("Error Neo4j: %v", err)
	}
	defer driver.Close(ctx)

	fmt.Println("🚀 Inciando Worker de Eventos... Escuchando SQS.")

	// 2. Bucle infinito del Worker (Long Polling)
	for {
		msgResult, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(QUEUE_URL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // Long Polling de 20 segundos
		})

		if err != nil {
			log.Printf("Error leyendo SQS: %v", err)
			continue
		}

		// Procesar cada mensaje recibido
		for _, message := range msgResult.Messages {
			processEvent(ctx, *message.Body, driver)

			// 3. Borrar el mensaje de SQS una vez procesado con éxito
			_, err := sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(QUEUE_URL),
				ReceiptHandle: message.ReceiptHandle,
			})
			if err != nil {
				log.Printf("Error borrando mensaje de SQS: %v", err)
			}
		}
	}
}

// Enrutador Lógico: Decide qué hacer basado en el eventName
func processEvent(ctx context.Context, body string, driver neo4j.DriverWithContext) {
	var event EventBridgeMsg
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		log.Printf("Error parseando JSON: %v", err)
		return
	}

	eventName := event.Detail.EventName
	log.Printf("📥 Evento recibido: %s", eventName)

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	switch eventName {
	case "CreateQueue":
		// Construimos el ARN a partir de los datos del evento
		qName := event.Detail.RequestParameters["queueName"].(string)
		arn := fmt.Sprintf("arn:aws:sqs:%s:%s:%s", event.Region, event.Account, qName)

		query := "MERGE (q:Queue {arn: $arn})"
		session.Run(ctx, query, map[string]any{"arn": arn})
		log.Printf("✅ Nodo Queue creado: %s", qName)

	case "DeleteQueue":
		// CloudTrail devuelve la URL en la petición de borrado
		qUrl := event.Detail.RequestParameters["queueUrl"].(string)
		// Para simplificar la PoC, borramos basándonos en si el ARN termina con el nombre de la cola
		// (Extraer el nombre de la URL requeriría un pequeño regex en producción)
		query := "MATCH (q:Queue) WHERE q.arn CONTAINS $qUrl DETACH DELETE q"
		session.Run(ctx, query, map[string]any{"qUrl": qUrl})
		log.Printf("🗑️ Nodo Queue eliminado.")

	case "CreateFunction20150331":
		if event.Detail.ResponseElements != nil {
			arn := event.Detail.ResponseElements["functionArn"].(string)
			name := event.Detail.ResponseElements["functionName"].(string)
			query := "MERGE (f:Function {arn: $arn})"
			session.Run(ctx, query, map[string]any{"arn": arn, "name": name})
			log.Printf("✅ Nodo Lambda creado: %s", arn)
		}

	case "DeleteFunction20150331":
		fnName := event.Detail.RequestParameters["functionName"].(string)
		// Usamos CONTAINS porque el ARN tiene el nombre de la función
		query := "MATCH (f:Function) WHERE f.arn CONTAINS $fnName DETACH DELETE f"
		session.Run(ctx, query, map[string]any{"fnName": fnName})
		log.Printf("🗑️ Nodo Lambda eliminado: %s", fnName)

	case "CreateEventSourceMapping":
		if event.Detail.ResponseElements != nil {
			sqsArn := event.Detail.ResponseElements["eventSourceArn"].(string)
			lambdaArn := event.Detail.ResponseElements["functionArn"].(string)

			query := `
				MATCH (q:Queue {arn: $sqsArn})
				MATCH (f:Function) WHERE $lambdaArn STARTS WITH f.arn
				MERGE (q)-[:TRIGGERS]->(f)
			`
			session.Run(ctx, query, map[string]any{"sqsArn": sqsArn, "lambdaArn": lambdaArn})
			log.Printf("🔗 Relación creada: %s -> %s", sqsArn, lambdaArn)
		}
	}
}
