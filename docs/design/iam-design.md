# Diseño: AWS IAM Crawler

## 1. Modelos de nodos

Ambos nodos no tienen campo `Region` — IAM es global (research §1, §6).

### IAMRole

```go
// internal/models/nodes.go
type IAMRole struct {
	AWSResource
	RoleName           string `neo4j:"roleName"`
	RoleId             string `neo4j:"roleId"`
	Path               string `neo4j:"path"`
	CreateDate         string `neo4j:"createDate"`        // ISO 8601
	Description        string `neo4j:"description"`       // vacío si nil en SDK
	MaxSessionDuration int32  `neo4j:"maxSessionDuration"` // segundos, 0 si nil
}
```

**Campos excluidos:** `AssumeRolePolicyDocument` (JSON URL-encoded, grande), `RoleLastUsed` (cambia con cada `AssumeRole`, no es estado de infraestructura), `Tags`.

### IAMPolicy

```go
// internal/models/nodes.go
type IAMPolicy struct {
	AWSResource
	PolicyName      string `neo4j:"policyName"`
	PolicyId        string `neo4j:"policyId"`
	Path            string `neo4j:"path"`
	Description     string `neo4j:"description"`    // vacío en bulk; poblado en IngestSinglePolicy
	CreateDate      string `neo4j:"createDate"`     // ISO 8601
	UpdateDate      string `neo4j:"updateDate"`     // ISO 8601
	AttachmentCount int32  `neo4j:"attachmentCount"` // 0 si sin attachments
}
```

**Nota:** `Description` solo viene en `GetPolicy`, no en `ListPolicies` (research §3). En `ingestPolicyNodes` (bulk) queda vacío. `IngestSinglePolicy` llama `GetPolicy` y lo popula.

**Campos excluidos:** `DefaultVersionId`, `IsAttachable`, documento de política (research §3).

---

## 2. Modelos de relaciones

### AttachedPolicyRelation

```go
// internal/models/relations.go

// (:IAMRole)-[:ATTACHED_POLICY]->(:IAMPolicy)
// Fuente: iam.ListAttachedRolePolicies
type AttachedPolicyRelation struct {
	RoleArn   string `neo4j:"roleArn"`
	PolicyArn string `neo4j:"policyArn"`
}
```

No reutilizamos `BelongsToRelation` — su nomenclatura Child/Parent es semánticamente incorrecta para esta relación N-a-N.

### InstanceProfileOfRelation

```go
// (:IAMRole)-[:INSTANCE_PROFILE_OF]->(:EC2Instance)
// Fuente: ec2.DescribeInstances → IamInstanceProfile + iam.GetInstanceProfile
type InstanceProfileOfRelation struct {
	RoleArn     string `neo4j:"roleArn"`
	InstanceArn string `neo4j:"instanceArn"`
}
```

### ExecutesRelation

```go
// (:IAMRole)-[:EXECUTES]->(:Function)
// Fuente: lambda.ListFunctions → Role field
type ExecutesRelation struct {
	RoleArn     string `neo4j:"roleArn"`
	FunctionArn string `neo4j:"functionArn"`
}
```

---

## 3. Estructura del crawler

### Struct y dependencias

```go
// internal/crawler/iam.go
type IAMCrawler struct {
	iamClient *iam.Client

	// Cache de roles: usada por IngestNodes (Fase 1) e IngestRelations (Fase 2).
	// sync.Once garantiza una sola llamada a ListRoles aunque ambas fases corran.
	rolesOnce sync.Once
	roles     []iamtypes.Role
	rolesErr  error
}
```

IAMCrawler no necesita `ec2Client` ni `lambdaClient` (Decisión 2, Option A): las relaciones cross-service se descubren leyendo el grafo, donde EC2Crawler y LambdaCrawler ya persisten `iamInstanceProfileArn` y `executionRoleArn`.

### Constructor

