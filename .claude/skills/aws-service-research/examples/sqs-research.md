# Research: AWS SQS

## 1. Identidad del servicio

- **Nombre:** Amazon Simple Queue Service (SQS)
- **SDK package:** `github.com/aws/aws-sdk-go-v2/service/sqs`
- **Tipo de recursos:** Colas de mensajes (queues)
- **Regional:** Sí (cada queue vive en una sola región)

## 2. Recursos a modelar

### Queue

- **Label sugerido:** `Queue`
- **API bulk:** `sqs.ListQueues` (devuelve solo URLs)
- **API individual:** `sqs.GetQueueAttributes` (necesario para obtener ARN y atributos)
- **Paginado:** Sí, `NewListQueuesPaginator`
- **Identificador natural:** ARN
- **Formato ARN:** `arn:aws:sqs:{region}:{accountId}:{queueName}`

## 3. Propiedades a extraer

### Queue

- `name` — nombre de la queue (extraer del ARN, último segmento)
- `url` — `*string` desde `ListQueues`, requerido para todas las operaciones posteriores
- `region` — extraer del ARN
- `accountId` — extraer del ARN

NO incluir: `MessageCount`, `VisibilityTimeout` (son configuración cambiante o estado transitorio).

## 4. Relaciones identificadas

- **Queue ←[:TRIGGERS]— Lambda Function**
  - Cómo descubrirla: `lambda.ListEventSourceMappings` (manejada por LambdaCrawler)
  - Cardinalidad: N-a-N (una queue puede triggerear múltiples lambdas)
  - Estable: Cambia con `CreateEventSourceMapping`/`DeleteEventSourceMapping`
  - Cubierta hoy: Sí, en `LambdaCrawler.ingestEventSourceMappings`. **Esta relación NO la maneja SQSCrawler.**

- **Queue ←[:NOTIFIES]— S3 Bucket**
  - Cómo descubrirla: `s3.GetBucketNotificationConfiguration` → `QueueConfigurations[]`
  - Cardinalidad: N-a-N
  - Estable: Cambia con `PutBucketNotificationConfiguration`
  - Cubierta hoy: Parcialmente — `S3Crawler` solo maneja `LambdaFunctionConfigurations`. Las notificaciones a SQS están **fuera de scope inicial**.

- **Queue ←[:RECEIVES_FROM]— SNS Topic**
  - Cómo descubrirla: `sns.ListSubscriptionsByTopic` → buscar protocol `sqs`
  - Cardinalidad: N-a-N
  - Estable: Cambia con `Subscribe`/`Unsubscribe`
  - **Fuera de scope inicial** — requiere SNS crawler que no existe.

## 5. Eventos de CloudTrail relevantes

### HandleChange
- `CreateQueue` — `requestParameters.queueName` y `responseElements.queueUrl`
- `SetQueueAttributes` — `requestParameters.queueUrl`
- `TagQueue` — `requestParameters.queueUrl`

### HandleDelete
- `DeleteQueue` — `requestParameters.queueUrl`

### NO manejar
- `ListQueues`, `GetQueueAttributes` — eventos de lectura, no afectan el grafo
- `SendMessage`, `ReceiveMessage`, `DeleteMessage` — eventos de tráfico, no de configuración

### Sobre `Resources[]`
SQS sí propaga el ARN en `detail.resources[0].ARN` consistentemente. No requiere fallback de construcción manual (a diferencia de eventos EC2).

## 6. Edge cases y quirks

- **`ListQueues` devuelve URLs, no ARNs.** Necesitas `GetQueueAttributes` con atributo `QueueArn` para obtener el ARN. Esto es N+1 calls al inicio del discovery.
- **El nombre de la queue está en el ARN, no en `ListQueues`.** Hay que parsearlo: el último segmento separado por `:`.
- **FIFO queues** tienen sufijo `.fifo` en su nombre. No requieren manejo especial pero el nombre lo refleja.
- **Queues con VPC endpoint** tienen una relación adicional con VPC, **fuera de scope inicial**.

## 7. Permisos IAM mínimos
- sqs:ListQueues
- sqs:GetQueueAttributes

Para el worker no se requieren permisos adicionales — re-usa los del discovery.

## 8. Decisiones abiertas

Ninguna decisión pendiente, todas las APIs son evidentes