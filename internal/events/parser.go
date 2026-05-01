package events

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventBridgeMessage es el envelope que EventBridge pone alrededor del
// evento original de CloudTrail. Lo que llega a SQS tiene esta forma.
type EventBridgeMessage struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"` // ej "aws.sqs"
	Account    string          `json:"account"`
	Time       time.Time       `json:"time"`
	Region     string          `json:"region"`
	Detail     CloudTrailEvent `json:"detail"`
}

// CloudTrailEvent es el evento original de CloudTrail anidado dentro de
// EventBridgeMessage.Detail. Es donde están los datos que nos interesan.
type CloudTrailEvent struct {
	EventVersion      string          `json:"eventVersion"`
	EventTime         time.Time       `json:"eventTime"`
	EventSource       string          `json:"eventSource"` // ej "sqs.amazonaws.com"
	EventName         string          `json:"eventName"`   // ej "CreateQueue" — el campo de routing
	AwsRegion         string          `json:"awsRegion"`
	UserIdentity      UserIdentity    `json:"userIdentity"`
	RequestParameters json.RawMessage `json:"requestParameters"` // forma variable, lo dejamos crudo
	ResponseElements  json.RawMessage `json:"responseElements"`  // idem
	Resources         []ResourceRef   `json:"resources"`
	ErrorCode         string          `json:"errorCode,omitempty"`
	ErrorMessage      string          `json:"errorMessage,omitempty"`
}

// UserIdentity describe quién hizo la llamada API.
type UserIdentity struct {
	Type      string `json:"type"`
	ARN       string `json:"arn"`
	AccountID string `json:"accountId"`
	UserName  string `json:"userName,omitempty"`
}

// ResourceRef es el identificador de un recurso afectado por el evento.
// Para la mayoría de eventos del MVP, la primera entrada de Resources tiene
// el ARN del recurso creado/modificado/eliminado.
type ResourceRef struct {
	AccountID string `json:"accountId"`
	Type      string `json:"type"` // ej "AWS::SQS::Queue"
	ARN       string `json:"ARN"`
}

// ParseMessage deserializa el body de un mensaje SQS al struct del evento.
func ParseMessage(body string) (*EventBridgeMessage, error) {
	var msg EventBridgeMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return nil, fmt.Errorf("unmarshaling event: %w", err)
	}

	// Validaciones básicas — si falta algo crítico, el evento es inválido
	// y no hay mucho que el resto del pipeline pueda hacer con él.
	if msg.Detail.EventName == "" {
		return nil, fmt.Errorf("event missing eventName")
	}

	return &msg, nil
}

// PrimaryResourceARN devuelve el ARN del primer recurso afectado, o ""
// si el evento no trae un Resources array poblado. Conveniente para
// handlers que solo necesitan saber qué recurso se afectó.
func (e *CloudTrailEvent) PrimaryResourceARN() string {
	if len(e.Resources) == 0 {
		return ""
	}
	return e.Resources[0].ARN
}
