# Research: AWS IAM

## 1. Identidad del servicio

- **Nombre:** AWS Identity and Access Management (IAM)
- **SDK package:** `github.com/aws/aws-sdk-go-v2/service/iam`
- **Tipo de recursos:** Identidades y políticas de acceso (roles, users, groups, policies, instance profiles)
- **Regional:** **No — IAM es un servicio global.** El cliente se inicializa una sola vez (sin región), no en el loop por región que usan EC2/Lambda/SQS. Los ARNs no tienen campo de región: `arn:aws:iam::{accountId}:role/...`

## 2. Recursos a modelar

### IAMRole

- **Label sugerido:** `IAMRole`
- **API bulk:** `iam.ListRoles`
- **API individual:** `iam.GetRole` (input: `RoleName`)
- **Paginado:** Sí — marker-based. Respuesta tiene `IsTruncated bool` + `Marker *string`. Paginator: `NewListRolesPaginator`
- **Identificador natural:** ARN
- **Formato ARN:** `arn:aws:iam::{accountId}:role/{roleName}` (service-linked roles: `arn:aws:iam::{accountId}:role/aws-service-role/{service}/{roleName}`)

---

### IAMUser

- **Label sugerido:** `IAMUser`
- **API bulk:** `iam.ListUsers`
- **API individual:** `iam.GetUser` (input: `UserName`)
- **Paginado:** Sí — marker-based. Paginator: `NewListUsersPaginator`
- **Identificador natural:** ARN
- **Formato ARN:** `arn:aws:iam::{accountId}:user/{userName}` (con path: `arn:aws:iam::{accountId}:user/{path}/{userName}`)

---

### IAMGroup

- **Label sugerido:** `IAMGroup`
- **API bulk:** `iam.ListGroups`
- **API individual:** `iam.GetGroup` (input: `GroupName`) — respuesta incluye el array de `Users` miembros
- **Paginado:** Sí — marker-based. Paginator: `NewListGroupsPaginator`
- **Identificador natural:** ARN
- **Formato ARN:** `arn:aws:iam::{accountId}:group/{groupName}`

---

### IAMPolicy (solo customer managed — ver Decisiones abiertas)

- **Label sugerido:** `IAMPolicy`
- **API bulk:** `iam.ListPolicies` con `Scope: "Local"` para solo customer managed
- **API individual:** `iam.GetPolicy` (input: `PolicyArn`)
- **Paginado:** Sí — marker-based. Paginator: `NewListPoliciesPaginator`
- **Identificador natural:** ARN
- **Formato ARN:** `arn:aws:iam::{accountId}:policy/{policyName}` (AWS managed: `arn:aws:iam::aws:policy/{policyName}`, account vacío)

---

## 3. Propiedades a extraer

### IAMRole

| Nombre en SDK | Tipo Go | Nullable | Nota |
|---|---|---|---|
| `RoleName` | `*string` | No | Nombre amigable |
| `RoleId` | `*string` | No | ID estable (prefijo `AROA`) |
| `Arn` | `*string` | No | Identificador principal |
| `Path` | `*string` | No | Path organizacional |
| `CreateDate` | `*time.Time` | No | Guardar como ISO 8601 string |
| `Description` | `*string` | Sí | Puede venir nil |
| `MaxSessionDuration` | `*int32` | Sí | En segundos (3600–43200) |

NO incluir: `AssumeRolePolicyDocument` (documento JSON grande, URL-encoded), `RoleLastUsed` (cambia con cada uso), `Tags` (por ahora), inline policies (decisión abierta).

---

### IAMUser

| Nombre en SDK | Tipo Go | Nullable | Nota |
|---|---|---|---|
| `UserName` | `*string` | No | |
| `UserId` | `*string` | No | ID estable (prefijo `AIDA`) |
| `Arn` | `*string` | No | |
| `Path` | `*string` | No | |
| `CreateDate` | `*time.Time` | No | Guardar como ISO 8601 string |
| `PasswordLastUsed` | `*time.Time` | Sí | Solo en `GetUser`/`ListUsers`; nil si nunca usó password |

NO incluir: `PermissionsBoundary`, `Tags`.

---

### IAMGroup

| Nombre en SDK | Tipo Go | Nullable | Nota |
|---|---|---|---|
| `GroupName` | `*string` | No | |
| `GroupId` | `*string` | No | ID estable (prefijo `AGPA`) |
| `Arn` | `*string` | No | |
| `Path` | `*string` | No | |
| `CreateDate` | `*time.Time` | No | Guardar como ISO 8601 string |