```go
func NewIAMCrawler(iamClient *iam.Client) *IAMCrawler {
	return &IAMCrawler{iamClient: iamClient}
}
```

### Métodos públicos

| Método | Firma | Hace |
|---|---|---|
| `IngestNodes` | `(ctx, driver) error` | Llama `ingestRoleNodes` + `ingestPolicyNodes` en secuencia |
| `IngestSingleRole` | `(ctx, session, roleName string) error` | Re-fetcha via `GetRole` y hace upsert del nodo |
| `IngestSingleRoleWithDriver` | `(ctx, driver, roleName string) error` | Wrapper que abre sesión |
| `IngestSinglePolicy` | `(ctx, session, policyArn string) error` | Re-fetcha via `GetPolicy` y hace upsert del nodo |
| `IngestSinglePolicyWithDriver` | `(ctx, driver, policyArn string) error` | Wrapper que abre sesión |
| `IngestRelations` | `(ctx, driver) error` | Llama los 3 bulk relation builders |
| `RebuildRelations` | `(ctx, session, roleName string) error` | Recrea solo ATTACHED_POLICY de un rol (ver §4 decisión de scope) |
| `RebuildRelationsWithDriver` | `(ctx, driver, roleName string) error` | Wrapper que abre sesión |
| `DeleteRoleByArn` | `(ctx, driver, arn string) error` | DETACH DELETE `(:IAMRole {arn})` |
| `DeletePolicyByArn` | `(ctx, driver, arn string) error` | DETACH DELETE `(:IAMPolicy {arn})` |

**Por qué dos recursos en un solo crawler:** roles y policies están acoplados por `ATTACHED_POLICY`. Separarlos en dos crawlers crea dependencia de orden: IAMPolicyCrawler debe correr antes que IAMRoleCrawler en la Fase 1 para que los nodos existan al crear relaciones. Un solo crawler controla ese orden internamente.

**Por qué `IngestSingleRole`/`IngestSinglePolicy` en vez de `IngestSingle`:** IAM tiene dos tipos de recursos con identificadores diferentes (`roleName` string vs `policyArn` ARN). Forzar ambos a una firma genérica sería menos legible.

### Métodos privados auxiliares

| Método | Hace |
|---|---|
| `loadRoles(ctx)` | `sync.Once`: llama `ListRoles` paginado y cachea el slice |
| `ingestRoleNodes(ctx, session)` | Itera `loadRoles`, llama `upsertNodeFromRole` por cada rol |
| `ingestPolicyNodes(ctx, session)` | Pagina `ListPolicies(Scope=Local)`, llama `upsertNodeFromPolicy` |
| `upsertNodeFromRole(ctx, session, role)` | Cypher MERGE para un `iamtypes.Role` ya fetcheado |
| `upsertNodeFromPolicy(ctx, session, policy)` | Cypher MERGE para un `iamtypes.Policy` ya fetcheado |
| `fetchRole(ctx, roleName)` | Llama `iam.GetRole` → devuelve `iamtypes.Role` |
| `fetchPolicy(ctx, policyArn)` | Llama `iam.GetPolicy` → devuelve `iamtypes.Policy` |
| `rebuildRoleRelations(ctx, session, role)` | Helper interno de `RebuildRelations`: llama `ListAttachedRolePolicies` y usa `rebuildAttachedPolicies` |
| `ingestAttachedPolicyRelationsBulk(ctx, driver)` | Para cada rol: `ListAttachedRolePolicies` + MERGE ATTACHED_POLICY |
| `ingestInstanceProfileRelationsBulk(ctx, driver)` | `MATCH (e:EC2Instance)` en grafo + `GetInstanceProfile` por profile ARN + MERGE INSTANCE_PROFILE_OF |
| `ingestExecutesRelationsBulk(ctx, driver)` | `MATCH (f:Function)` en grafo + MERGE EXECUTES por `executionRoleArn` |

---

