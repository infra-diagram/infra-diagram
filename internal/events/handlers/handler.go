package handlers

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/events"
)

// Handler procesa un evento de CloudTrail y aplica los cambios
// correspondientes al grafo de Neo4j.

// Cada handler atiende una sola operación AWS (ej. "CreateQueue",
// "DeleteFunction20150331"). El router se encarga de elegir cuál llamar
// según el eventName del mensaje.
type Handler interface {
	// Handle procesa el evento. Si retorna error, el worker no borra el
	// mensaje de SQS y AWS lo redelivery despues del visibility timeout.
	// Despues de N reintentos (definido en redrive policy), el mensaje
	// va al DLQ.
	Handle(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error
}

// HandlerFunc permite usar funciones planas como Handlers — útil cuando
// no necesitas state, que es lo común para nuestros handlers.
//
// Patrón estándar de Go (igual que http.HandlerFunc).
type HandlerFunc func(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error

func (f HandlerFunc) Handle(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	return f(ctx, driver, event)
}