---

### IAMPolicy (customer managed)

| Nombre en SDK | Tipo Go | Nullable | Nota |
|---|---|---|---|
| `PolicyName` | `*string` | No | |
| `PolicyId` | `*string` | No | ID estable (prefijo `ANPA`) |
| `Arn` | `*string` | No | |
| `Path` | `*string` | No | |
| `Description` | `*string` | Sí | Solo en `GetPolicy`, no en `ListPolicies` |
| `CreateDate` | `*time.Time` | Sí | |
| `UpdateDate` | `*time.Time` | Sí | |
| `AttachmentCount` | `*int32` | Sí | Número de principals adjuntos |

NO incluir: documento de política (`GetPolicyVersion`), `DefaultVersionId`, `IsAttachable`.

---

## 4. Relaciones identificadas

### `IAMRole -[:INSTANCE_PROFILE_OF]-> EC2Instance`
- **Cómo descubrirla:** `ec2.DescribeInstances` → `Instances[].IamInstanceProfile.Arn`. El ARN es del instance profile (no del role directamente). Luego `iam.GetInstanceProfile` para resolver el role ARN.
- **Alternativa más simple:** `iam.ListInstanceProfiles` → `InstanceProfile.Roles[0]` + matchear con las instancias EC2 del grafo por `iamInstanceProfile.arn`.
- **Cardinalidad:** N-a-1 (muchas instancias pueden usar el mismo role via instance profile; un instance profile tiene exactamente 1 role)
- **Estable:** Cambia con `AssociateIamInstanceProfile` / `DisassociateIamInstanceProfile`
- **Cubierta hoy:** No. EC2Crawler guarda `VpcId` y `SubnetId` pero no el instance profile. Esta relación puede manejarse en `IAMCrawler.IngestRelations` consultando las instancias existentes en el grafo o re-usando el EC2 client. **Requiere decisión del usuario.**

---

### `IAMRole -[:EXECUTES]-> Function`
- **Cómo descubrirla:** `lambda.ListFunctions` → `Functions[].Role` (ARN del execution role). Ya hay datos en el grafo de `FunctionNode`.
- **Cardinalidad:** N-a-1 (muchas lambdas pueden usar el mismo role; una lambda tiene exactamente 1 execution role)
- **Estable:** Cambia con `UpdateFunctionConfiguration`
- **Cubierta hoy:** No. LambdaCrawler guarda el ARN de la función pero no persiste la relación con el role. Esta relación puede manejarse en `IAMCrawler.IngestRelations` iterando las funciones Lambda del grafo.

---

### `IAMRole -[:ATTACHED_POLICY]-> IAMPolicy` (N-a-N)
- **Cómo descubrirla:** `iam.ListAttachedRolePolicies` (paginado) para cada role
- **Cardinalidad:** N-a-N (un role puede tener múltiples policies; una policy puede estar en múltiples roles)
- **Estable:** Cambia con `AttachRolePolicy` / `DetachRolePolicy`
- **Cubierta hoy:** No requiere otro crawler, pero sí que IAMPolicy esté modelada. **Ver Decisiones abiertas.**

---

### `IAMUser -[:MEMBER_OF]-> IAMGroup`
- **Cómo descubrirla:** `iam.GetGroup` → `Users[]` o `iam.ListGroupsForUser` por usuario
- **Cardinalidad:** N-a-N
- **Estable:** Cambia con `AddUserToGroup` / `RemoveUserFromGroup`
- **Cubierta hoy:** Dentro del scope de IAMCrawler si se modelan Groups.

---

### `IAMUser -[:ATTACHED_POLICY]-> IAMPolicy` (N-a-N)
- **Cómo descubrirla:** `iam.ListAttachedUserPolicies` para cada user
- **Cardinalidad:** N-a-N
- **Mismas consideraciones que Role→Policy.**

---

### `IAMGroup -[:ATTACHED_POLICY]-> IAMPolicy` (N-a-N)
- **Cómo descubrirla:** `iam.ListAttachedGroupPolicies` para cada group
- **Cardinalidad:** N-a-N

---

## 5. Eventos de CloudTrail relevantes

**Nota crítica:** IAM es global, pero CloudTrail registra todos los eventos IAM con `awsRegion: "us-east-1"` aunque la cuenta esté en otra región. El filtro de EventBridge debe contemplar esto o usar un trail global.