## 4. Estrategia para cada relación

### `IAMRole -[:ATTACHED_POLICY]-> IAMPolicy`

| Pregunta | Decisión |
|---|---|
| ¿Quién la crea? | IAMCrawler (ambos extremos son nodos IAM) |
| ¿En qué fase? | Fase 2 — `IngestRelations` (los nodos de Fase 1 deben existir) |
| ¿Es estable? | Cambia — `AttachRolePolicy` / `DetachRolePolicy` |
| ¿Rebuild incremental? | Sí — `RebuildRelations(ctx, session, roleName)` |
| Helper | Nuevo: `rebuildAttachedPolicies` (ver abajo) |

**Lógica de bulk (`ingestAttachedPolicyRelationsBulk`):**
1. Iterar `loadRoles` (cache ya poblado en Fase 1).
2. Para cada rol: paginar `iam.ListAttachedRolePolicies(roleName)`.
3. Por cada policy adjunta: si el ARN NO contiene `arn:aws:iam::aws:policy/` (filtro AWS managed), llamar `rebuildAttachedPolicies`.
4. El MERGE falla silenciosamente si el nodo `IAMPolicy` no existe (p.ej. la policy fue borrada entre discovery y esta fase) — aceptable.

**Lógica de rebuild (`RebuildRelations`):**
1. `fetchRole(roleName)` → obtener ARN del rol.
2. `ListAttachedRolePolicies(roleName)` paginated → lista de policy ARNs.
3. Filtrar AWS managed.
4. `rebuildAttachedPolicies(ctx, session, roleArn, policyArns)`.

---

### `IAMRole -[:INSTANCE_PROFILE_OF]-> EC2Instance`

| Pregunta | Decisión |
|---|---|
| ¿Quién la crea? | IAMCrawler (research §9.5) |
| ¿En qué fase? | Fase 2 — `IngestRelations` (EC2Crawler debe haber corrido en Fase 1) |
| ¿Es estable? | Cambia — `AssociateIamInstanceProfile` / `DisassociateIamInstanceProfile` |
| ¿Rebuild incremental? | Sí — **EC2Handler** lo maneja (Decisión 1, Option E) |
| Helper | Nuevo: `rebuildInstanceProfileOf` |

**Lógica de bulk (`ingestInstanceProfileRelationsBulk`):**

EC2Crawler persiste `iamInstanceProfileArn` en `EC2Instance` (Decisión 2, Option A):
1. `MATCH (e:EC2Instance) WHERE e.iamInstanceProfileArn <> "" RETURN e.arn, e.iamInstanceProfileArn`.
2. Para cada instancia: extraer el nombre del profile del ARN usando `GetIAMResourceNameFromArn` (`arn:aws:iam::{account}:instance-profile/{name}` → último segmento).
3. `iam.GetInstanceProfile(profileName)` → `InstanceProfile.Roles[0].Arn` = ARN del rol (sub-opción A1 confirmada).
4. Agrupar por rol: `roleArn → []instanceArn`.
5. Por cada rol: `rebuildInstanceProfileOf(ctx, session, roleArn, instanceArns)`.

**Lógica de rebuild incremental (EC2Handler — Decisión 1, Option E):**

EC2Handler extiende su `HandleChange` para los eventos `RunInstances` y `AssociateIamInstanceProfile`:
1. Después de upsert del nodo EC2, si la instancia tiene `iamInstanceProfileArn`:
   - `GetInstanceProfile(profileName)` → role ARN.
   - `rebuildInstanceProfileOf(ctx, session, roleArn, []string{instanceArn})`.
2. Para `DisassociateIamInstanceProfile`:
   - Borrar `INSTANCE_PROFILE_OF` entrantes a la instancia (Cypher: `MATCH ()-[r:INSTANCE_PROFILE_OF]->(e:EC2Instance {arn: $arn}) DELETE r`).

---

### `IAMRole -[:EXECUTES]-> Function`

