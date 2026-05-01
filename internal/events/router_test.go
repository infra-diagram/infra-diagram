// internal/events/router_test.go
package events

import (
	"context"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestRouter_RegisteredHandler(t *testing.T) {
	called := false
	handler := handlerFunc(func(ctx context.Context, driver neo4j.DriverWithContext, event *EventBridgeMessage) error {
		called = true
		return nil
	})

	router := NewRouter()
	router.Register("CreateQueue", handler)

	event := &EventBridgeMessage{
		Detail: CloudTrailEvent{EventName: "CreateQueue"},
	}

	if err := router.Route(context.Background(), nil, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not invoked")
	}
}

func TestRouter_UnregisteredHandler(t *testing.T) {
	router := NewRouter()
	event := &EventBridgeMessage{
		Detail: CloudTrailEvent{EventName: "UnknownEvent"},
	}

	// No handler registrado → no error, simplemente loggea y devuelve nil
	if err := router.Route(context.Background(), nil, event); err != nil {
		t.Errorf("expected nil error for unregistered event, got: %v", err)
	}
}

// handlerFunc es un adaptador local para usar funciones como handlers en tests.
type handlerFunc func(context.Context, neo4j.DriverWithContext, *EventBridgeMessage) error

func (f handlerFunc) Handle(ctx context.Context, driver neo4j.DriverWithContext, event *EventBridgeMessage) error {
	return f(ctx, driver, event)
}
