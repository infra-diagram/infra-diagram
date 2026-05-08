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
HandleRoleChange (CreateRole,