| Pregunta | Decisión |
|---|---|
| ¿Quién la crea? | IAMCrawler (research §9.6) |
| ¿En qué fase? | Fase 2 — `IngestRelations` (LambdaCrawler debe haber corrido en Fase 1) |
| ¿Es estable? | Cambia — `UpdateFunctionConfiguration` |
| ¿Rebuild incremental? | Sí — **LambdaHandler** lo maneja (Decisión 1, Option E) |
| Helper | Nuevo: `rebuildExecutes` |

**Lógica de bulk (`ingestExecutesRelationsBulk`):**

LambdaCrawler persiste `executionRoleArn` en `Function` — campo `fn.Role` del SDK (Decisión 2, Option A):
1. `MATCH (f:Function) WHERE f.executionRoleArn <> "" RETURN f.arn, f.executionRoleArn`.
2. Agrupar por rol: `roleArn → []functionArn`.
3. Por cada rol: `rebuildExecutes(ctx, session, roleArn, functionArns)`.

**Lógica de rebuild incremental (LambdaHandler — Decisión 1, Option E):**

LambdaHandler extiende `HandleChange` para `CreateFunction*` y `UpdateFunctionConfiguration*`:
1. Después de upsert del nodo Function, si tiene `executionRoleArn`:
   - `rebuildExecutes(ctx, session, executionRoleArn, []string{functionArn})`.
2. Nota: una función solo tiene un execution role, pero el helper es multi-valor para consistencia con el patrón del proyecto.

---

### Helpers nuevos en `internal/crawler/relations.go`

```go
// rebuildAttachedPolicies reemplaza TODAS las relaciones ATTACHED_POLICY
// salientes del rol. Si policyArns está vacío, equivale a borrar todas.
func rebuildAttachedPolicies(ctx context.Context, session neo4j.SessionWithContext, roleArn string, policyArns []string) error

// rebuildExecutes reemplaza TODAS las relaciones EXECUTES salientes del rol.
func rebuildExecutes(ctx context.Context, session neo4j.SessionWithContext, roleArn string, functionArns []string) error

// rebuildInstanceProfileOf reemplaza TODAS las relaciones INSTANCE_PROFILE_OF
// salientes del rol.
func rebuildInstanceProfileOf(ctx context.Context, session neo4j.SessionWithContext, roleArn string, instanceArns []string) error
```

Todos siguen el patrón de `rebuildUsesSubnetMulti`: DELETE relaciones salientes existentes, luego MERGE las nuevas.

### Helper nuevo en `internal/crawler/helpers.go`

```go
// GetIAMResourceNameFromArn extrae el nombre de un recurso IAM desde su ARN.
// Maneja paths con múltiples segmentos (service-linked roles):
//   arn:aws:iam::123:role/my-role              → "my-role"
//   arn:aws:iam::123:role/aws-service-role/elb.amazonaws.com/my-role → "my-role"
//   arn:aws:iam::123:policy/my-policy          → "my-policy"
//   arn:aws:iam::123:instance-profile/my-prof  → "my-prof"
func GetIAMResourceNameFromArn(arn string) (string, error)
```

**IMPORTANTE:** `GetRegionFromArn` NO debe llamarse para ARNs de IAM — el segmento de región está vacío y la función retorna error (research §6). El crawler no necesita región.

`BuildArn` SÍ puede usarse con región vacía: `BuildArn("iam", "", accountId, "role", roleName)` produce `arn:aws:iam::{accountId}:role/{roleName}`.

---

## 5. Eventos del worker — handler y registros

### Struct y constructor

```go
// internal/events/handlers/iam.go
type IAMHandler struct {
	crawler *crawler.IAMCrawler
}

func NewIAMHandler(crawler *crawler.IAMCrawler) *IAMHandler {
	return &IAMHandler{crawler: crawler}
}
```

### Métodos Handle*

