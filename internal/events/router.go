package events

import (
	"context"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Router despacha eventos al handler correspondiente segun su eventName.

// Es el punto unico de configuracion del worker: agregar soporte para una
// nueva operacion AWS es una linea aqui (Register) y un nuevo archivo en
// internal/events/handlers/.
type Router struct {
	handlers map[string]HandlerLike
}

// HandlerLike es la interfaz que implementan los handlers. Definida aqui
// (no en el package handlers/) para evitar import cycle: el package events
// no puede importar handlers, porque handlers importa events.
type HandlerLike interface {
	Handle(ctx context.Context, driver neo4j.DriverWithContext, event *EventBridgeMessage) error
}

func NewRouter() *Router {
	return &Router{
		handlers: make(map[string]HandlerLike),
	}
}

// Register asocia un handler a un eventName especifico.
// Si ya existe un handler para ese eventName, lo sobreescribe.
func (r *Router) Register(eventName string, h HandlerLike) {
	r.handlers[eventName] = h
}

// Route procesa el evento llamando al handler registrado para su eventName.
// Si no hay handler registrado, loggea y devuelve nil — eventos no soportados
// se ignoran silenciosamente y el mensaje se borra de SQS.
func (r *Router) Route(ctx context.Context, driver neo4j.DriverWithContext, event *EventBridgeMessage) error {
	eventName := event.Detail.EventName

	handler, ok := r.handlers[eventName]
	if !ok {
		log.Printf("no handler registered for event: %s (source=%s)", eventName, event.Source)
		return nil
	}

	log.Printf("handling event: %s [arn=%s]", eventName, event.Detail.PrimaryResourceARN())
	return handler.Handle(ctx, driver, event)
}
