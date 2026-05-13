---
name: crawler-design
description: "Diseñar el modelo de datos y la estructura del crawler/handler para un servicio AWS en el proyecto infra-diagram, basándose en un documento de research previo. Activar cuando el usuario menciona diseñar el crawler, modelar el servicio, definir nodes/relations, o pasar del research al diseño. Ejemplos: 'vamos a diseñar el crawler de IAM', 'modelemos los nodos de RDS', 'cómo estructuramos el crawler de SNS', 'pasemos al diseño después del research'."
---

# Crawler Design

Tu tarea es producir un **documento de diseño técnico** que traduce el research de un servicio AWS en decisiones concretas de implementación para el proyecto infra-diagram.

Este documento es el input directo de la fase de implementación. Debe ser tan específico que escribir el código sea trabajo mecánico de seguir las decisiones tomadas aquí.

## Input requerido

El documento de research del servicio en `docs/research/{servicio}-research.md`. **Si el archivo no existe, no procedas** — pide al usuario que lo genere primero usando el SKILL `aws-service-research`.

El research debe incluir la sección "9. Decisiones tomadas" con las decisiones del usuario sobre scope. Si esa sección no existe o está incompleta, pide al usuario que la complete antes de continuar.

## Contexto del proyecto

Antes de escribir el diseño, **lee estos archivos del repo** para entender el patrón actual:

- `internal/models/nodes.go` — para ver el formato de los structs de nodos
- `internal/models/relations.go` — para ver el formato de los structs de relaciones
- `internal/crawler/queue.go` — caso simple sin relaciones (usar como base)
- `internal/crawler/lambda.go` — caso complejo con relaciones cambiantes (referencia avanzada)
- `internal/crawler/helpers.go` — helpers existentes (BuildArn, GetRegionFromArn, rebuildBelongsTo, etc.)
- `internal/events/handlers/sqs.go` — handler simple
- `internal/events/handlers/lambda.go` — handler con relaciones
- `infra/terraform/main.tf` — para ver el event_pattern actual de EventBridge (qué services y eventNames están registrados)

Esto te da el "estilo casero" del proyecto, que es más importante que cualquier convención teórica.

## Output: estructura del documento

Produce un archivo markdown en `docs/design/{servicio}-design.md` con estas secciones EN ESTE ORDEN.

### 1. Modelos de nodos

Para cada tipo de recurso, especifica el struct Go exacto que va en `internal/models/nodes.go`:

