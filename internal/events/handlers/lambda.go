package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/crawler"
	"infra-diagram/internal/events"
)

type LambdaHandler struct {
	crawler *crawler.LambdaCrawler
}

func NewLambdaHandler(crawler *crawler.LambdaCrawler) *LambdaHandler {
	return &LambdaHandler{crawler: crawler}
}

// HandleChange procesa CreateFunction, UpdateFunctionConfiguration, etc.
//
// Lambda es el primer servicio donde RebuildRelations importa de verdad:
// UpdateFunctionConfiguration puede cambiar VPC, subnets, o sacar la lambda
// de VPC completamente. Por eso siempre llamamos RebuildRelations después
// del IngestSingle.
func (h *LambdaHandler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	functionName, err := extractFunctionName(event)
	if err != nil {
		return fmt.Errorf("extracting function name: %w", err)
	}

	log.Printf("[%s] re-fetching function: %s", event.Detail.EventName, functionName)

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// 1. Upsert del nodo
	if err := h.crawler.IngestSingle(ctx, session, functionName); err != nil {
		return fmt.Errorf("ingesting function %s: %w", functionName, err)
	}

	// 2. Rebuild de relaciones (best effort — el nodo ya está bien)
	//
	// Si las subnets/VPC referenciadas no existen aún en el grafo (pueden
	// crearse en paralelo o después), las relaciones no se crearán y se
	// completarán en el siguiente discovery. No retornamos error porque
	// el nodo en sí se creó correctamente.
	if err := h.crawler.RebuildRelations(ctx, session, functionName); err != nil {
		log.Printf("rebuilding relations for %s (non-fatal): %v", functionName, err)
	}

	return nil
}

// HandleDelete procesa DeleteFunction. Tomamos el ARN del payload y borramos
// el nodo del grafo (DETACH DELETE limpia las relaciones automáticamente).
func (h *LambdaHandler) HandleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	arn := event.Detail.PrimaryResourceARN()
	if arn == "" {
		// Fallback: construir el ARN desde requestParameters.functionName
		// Algunos eventos de Lambda no llenan resources[].
		functionName, err := extractFunctionName(event)
		if err != nil {
			return fmt.Errorf("DeleteFunction event has no ARN and no function name")
		}
		// Construimos el ARN — necesitamos region y accountId del evento
		arn = fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s",
			event.Detail.AwsRegion, event.Account, functionName)
	}

	log.Printf("[%s] removing function: %s", event.Detail.EventName, arn)

	if err := h.crawler.DeleteByArn(ctx, driver, arn); err != nil {
		return fmt.Errorf("deleting function %s: %w", arn, err)
	}
	return nil
}

// extractFunctionName saca el nombre de la función del payload.
//
// Lambda no es consistente con dónde pone el identificador:
//   - CreateFunction → responseElements.functionArn (también functionName)
//   - UpdateFunctionConfiguration → requestParameters.functionName
//   - DeleteFunction → requestParameters.functionName
//
// Intentamos request primero porque es más común en eventos de modificación.
func extractFunctionName(event *events.EventBridgeMessage) (string, error) {
	// 1. requestParameters.functionName (caso Update/Delete)
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			FunctionName string `json:"functionName"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.FunctionName != "" {
			return req.FunctionName, nil
		}
	}

	// 2. responseElements.functionName (caso Create)
	if len(event.Detail.ResponseElements) > 0 {
		var resp struct {
			FunctionName string `json:"functionName"`
		}
		if err := json.Unmarshal(event.Detail.ResponseElements, &resp); err == nil && resp.FunctionName != "" {
			return resp.FunctionName, nil
		}
	}

	// 3. Último recurso: extraer del ARN si está en resources[]
	arn := event.Detail.PrimaryResourceARN()
	if arn != "" {
		// arn:aws:lambda:us-east-1:123456789012:function:my-func
		// El nombre es el último segmento separado por ":"
		name, err := GetResourceNameFromArnLambda(arn)
		if err == nil && name != "" {
			return name, nil
		}
	}

	return "", fmt.Errorf("could not extract function name from event payload")
}

// GetResourceNameFromArnLambda parsea el nombre de una Lambda desde su ARN.
// Lambda usa ":" como separador (no "/" como otros servicios).
//
// Formato: arn:aws:lambda:{region}:{account}:function:{name}
func GetResourceNameFromArnLambda(arn string) (string, error) {
	// El ARN de Lambda tiene 7 partes separadas por ":"
	// Index 6 es el nombre de la función
	parts := splitARN(arn)
	if len(parts) < 7 {
		return "", fmt.Errorf("invalid lambda ARN: %s", arn)
	}
	return parts[6], nil
}

func splitARN(arn string) []string {
	result := []string{}
	current := ""
	for _, c := range arn {
		if c == ':' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