**`HandleRoleChange`** — cubre: `CreateRole`, `UpdateRole`, `UpdateAssumeRolePolicy`, `AttachRolePolicy`, `DetachRolePolicy`, `PutRolePolicy`

```
1. extractRoleName(event) → roleName
2. log "[EventName] re-fetching role: {roleName}"
3. session := driver.NewSession(...)
4. IngestSingleRole(ctx, session, roleName)      // upsert nodo
5. RebuildRelations(ctx, session, roleName)       // rebuild ATTACHED_POLICY (best effort)
```

Misma estructura que `LambdaHandler.HandleChange` (§LambdaHandler): abre sesión una sola vez, llama IngestSingle + RebuildRelations, logea RebuildRelations como non-fatal.

**`HandleRoleDelete`** — cubre: `DeleteRole`

```
1. roleName := event.Detail.RequestParameters["roleName"]
2. arn := fmt.Sprintf("arn:aws:iam::%s:role/%s", event.Account, roleName)
3. DeleteRoleByArn(ctx, driver, arn)
```

Nota: `event.Detail.PrimaryResourceARN()` puede no estar disponible en `DeleteRole` (research §5, §6). El ARN se construye desde `requestParameters.roleName` + `event.Account`.

**`HandlePolicyChange`** — cubre: `CreatePolicy`, `CreatePolicyVersion`, `SetDefaultPolicyVersion`

```
1. extractPolicyArn(event) → policyArn
2. log "[EventName] re-fetching policy: {policyArn}"
3. IngestSinglePolicyWithDriver(ctx, driver, policyArn)  // upsert nodo
   // No RebuildRelations: las relations ATTACHED_POLICY van del Role, no de la Policy
```

**`HandlePolicyDelete`** — cubre: `DeletePolicy`

```
1. policyArn = event.Detail.RequestParameters["policyArn"]  // viene directo (research §5)
2. DeletePolicyByArn(ctx, driver, policyArn)
```

### Extractores de identificadores

```go
// extractRoleName extrae el nombre del rol del payload.
// Estrategia: requestParameters.roleName primero (Update/Delete/Attach/Detach);
// si vacío, responseElements.role.roleName (Create).
func extractRoleName(event *events.EventBridgeMessage) (string, error)

// extractPolicyArn extrae el ARN de la policy.
// Estrategia: responseElements.policy.arn primero (Create);
// si vacío, requestParameters.policyArn (Update/Delete).
func extractPolicyArn(event *events.EventBridgeMessage) (string, error)
```

### Registros en `cmd/worker/main.go`

```go
// IAM — construir crawler e inicializar handler
iamClient := iam.NewFromConfig(awsCfg) // sin región — IAM es global
iamCrawler := crawler.NewIAMCrawler(iamClient)
iamHandler := handlers.NewIAMHandler(iamCrawler)

// IAM Roles
router.Register("CreateRole", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("UpdateRole", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("UpdateAssumeRolePolicy", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("AttachRolePolicy", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("DetachRolePolicy", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("PutRolePolicy", events.HandlerFunc(iamHandler.HandleRoleChange))
router.Register("DeleteRole", events.HandlerFunc(iamHandler.HandleRoleDelete))

// IAM Policies
router.Register("CreatePolicy", events.HandlerFunc(iamHandler.HandlePolicyChange))
router.Register("CreatePolicyVersion", events.HandlerFunc(iamHandler.HandlePolicyChange))
router.Register("SetDefaultPolicyVersion", events.HandlerFunc(iamHandler.HandlePolicyChange))
router.Register("DeletePolicy", events.HandlerFunc(iamHandler.HandlePolicyDelete))
```

**Nota:** `iam.NewFromConfig` no necesita región — no usar `config.WithRegion`. El cliente IAM es global.

---

## 6. Actualización de infraestructura Terraform

Archivo: `infra/terraform/main.tf` — recurso `aws_cloudwatch_event_rule.worker_rule`.

