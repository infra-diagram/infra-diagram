# Implementation reference: AWS SQS

Para entender el estilo de código objetivo, usa los siguientes archivos del repo:

## Archivos a leer

| Archivo | Qué demuestra |
|---|---|
| `internal/models/nodes.go` (struct `Queue`) | Cómo declarar un nodo simple |
| `internal/crawler/queue.go` | Crawler completo con IngestSingle pero sin RebuildRelations (porque Queue no tiene relaciones salientes) |
| `internal/events/handlers/sqs.go` | Handler con HandleChange y HandleDelete, incluyendo el extractor `extractQueueUrl` |
| `cmd/discover/main.go` (líneas con `NewQueueCrawler`) | Composition root del discovery |
| `cmd/worker/main.go` (líneas con `NewSQSHandler` y `router.Register`) | Composition root del worker |
| `infra/terraform/main.tf` (sección SQS del event_pattern) | EventBridge rule incluye los eventNames del servicio |

## Características específicas de SQS que el código refleja

1. **Sin cache:** SQS solo llama `ListQueues` una vez (en `IngestNodes`). No requiere `sync.Once`.

2. **GetQueueAttributes en N+1:** después de `ListQueues`, hay que llamar `GetQueueAttributes` por cada queue para obtener el ARN. Ese N+1 está aceptado para el MVP.

3. **`IngestRelations` retorna nil:** SQS no tiene relaciones salientes. Su `IngestRelations` es no-op.

4. **No requiere `RebuildRelations`:** la consecuencia natural del punto anterior.

5. **`extractQueueUrl` con fallbacks:** primero busca en `responseElements.queueUrl` (caso CreateQueue), después en `requestParameters.queueUrl` (caso DeleteQueue/SetQueueAttributes).

6. **`DeleteByArn` directo:** `DeleteQueue` en CloudTrail incluye `resources[]` con el ARN. No requiere construcción manual del ARN (a diferencia de eventos EC2).

## Anti-patrones a evitar

- **NO agregar cache si no se necesita.** SQS no lo tiene; no lo agregues "por consistencia".
- **NO crear `RebuildRelations` vacío.** Si el crawler no tiene relaciones salientes, simplemente no expongas el método.
- **NO meter llamadas a otros services dentro del crawler.** SQS solo habla con SQS. Si necesita info de Lambda, eso se modela como relación desde el otro lado.