### HandleChange
| EventName | Identificador en el evento | Notas |
|---|---|---|
| `CreateRole` | `responseElements.role.arn` | Viene en responseElements |
| `UpdateRole` | `requestParameters.roleName` | Hay que construir el ARN |
| `UpdateAssumeRolePolicy` | `requestParameters.roleName` | Modifica el trust policy |
| `AttachRolePolicy` | `requestParameters.roleName` | Relación role→policy cambia |
| `DetachRolePolicy` | `requestParameters.roleName` | Relación role→policy cambia |
| `PutRolePolicy` | `requestParameters.roleName` | Inline policy |
| `CreateUser` | `responseElements.user.arn` | |
| `UpdateUser` | `requestParameters.userName` | |
| `AttachUserPolicy` | `requestParameters.userName` | |
| `DetachUserPolicy` | `requestParameters.userName` | |
| `CreateGroup` | `responseElements.group.arn` | |
| `UpdateGroup` | `requestParameters.groupName` | |
| `AddUserToGroup` | `requestParameters.groupName` + `userName` | Relación membership |
| `RemoveUserFromGroup` | `requestParameters.groupName` + `userName` | |
| `CreatePolicy` | `responseElements.policy.arn` | |
| `CreatePolicyVersion` | `requestParameters.policyArn` | Nueva versión default |
| `SetDefaultPolicyVersion` | `requestParameters.policyArn` | |

### HandleDelete
| EventName | Identificador en el evento | Notas |
|---|---|---|
| `DeleteRole` | `requestParameters.roleName` | ARN debe construirse o buscarse en grafo |
| `DeleteUser` | `requestParameters.userName` | |
| `DeleteGroup` | `requestParameters.groupName` | |
| `DeletePolicy` | `requestParameters.policyArn` | ARN viene directo |

### NO manejar
- `GetRole`, `ListRoles`, `GetUser`, `ListUsers`, etc. — eventos de lectura
- `CreateLoginProfile`, `UpdateLoginProfile` — credenciales de consola, no infraestructura
- `CreateAccessKey`, `DeleteAccessKey` — credenciales programáticas, no infraestructura
- `AssumeRole`, `AssumeRoleWithWebIdentity` — uso del role, no su configuración
- `DeleteRolePolicy`, `DeleteUserPolicy` — eliminación de inline policies (ver Decisiones abiertas)

### Sobre `Resources[]`
Los eventos IAM **sí suelen poplar `resources[]`** con el ARN del recurso afectado. Sin embargo, para eventos que operan por nombre (ej. `DeleteRole` con `requestParameters.roleName`), el ARN no siempre está disponible directamente y puede necesitar construirse como `arn:aws:iam::{accountId}:role/{roleName}`.

---

## 6. Edge cases y quirks

- **IAM es global, no regional.** El `iam.NewFromConfig` no toma región. A diferencia de todos los otros crawlers del proyecto (que iteran por región), IAM se llama una sola vez. El crawler necesita un constructor distinto.

- **ARN sin región.** Formato: `arn:aws:iam::{accountId}:role/{name}`. El quinto segmento (región) está vacío. Cualquier parsing de ARN que asuma 6 segmentos con región puede fallar.

- **Los eventos CloudTrail de IAM llegan con `awsRegion: "us-east-1"`** aunque el trail sea multi-región. Si el EventBridge rule filtra por región, puede perder eventos IAM en cuentas que no tengan us-east-1 configurado.

- **DeleteRole en CloudTrail solo da el nombre, no el ARN.** `requestParameters.roleName` = `"my-role"`. Para el `HandleDelete`, el ARN se puede construir como `fmt.Sprintf("arn:aws:iam::%s:role/%s", accountId, roleName)`, o buscar en el grafo por nombre antes de borrar.

- **Los nombres de recursos IAM pueden incluir paths** (ej. `/division/team/my-role`). El ARN incluye el path: `arn:aws:iam::123456789012:role/division/team/my-role`. Los eventos CloudTrail usan solo el nombre base sin path en `requestParameters.roleName`.

- **`AssumeRolePolicyDocument` viene URL-encoded** en las respuestas de `GetRole`. Hay que hacer `url.QueryUnescape()` antes de parsear el JSON. No se recomienda guardar este campo como propiedad Neo4j dado su tamaño.

- **Roles de servicio vinculados (service-linked roles)** no pueden ser modificados ni eliminados directamente en muchos casos. Sus ARNs tienen el formato especial: `arn:aws:iam::{accountId}:role/aws-service-role/...`. Son válidos como nodos IAMRole.