### Cambio 1 — agregar `"aws.iam"` al array `source`

```hcl
source = ["aws.ec2", "aws.lambda", "aws.s3", "aws.sqs", "aws.iam"]
```

### Cambio 2 — agregar eventos al array `eventName`

```hcl
eventName = [
  # EC2 / VPC / Subnet
  "RunInstances", "TerminateInstances",
  "CreateVpc", "DeleteVpc",
  "CreateSubnet", "DeleteSubnet",
  # EC2 — instance profile (Decisión 1, Option E: EC2Handler reconstruye INSTANCE_PROFILE_OF)
  "AssociateIamInstanceProfile", "DisassociateIamInstanceProfile",

  # Lambda
  "CreateFunction20150331", "DeleteFunction20150331",
  "UpdateFunctionConfiguration20150331v2",
  "CreateEventSourceMapping", "DeleteEventSourceMapping",
  "UpdateEventSourceMapping",

  # S3
  "CreateBucket", "DeleteBucket",
  "PutBucketNotificationConfiguration",

  # SQS
  "CreateQueue", "DeleteQueue",

  # IAM Roles
  "CreateRole", "UpdateRole",
  "UpdateAssumeRolePolicy",
  "AttachRolePolicy", "DetachRolePolicy",
  "PutRolePolicy",
  "DeleteRole",

  # IAM Policies
  "CreatePolicy",
  "CreatePolicyVersion", "SetDefaultPolicyVersion",
  "DeletePolicy"
]
```

**Nota:** los eventos IAM llegan en CloudTrail con `awsRegion: "us-east-1"` sin importar la región de la cuenta (research §6). Si la EventBridge rule tiene un filtro de región explícito, debe incluir `us-east-1` o la regla no capturará eventos IAM fuera de esa región.

---

## 7. Decisiones de implementación tomadas

### Decisión 1: Relaciones incrementales para `INSTANCE_PROFILE_OF` y `EXECUTES` → **Option E (híbrida)**

**Regla:** la propiedad del rebuild pertenece al lado que conoce el cambio.
- IAMCrawler maneja `INSTANCE_PROFILE_OF` y `EXECUTES` **solo en bulk** (`IngestRelations`).
- **EC2Handler** extiende su `HandleChange` para reconstruir `INSTANCE_PROFILE_OF` en eventos `RunInstances` y `AssociateIamInstanceProfile`; registra también `DisassociateIamInstanceProfile` para limpiar la relación.
- **LambdaHandler** extiende su `HandleChange` para reconstruir `EXECUTES` en eventos `CreateFunction*` y `UpdateFunctionConfiguration*`.

**Por qué:** EC2 sabe cuándo cambia su instance profile; Lambda sabe cuándo cambia su execution role. Poner el rebuild en el handler del servicio que origina el evento elimina la necesidad de un segundo handler IAM para esos eventos, y evita el coupling de pasar `iamCrawler` a EC2Handler/LambdaHandler.

**Implicación de implementación:** `rebuildInstanceProfileOf` y `rebuildExecutes` deben ser funciones exportadas del package `crawler` (no métodos del `IAMCrawler`) para que EC2Handler y LambdaHandler puedan llamarlas directamente pasando la sesión. O bien se exportan como métodos de IAMCrawler y los handlers reciben el crawler como dependencia. Ver §7 para el detalle.

---

### Decisión 2: Descubrimiento de relaciones cross-service → **Option A pura**

- `EC2Crawler` persiste `IamInstanceProfileArn string \`neo4j:"iamInstanceProfileArn"\`` en `EC2Instance`.
- `LambdaCrawler` persiste `ExecutionRoleArn string \`neo4j:"executionRoleArn"\`` en `Function`.
- `IAMCrawler` solo recibe `*iam.Client`. Sin `ec2Client` ni `lambdaClient`.
- Para descubrimiento bulk, IAMCrawler hace `MATCH` en el grafo y llama `GetInstanceProfile` para resolver profile ARN → role ARN (sub-opción A1).

