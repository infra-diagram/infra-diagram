# Design: AWS SQS

Basado en `docs/research/sqs-research.md` (decisiones: ninguna pendiente, todas las APIs son evidentes).

## 1. Modelos de nodos

\```go
// internal/models/nodes.go

type Queue struct {
    AWSResource
    Name   string `neo4j:"name"`
    Url    string `neo4j:"url"`
    Region string `neo4j:"region"`
}
\```

Una sola struct. SQS solo tiene un tipo de recurso.

## 2. Modelos de relaciones

SQS no tiene relaciones salientes propias en el MVP. Las relaciones donde SQS participa (`TRIGGERS`, `NOTIFIES`) las maneja el otro lado:

- `TRIGGERS` (Queue → Lambda): manejada por LambdaCrawler vía `ListEventSourceMappings`
- `NOTIFIES` (S3 → Queue): fuera del MVP

No requiere structs nuevos en `relations.go`.

## 3. Estructura del crawler

\```go
type QueueCrawler struct {
    client *sqs.Client
}

func NewQueueCrawler(client *sqs.Client) *QueueCrawler
\```

**No requiere cache** porque `ListQueues` solo se llama una vez en `IngestNodes`. `IngestRelations` retorna nil sin trabajo.

**Métodos públicos:**

| Método | Recibe | Hace |
|---|---|---|
| `IngestNodes` | driver | Lista todas las queues, hace `GetQueueAttributes` para cada una, upsertea |
| `IngestSingle` | session, queueUrl | Hace `GetQueueAttributes` y upsertea |
| `IngestSingleWithDriver` | driver, queueUrl | Wrapper que abre sesión |
| `IngestRelations` | driver | Retorna nil — no hay relaciones |
| `DeleteByArn` | driver, arn | DETACH DELETE del nodo |

**No requiere `RebuildRelations`** porque no hay relaciones que rebuildar.

**Métodos privados:**

- Ninguno. La lógica de upsertion vive directo en `IngestSingle` (que también es llamada desde `IngestNodes` por cada queueUrl).

## 4. Estrategia para cada relación

| Relación | Quién la crea | Fase | Estable? | Helper |
|---|---|---|---|---|
| `Queue ←[:TRIGGERS]— Lambda` | LambdaCrawler | Fase 2 | Cambia con `CreateEventSourceMapping` | N/A (manejado por LambdaCrawler) |

QueueCrawler no tiene responsabilidad sobre relaciones.

## 5. Eventos del worker

\```go
type SQSHandler struct {
    crawler *crawler.QueueCrawler
}

func NewSQSHandler(crawler *crawler.QueueCrawler) *SQSHandler
\```

**Métodos:**

- `HandleChange` — para `CreateQueue`, `SetQueueAttributes`, `TagQueue`
- `HandleDelete` — para `DeleteQueue`

**Helper de extracción:**

\```go
func extractQueueUrl(event *events.EventBridgeMessage) (string, error)
\```

Intenta `responseElements.queueUrl` primero (caso CreateQueue), luego `requestParameters.queueUrl` (caso modificación), luego construye desde el ARN como fallback.

**Registros en `cmd/worker/main.go`:**

\```go
router.Register("CreateQueue", events.HandlerFunc(sqsHandler.HandleChange))
router.Register("SetQueueAttributes", events.HandlerFunc(sqsHandler.HandleChange))
router.Register("TagQueue", events.HandlerFunc(sqsHandler.HandleChange))
router.Register("DeleteQueue", events.HandlerFunc(sqsHandler.HandleDelete))
\```

## 6. Decisiones de implementación a confirmar

Ninguna. SQS es lo suficientemente simple que todas las decisiones son evidentes.

## 7. Checklist de archivos a crear/modificar

- [x] `internal/models/nodes.go` — agregar `Queue`
- [ ] `internal/models/relations.go` — sin cambios
- [x] `internal/crawler/queue.go` — crear nuevo
- [ ] `internal/crawler/helpers.go` — sin cambios
- [x] `internal/events/handlers/sqs.go` — crear nuevo
- [x] `cmd/discover/main.go` — agregar `crawler.NewQueueCrawler(sqsClient)`
- [x] `cmd/worker/main.go` — registrar 4 handlers
- [x] `internal/graph/setup.go` — el índice `idx_resource_arn` ya cubre Queue (vía `:AWSResource`)

## 8. Decisiones de implementación tomadas

Ninguna decisión adicional requerida. Implementación straight-forward.