- **`iam.ListPolicies` sin `Scope: "Local"`** devuelve también las ~1000+ AWS managed policies. Siempre usar `Scope: types.PolicyScopeTypeLocal` para discovery de customer managed policies.

- **`AttachmentCount` en IAMPolicy** puede ser 0 si la policy existe pero no está adjunta a nada. No es un error.

- **Paginación marker-based (no token-based como Lambda/SQS).** El SDK tiene paginators (`NewListRolesPaginator`, `NewListUsersPaginator`, etc.) que manejan `IsTruncated` + `Marker` automáticamente.

---

## 7. Permisos IAM mínimos

```
iam:ListRoles
iam:GetRole
iam:ListUsers
iam:GetUser
iam:ListGroups
iam:GetGroup
iam:ListPolicies
iam:GetPolicy
iam:ListAttachedRolePolicies
iam:ListAttachedUserPolicies
iam:ListAttachedGroupPolicies
iam:ListInstanceProfiles
iam:GetInstanceProfile
```

Para las relaciones con EC2 e Lambda (si se manejan desde IAMCrawler):
```
ec2:DescribeInstances   (o reusar el EC2 client ya existente)
lambda:ListFunctions    (o reusar el Lambda client ya existente)
```

## 8 Decisiones abiertas

1. **¿Modelamos Users y Groups en el MVP o solo Roles?**
Los roles son los recursos más relevantes para un infra-diagram (EC2, Lambda, servicios AWS los usan). Users y Groups son más relevantes para auditoría de acceso humano. ¿Están en scope?

2. **¿Modelamos IAMPolicy como nodo o solo como relación con propiedades?**
Opción A: Nodo `IAMPolicy` con sus propiedades + relaciones `ATTACHED_POLICY`.
Opción B: Solo agregar el ARN de policies como propiedad del Role/User (sin nodo separado).
¿Cuál preferís?

3. **¿Incluimos AWS managed policies (`arn:aws:iam::aws:policy/...`) o solo customer managed?**
Las AWS managed son ~1000+ y no cambian. Incluirlas llenaría el grafo de nodos poco útiles.
Recomendación: solo `Scope: "Local"` (customer managed). ¿Confirmar?

4. **¿Inline policies como nodos o ignorarlas?**
Las inline policies no tienen ARN propio y están embebidas en el principal.
Opciones:
- Ignorarlas
- Guardar sus nombres como propiedad `[]string` en el Role/User
- Modelarlas como nodo con label `IAMInlinePolicy`
¿Cuál preferís?

5. **¿La relación `IAMRole → EC2Instance` la maneja IAMCrawler o EC2Crawler?**
Ambas estrategias son válidas.
IAMCrawler puede iterar instance profiles; EC2Crawler ya tiene los datos de `IamInstanceProfile`.
¿Cuál tiene más sentido para el proyecto?

6. **¿La relación `IAMRole → Lambda Function` la maneja IAMCrawler o LambdaCrawler?**
Similar al caso anterior.
LambdaCrawler ya lista funciones y tiene el campo `Role` en cada una.
¿Lo movemos allí o lo centraliza IAMCrawler?

## 9. Decisiones tomadas

1. **Users y Groups:** Excluidos del MVP. Solo se modelan Roles.
   _Razón: el proyecto modela infraestructura, no acceso humano._

2. **IAMPolicy:** Modelada como nodo separado, no como propiedad.
   _Razón: relaciones N-a-N son consultables; propiedades string array no lo son._

3. **AWS managed policies:** Excluidas. Solo customer managed (`Scope: "Local"`).
   _Razón: las ~1000 AWS managed son ruido visual y no aportan info sobre la cuenta._

4. **Inline policies:** Fuera del MVP. Documentar como limitación.
   _Razón: requieren llamadas API extra y son menos comunes en IaC moderno._

5. **Relación `Role → EC2Instance`:** Manejada por IAMCrawler.
   _Razón: evita que EC2Crawler tenga que crear nodos IAMRole ghost. Costo aceptado: IAMCrawler hace `DescribeInstances`._

6. **Relación `Role → Function`:** Manejada por IAMCrawler.
   _Razón: misma lógica que el caso anterior. Costo aceptado: IAMCrawler hace `ListFunctions`._

## Recursos en MVP

- IAMRole
- IAMPolicy (solo customer managed)

## Relaciones en MVP

- `IAMRole -[:ATTACHED_POLICY]-> IAMPolicy`
- `IAMRole -[:INSTANCE_PROFILE_OF]-> EC2Instance`
- `IAMRole -[:EXECUTES]-> Function`