\```go
type IAMRole struct {
    AWSResource
    RoleName    string `neo4j:"roleName"`
    RoleId      string `neo4j:"roleId"`
    Path        string `neo4j:"path"`
    CreateDate  string `neo4j:"createDate"`  // ISO 8601
    Description string `neo4j:"description"` // puede venir vacío
}
\```

Reglas:
- Heredan de `AWSResource` (que incluye `Arn` y `AccountId`)
- Solo propiedades estables (no transitorias como `state` que cambian seguido — excepto cuando el research lo justifique)
- `time.Time` se serializa como string ISO 8601
- Tags `neo4j:"camelCase"` siempre

### 2. Modelos de relaciones

Para cada tipo de relación, especifica el struct Go que va en `internal/models/relations.go`. Si la relación encaja con uno existente (`BelongsToRelation`, `TriggerRelation`, etc.), reutilízalo y nóta cuál.

\```go
// (:IAMRole)-[:ATTACHED_POLICY]->(:IAMPolicy)
type AttachedPolicyRelation struct {
    SourceArn string `neo4j:"sourceArn"`
    TargetArn string `neo4j:"targetArn"`
}
\```

### 3. Estructura del crawler

Especifica el archivo `internal/crawler/{servicio}.go` con:

**Tipo del struct y campos del crawler:**
\```go
type IAMCrawler struct {
    client      *iam.Client
    ec2Client   *ec2.Client      // necesario para Role→EC2
    lambdaClient *lambda.Client  // necesario para Role→Lambda

    rolesOnce sync.Once
    roles     []iamtypes.Role
    rolesErr  error
    // ... otros caches si aplica
}
\```

Justifica cada dependencia: por qué necesita un EC2 client, por qué necesita el cache, etc.

**Constructor:**
\```go
func NewIAMCrawler(iamClient *iam.Client, ec2Client *ec2.Client, lambdaClient *lambda.Client) *IAMCrawler
\```

**Métodos públicos a implementar (en este orden):**

| Método | Recibe | Hace |
|---|---|---|
| `IngestNodes` | driver | Lista todos los recursos del servicio y upserta cada uno |
| `IngestSingle` | session, identificador | Upserta UN nodo por su identificador natural |
| `IngestSingleWithDriver` | driver, identificador | Wrapper que abre sesión |
| `IngestRelations` | driver | Recrea TODAS las relaciones del servicio (bulk) |
| `RebuildRelations` | session, identificador | Recrea las relaciones de UN nodo |
| `RebuildRelationsWithDriver` | driver, identificador | Wrapper que abre sesión |
| `DeleteByArn` | driver, arn | Borra el nodo + todas sus relaciones (DETACH DELETE) |

Si el servicio tiene **múltiples tipos de recursos** (ej. IAM con Roles y Policies), cada tipo necesita sus propios métodos `IngestNodes_X`, `IngestSingle_X`, etc., o el crawler se divide en varios crawlers (decisión a tomar).

**Métodos privados auxiliares:**

Lista métodos privados como `upsertNodeFromRole`, `fetchRole`, `rebuildRoleRelations`, etc. Justifica cuáles necesitas y por qué.

### 4. Estrategia para cada relación

Para cada relación identificada en el research, decide:

| Pregunta | Opciones |
|---|---|
| ¿Quién la crea? | El crawler del lado origen / del lado destino / un crawler externo |
| ¿En qué fase del Engine? | Fase 1 (durante IngestNodes) / Fase 2 (durante IngestRelations) |
| ¿Es estable o cambia? | Estable (no requiere rebuild) / Cambia (requiere rebuild) |
| ¿Qué helper usar? | `rebuildBelongsTo`, `rebuildDeployedIn`, etc. — o nuevo helper |

Si necesitas un helper nuevo, especifícalo:

\```go
// rebuildAttachedPolicy reemplaza relaciones ATTACHED_POLICY salientes.
func rebuildAttachedPolicy(ctx, session, sourceArn, policyArns []string) error
\```

### 5. Eventos del worker — handler y registros

Especifica el archivo `internal/events/handlers/{servicio}.go`.

**Struct y constructor:**
\```go
type IAMHandler struct {
    crawler *crawler.IAMCrawler
}
func NewIAMHandler(crawler *crawler.IAMCrawler) *IAMHandler
\```

**Métodos:**

Especifica cuántos métodos `Handle*` necesita y qué eventos cubre cada uno. Para servicios con múltiples tipos de recursos, puede haber varios:

\```
HandleRoleChange (CreateRole, UpdateRole, AttachRolePolicy, DetachRolePolicy, ...)
HandleRoleDelete (DeleteRole)
HandlePolicyChange (CreatePolicy, CreatePolicyVersion, SetDefaultPolicyVersion)
HandlePolicyDelete (DeletePolicy)
\```

**Helper de extracción:**

Para cada tipo de recurso, especifica cómo se extrae el identificador del payload. Reusa el patrón:

\```go
func extractRoleName(event *events.EventBridgeMessage) (string, error)
\```

Especifica de qué campo del payload sale (`requestParameters.roleName`, `responseElements.role.arn`, etc.) basado en la sección 5 del research.

**Registros en `cmd/worker/main.go`:**

Lista exactamente las líneas a agregar:

\```go
// IAM Roles
router.Register("CreateRole", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("UpdateRole", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("AttachRolePolicy", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("DetachRolePolicy", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("DeleteRole", events.HandlerFunc(iamHandler.HandleRoleDelete))
// ... etc
\```

### 6. Actualización de infraestructura Terraform

El worker solo recibe eventos que matcheen el `event_pattern` de la EventBridge rule en `infra/terraform/main.tf`. Agregar handlers en Go no es suficiente — la rule también debe contemplar el nuevo servicio.

Especifica exactamente qué hay que agregar al recurso `aws_cloudwatch_event_rule.worker_rule`:

**Source nuevos a agregar:**

```hcl
source = ["aws.ec2", "aws.lambda", "aws.s3", "aws.sqs", "aws.{servicio}"]
```

**EventNames nuevos a agregar:**

Lista exactamente los eventNames que el handler nuevo registra, con un comentario de bloque que separe del resto:

```hcl
# {Servicio} - sub-recurso 1
"CreateX", "UpdateX", "DeleteX",
# {Servicio} - sub-recurso 2
"CreateY", "DeleteY",
```

**Cambios al `MessageRetentionPeriod` o `VisibilityTimeout`:** Solo si el nuevo servicio tiene eventos que requieren procesamiento más lento (ej. handlers que hacen N+1 calls). Por defecto, no se cambian.

**Permisos IAM nuevos para el worker:** Lista los permisos `iam:Get*`, `iam:List*` que el worker necesita ejecutar (basado en research §7).

Después de aplicar los cambios:

```bash
cd infra/terraform
terraform plan
terraform apply
```

Y verifica con:

```bash
aws events describe-rule --name infra-diagram-resource-changes \
  --query 'EventPattern' --output text | jq .
