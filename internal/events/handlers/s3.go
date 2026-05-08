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

type S3Handler struct {
	crawler *crawler.S3Crawler
}

func NewS3Handler(crawler *crawler.S3Crawler) *S3Handler {
	return &S3Handler{crawler: crawler}
}

// HandleChange procesa CreateBucket y PutBucketNotificationConfiguration.
//
// Para CreateBucket, IngestSingle resuelve la región del bucket vía la
// cache de regiones del crawler. Para PutBucketNotificationConfiguration,
// también recreamos las relaciones NOTIFIES.
func (h *S3Handler) HandleChange(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	bucketName, err := extractBucketName(event)
	if err != nil {
		return fmt.Errorf("extracting bucket name: %w", err)
	}

	log.Printf("[%s] re-fetching bucket: %s", event.Detail.EventName, bucketName)

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// Para CreateBucket necesitamos crear el nodo. Para Put*Notification
	// el nodo ya existe pero IngestSingle es idempotente vía MERGE.
	if err := h.crawler.IngestSingle(ctx, session, bucketName); err != nil {
		return fmt.Errorf("ingesting bucket %s: %w", bucketName, err)
	}

	// RebuildRelations es relevante para PutBucketNotificationConfiguration.
	// Para CreateBucket no hay notificaciones todavía pero el rebuild es
	// no-op (borra cero, crea cero) — barato y mantiene consistencia.
	if err := h.crawler.RebuildRelations(ctx, session, bucketName); err != nil {
		log.Printf("rebuilding relations for %s (non-fatal): %v", bucketName, err)
	}

	return nil
}

// HandleDelete procesa DeleteBucket. Toma el ARN del payload y borra el nodo.
func (h *S3Handler) HandleDelete(ctx context.Context, driver neo4j.DriverWithContext, event *events.EventBridgeMessage) error {
	arn := event.Detail.PrimaryResourceARN()
	if arn == "" {
		// Fallback: construir desde requestParameters.bucketName
		bucketName, err := extractBucketName(event)
		if err != nil {
			return fmt.Errorf("DeleteBucket event has no ARN and no bucket name")
		}
		arn = "arn:aws:s3:::" + bucketName
	}

	log.Printf("[%s] removing bucket: %s", event.Detail.EventName, arn)

	if err := h.crawler.DeleteByArn(ctx, driver, arn); err != nil {
		return fmt.Errorf("deleting bucket %s: %w", arn, err)
	}
	return nil
}

// extractBucketName saca el nombre del bucket del payload.
//
// CloudTrail para S3 pone el bucket en requestParameters.bucketName en
// prácticamente todos los eventos (Create, Delete, Put*).
func extractBucketName(event *events.EventBridgeMessage) (string, error) {
	if len(event.Detail.RequestParameters) > 0 {
		var req struct {
			BucketName string `json:"bucketName"`
		}
		if err := json.Unmarshal(event.Detail.RequestParameters, &req); err == nil && req.BucketName != "" {
			return req.BucketName, nil
		}
	}

	// Fallback: extraer del ARN si está en resources[]
	arn := event.Detail.PrimaryResourceARN()
	if arn != "" {
		// arn:aws:s3:::bucket-name → split por ":" y tomar el último
		parts := splitARN(arn)
		if len(parts) >= 6 && parts[5] != "" {
			return parts[5], nil
		}
	}

	return "", fmt.Errorf("could not extract bucket name from event payload")
}