---

## 8. Checklist de archivos a crear/modificar

### Nuevos

- [ ] `internal/crawler/iam.go` — IAMCrawler completo
- [ ] `internal/events/handlers/iam.go` — IAMHandler

### Modificar — modelos

- [ ] `internal/models/nodes.go` — agregar `IAMRole`, `IAMPolicy`; agregar `IamInstanceProfileArn` a `EC2Instance`, `ExecutionRoleArn` a `Function`
- [ ] `internal/models/relations.go` — agregar `AttachedPolicyRelation`, `InstanceProfileOfRelation`, `ExecutesRelation`

### Modificar — crawlers

- [ ] `internal/crawler/relations.go` — agregar `rebuildAttachedPolicies`, `rebuildExecutes`, `rebuildInstanceProfileOf`
- [ ] `internal/crawler/helpers.go` — agregar `GetIAMResourceNameFromArn`
- [ ] `internal/crawler/ec2.go` — persistir `IamInstanceProfile.Arn` como `iamInstanceProfileArn` en el upsert de `EC2Instance`
- [ ] `internal/crawler/lambda.go` — persistir `fn.Role` como `executionRoleArn` en el upsert de `Function`

### Modificar — handlers existentes (Decisión 1, Option E)

- [ ] `internal/events/handlers/ec2.go` — extender `HandleChange` para reconstruir `INSTANCE_PROFILE_OF` en `RunInstances` y `AssociateIamInstanceProfile`; nuevo `HandleDisassociateProfile` para `DisassociateIamInstanceProfile`
- [ ] `internal/events/handlers/lambda.go` — extender `HandleChange` para reconstruir `EXECUTES` después del upsert de la función

### Modificar — infraestructura

- [ ] `internal/graph/setup.go` — agregar índices:
  ```sql
  CREATE INDEX idx_iam_role_name IF NOT EXISTS FOR (n:IAMRole) ON (n.roleName)
  CREATE INDEX idx_iam_policy_name IF NOT EXISTS FOR (n:IAMPolicy) ON (n.policyName)
  ```
  (El índice por `arn` ya existe vía `idx_resource_arn` en `:AWSResource`. El índice `idx_resource_name` cubre `name`, pero IAMRole usa `roleName` — necesita índice propio.)
- [ ] `cmd/discover/main.go` — agregar `iam.NewFromConfig(awsCfg)` + `crawler.NewIAMCrawler(iamClient)` a la lista de crawlers
- [ ] `cmd/worker/main.go` — crear `iamCrawler` + `iamHandler` + registros del router; agregar registros para `AssociateIamInstanceProfile` y `DisassociateIamInstanceProfile` en EC2Handler
- [ ] `infra/terraform/main.tf` — agregar `"aws.iam"` al array `source`; agregar 11 eventos IAM + 2 eventos EC2 de instance profile al array `eventName` (ver §6)

---

## 9. Limitaciones conocidas del MVP

- **AWS managed policies no modeladas como nodos.** Si un rol tiene `AdministratorAccess` adjunta, la relación `ATTACHED_POLICY` no aparece en el grafo.
- **Inline policies excluidas** (research §9.4). Los roles con inline policies no muestran esas policies en el grafo.
- **Users y Groups excluidos** (research §9.1). Solo se modelan Roles.
- **`INSTANCE_PROFILE_OF` y `EXECUTES` no se reconstruyen cuando se elimina un rol.** `DeleteRoleByArn` usa `DETACH DELETE`, que borra relaciones salientes del rol. Pero si la instancia o función apuntaba al rol desde otro nodo, esas relaciones quedan huérfanas hasta el próximo discovery. (En la práctica, `DETACH DELETE` sobre el nodo rol limpia todas sus relaciones, por lo que esto no es un problema real.)