```

### 7. Decisiones de implementación a confirmar

Lista las decisiones que NO están en el research pero requieren respuesta del usuario antes de implementar. Ejemplos típicos:

- "¿El IAMCrawler maneja Roles y Policies en un solo struct, o son dos crawlers separados?"
- "¿`AttachedPolicyRelation` es un struct nuevo o reusamos `BelongsToRelation`?"
- "Para la relación Role→EC2, ¿iteramos las instances del grafo via `MATCH (e:EC2Instance)` o llamamos `DescribeInstances` en el crawler?"

NO inventes la decisión — preséntala con tradeoff y espera input.

### 8. Checklist de archivos a crear/modificar

Lista exhaustiva de los archivos que el siguiente SKILL (implementation) tocará:

- [ ] `internal/models/nodes.go` — agregar structs `IAMRole`, `IAMPolicy`
- [ ] `internal/models/relations.go` — agregar struct `AttachedPolicyRelation`
- [ ] `internal/crawler/iam.go` — crear (nuevo)
- [ ] `internal/crawler/helpers.go` — agregar `rebuildAttachedPolicy`
- [ ] `internal/events/handlers/iam.go` — crear (nuevo)
- [ ] `cmd/discover/main.go` — agregar `IAMCrawler` a la lista
- [ ] `cmd/worker/main.go` — registrar los handlers
- [ ] `internal/graph/setup.go` — agregar índices: `CREATE INDEX FOR (n:IAMRole) ON (n.arn)` etc.
- [ ] `infra/terraform/main.tf` — agregar `aws.{servicio}` al `source` y los eventNames nuevos al filtro

## Reglas del diseño

1. **Sigue el patrón existente.** Si el proyecto siempre usa `IngestSingleWithDriver` como wrapper, tu diseño debe usarlo. No inventes patrones nuevos sin justificación.

2. **Especificidad sobre teoría.** "El crawler usa cache para evitar dobles llamadas" no sirve. "El crawler tiene `rolesOnce sync.Once` y método `loadRoles(ctx) ([]iamtypes.Role, error)` cacheado" sí sirve.

3. **Cita el research constantemente.** Cada decisión debe poder rastrearse a una sección del research. "Según research §4, esta relación es N-a-N estable, por eso..."

4. **Pregunta antes de asumir** decisiones grandes. Si el servicio tiene 3 tipos de recursos y el research solo decidió 2 explícitamente, pregunta sobre el tercero.

5. **No escribas el código completo.** Esta fase es diseño — solo signatures, structs, y decisiones. El código completo viene en la siguiente fase.

6. **Considera el orden de ingesta.** Si las relaciones del servicio dependen de nodos de otro crawler (ej. Role→EC2 necesita que EC2 exista), nóta esto en la sección de estrategia de relaciones.

## Ejemplo de output

Mira `examples/sqs-design.md` para un ejemplo completo del documento de diseño aplicado a SQS (servicio simple ya implementado).

## Cómo proceder

1. Pregunta al usuario qué servicio quiere diseñar (si no lo dijo).
2. **Lee el archivo de research** correspondiente (`docs/research/{servicio}-research.md`). Si no existe o le falta sección 9, detente y pídelo.
3. **Lee los archivos del proyecto** mencionados en "Contexto del proyecto" para entender el patrón actual.
4. Produce el documento siguiendo la estructura de arriba.
5. Presenta las decisiones de implementación a confirmar (sección 6) y **espera respuestas del usuario**.
6. Una vez confirmadas, **actualiza el documento agregando una sección final "8. Decisiones de implementación tomadas"** con las respuestas y un breve razonamiento.
7. Sugiere guardar el documento en `docs/design/{servicio}-design.md`.

El archivo final debe ser self-contained y suficientemente específico para que el siguiente SKILL (implementation) pueda escribir el código sin necesidad de inventar decisiones.

## Cambios iterativos

- 2026-05: Agregado paso 6 (Terraform updates). Descubierto durante el diseño de IAM
  — sin esto el worker nunca recibe eventos del nuevo servicio.