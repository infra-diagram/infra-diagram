package events

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Worker hace polling de una queue SQS, parsea los eventos y los despacha
// al router. Es el loop principal del cmd/worker.
//
// Su trabajo es deliberadamente delgado:
//  1. ReceiveMessage (long polling, hasta 10 mensajes a la vez)
//  2. Parsear cada mensaje a EventBridgeMessage
//  3. Pasarlo al router para que lo despache al handler correcto
//  4. DeleteMessage si el handler tuvo éxito (ack)
//  5. No hacer DeleteMessage si falló — SQS hace redelivery
type Worker struct {
	sqsClient *sqs.Client
	queueURL  string
	driver    neo4j.DriverWithContext
	router    *Router
}

func NewWorker(sqsClient *sqs.Client, queueURL string, driver neo4j.DriverWithContext, router *Router) *Worker {
	return &Worker{
		sqsClient: sqsClient,
		queueURL:  queueURL,
		driver:    driver,
		router:    router,
	}
}

// Run arranca el loop de polling. Bloquea hasta que el ctx se cancele.
//
// El loop es resistente a errores: si ReceiveMessage falla (network blip,
// AWS throttling), loggea y reintenta tras un breve backoff. Los errores
// individuales por mensaje no detienen el worker — solo evitan el ack
// para que ese mensaje vuelva.
func (w *Worker) Run(ctx context.Context) error {
	log.Printf("worker started, polling %s", w.queueURL)

	for {
		select {
		case <-ctx.Done():
			log.Println("worker stopping (context canceled)")
			return ctx.Err()
		default:
		}

		if err := w.pollOnce(ctx); err != nil {
			log.Printf("poll error: %v — retrying in 5s", err)

			// Backoff suave ante errores transitorios. Si AWS está
			// rate-limitando o hay problema de red, no martilleamos.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// pollOnce hace una iteración: recibe hasta 10 mensajes y los procesa.
// Long polling significa que si no hay mensajes, espera hasta 20 segundos
// antes de retornar — esto es eficiente y reduce costos.
func (w *Worker) pollOnce(ctx context.Context) error {
	resp, err := w.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(w.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     20, // long polling
	})
	if err != nil {
		return fmt.Errorf("receiving messages: %w", err)
	}

	if len(resp.Messages) == 0 {
		// No hay nada que procesar. Loop continúa.
		return nil
	}

	log.Printf("received %d message(s)", len(resp.Messages))

	for _, msg := range resp.Messages {
		w.processMessage(ctx, msg)
	}

	return nil
}

// processMessage maneja un solo mensaje. Errores aquí solo afectan a este
// mensaje — el worker continúa procesando los demás.
//
// Si el handler retorna error: NO hacemos DeleteMessage. SQS lo redelivery
// después del visibility timeout (60s en nuestra config). Tras N reintentos
// configurados en redrive policy, va al DLQ.
//
// Si el handler retorna nil: DeleteMessage (ack), el mensaje se va.
func (w *Worker) processMessage(ctx context.Context, msg sqstypes.Message) {
	if msg.Body == nil {
		log.Printf("message %s has nil body — skipping ack", aws.ToString(msg.MessageId))
		return
	}

	event, err := ParseMessage(*msg.Body)
	if err != nil {
		// Mensaje mal formado — no nos sirve reintentar, borrar para que
		// no contamine la queue. Va al DLQ implícitamente si el patrón
		// se repite.
		log.Printf("parse error for %s: %v — deleting malformed message", aws.ToString(msg.MessageId), err)
		w.deleteMessage(ctx, msg.ReceiptHandle)
		return
	}

	if err := w.router.Route(ctx, w.driver, event); err != nil {
		// Handler falló — NO borramos. SQS lo redelivery.
		log.Printf("handler error for %s [%s]: %v",
			aws.ToString(msg.MessageId), event.Detail.EventName, err)
		return
	}

	// Éxito — ack al mensaje
	w.deleteMessage(ctx, msg.ReceiptHandle)
}

func (w *Worker) deleteMessage(ctx context.Context, receiptHandle *string) {
	_, err := w.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(w.queueURL),
		ReceiptHandle: receiptHandle,
	})
	if err != nil {
		// Si DeleteMessage falla, el mensaje volverá tras visibility timeout
		// y se reprocesará. Todos nuestros handlers son idempotentes (MERGE
		// en Neo4j), así que reprocesar es seguro.
		log.Printf("delete message error: %v", err)
	}
}
