---
name: aws-service-research
description: "Investigar un servicio de AWS para preparar su onboarding al proyecto infra-diagram (mapeo de recursos AWS a un grafo Neo4j). Activar cuando el usuario menciona agregar, integrar, onboardear, investigar o estudiar un servicio AWS para incluirlo en el grafo. Ejemplos: 'vamos a agregar IAM', 'investiguemos RDS', 'cómo onboardeamos DynamoDB', 'qué necesito saber de SNS para integrarlo'."
---

# AWS Service Research

Tu tarea es producir un **documento de investigación estructurado** sobre un servicio AWS específico, con la información necesaria para diseñar e implementar su crawler y handler en el proyecto infra-diagram.

Este documento es el input de la siguiente fase (diseño del crawler), así que debe ser concreto y accionable, no un overview general del servicio.

## Contexto del proyecto

infra-diagram es una herramienta en Go que descubre recursos AWS y los modela como grafo en Neo4j. Tiene dos modos:

- **Discovery (bulk):** corre periódicamente, lista todos los recursos de un servicio y los persiste.
- **Worker (eventos):** consume eventos de CloudTrail vía EventBridge → SQS y actualiza el grafo en tiempo real.

Cada servicio AWS soportado tiene:
- Un **crawler** (`internal/crawler/{servicio}.go`) con métodos `IngestNodes`, `IngestSingle`, `IngestRelations`, `RebuildRelations`, `DeleteByArn`.
- Un **handler** (`internal/events/handlers/{servicio}.go`) con `HandleChange` y `HandleDelete`.
- Modelos en `internal/models/nodes.go` y relaciones en `internal/models/relations.go`.

Servicios ya implementados: VPC, Subnet, EC2, Lambda, SQS, S3.

## Output: estructura del documento

Produce un archivo markdown con estas secciones EN ESTE ORDEN. No agregues secciones extra, no cambies los nombres.

### 1. Identidad del servicio

Una línea que diga:
- Nombre del servicio
- AWS SDK package que se importa en Go (ej. `github.com/aws/aws-sdk-go-v2/service/iam`)
- Qué tipo de recursos maneja (instancias, configs, policies, etc.)
- Si los recursos son regionales o globales (importante para clientes regionales)

### 2. Recursos a modelar

Lista los **tipos de nodos** que vamos a crear en Neo4j. Para cada uno:

- **Label sugerido** (ej. `IAMRole`, `IAMPolicy`)
- **API para listarlos** en bulk (ej. `iam.ListRoles`)
- **API para obtener uno por ID/nombre** (ej. `iam.GetRole`)
- **¿Es paginado?** (sí/no, qué paginador usa)
- **Identificador natural** (qué lo identifica únicamente: ARN, ID, nombre)
- **Formato del ARN** (literal, ej. `arn:aws:iam::{accountId}:role/{roleName}`)

Si un servicio tiene múltiples tipos de recursos relacionados (ej. IAM tiene Roles y Policies), lista cada uno por separado.

### 3. Propiedades a extraer

Para cada tipo de nodo, lista las **propiedades** que vamos a guardar en el nodo Neo4j. Solo incluye propiedades que:

- Sean estables (no cambian con cada llamada)
- Tengan valor para visualización o queries (ej. nombre, fecha de creación, runtime)
- NO sean transitorias (status como "pending"/"available" que cambian seguido)
- NO sean enormes (no metas el código de una Lambda como propiedad)

Para cada propiedad indica:
- Nombre en el SDK (ej. `RoleName`, `CreateDate`)
- Tipo Go (ej. `*string`, `*time.Time`)
- Si es nullable (en cuyo caso necesita nil check)

### 4. Relaciones identificadas

Esta es la sección más importante. Lista **todas** las relaciones que este servicio tiene con otros recursos AWS, incluyendo recursos que aún no están en el proyecto.

Para cada relación:

- **Dirección** (ej. `IAMRole -[:ATTACHED_TO]-> EC2Instance`)
- **Cómo descubrirla:** API call necesaria (`iam.ListAttachedRolePolicies`, `ec2.DescribeInstances` y mirar `IamInstanceProfile`, etc.)
- **Cardinalidad** (1-a-1, 1-a-N, N-a-N)
- **¿Es estable o cambia?** (si cambia, indica con qué evento — `AttachUserPolicy`, etc.)
- **¿Está cubierta hoy o requiere otro crawler?** Si requiere otro crawler que no existe, márcalo como "fuera de scope inicial" y déjalo para una fase posterior.

Si la relación es N-a-N o tiene atributos propios, considéralo y nóta que probablemente requiere modelado especial.

### 5. Eventos de CloudTrail relevantes

Lista los `eventName` exactos que CloudTrail emite para este servicio. Sepáralos en:

- **Eventos de creación/modificación** (van a `HandleChange`)
- **Eventos de eliminación** (van a `HandleDelete`)
- **Eventos que SÍ matchean nuestro filtro pero NO debemos manejar** (ej. eventos de lectura, eventos secundarios irrelevantes)

Para cada evento:
- Nombre exacto incluyendo sufijos de versión (ej. `CreateFunction20150331`)
- Qué información tiene en `requestParameters` y `responseElements` (especialmente cómo se identifica el recurso)
- Si el `Resources[]` viene poblado o vacío (recordando que EventBridge a veces no propaga)

### 6. Edge cases y quirks

Sección crítica — aquí va todo lo raro del servicio. Ejemplos del tipo de cosas que deben aparecer aquí:

- ARNs con formato no estándar (ej. S3 sin región/account, IAM globales sin región)
- APIs que requieren múltiples llamadas para datos completos (ej. SQS necesita `GetQueueAttributes` después de `ListQueues`)
- Campos que vienen nil con frecuencia y para los que hay que defenderse
- Operaciones que pueden afectar múltiples recursos en un solo evento (ej. `RunInstances` crea N instancias)
- Quirks de paginación
- Diferencias en cómo se identifican los recursos entre eventos (algunos usan ID, otros nombre, otros ARN)
- Restricciones de IAM/permisos que el worker necesita

### 7. Permisos IAM mínimos

Lista las acciones IAM que el discovery + worker necesitan para este servicio. Formato:

\```
{servicio}:List*
{servicio}:Get{Recurso}
\```

Esto es importante para documentar después en el README y eventualmente generar políticas correctas.

### 8. Decisiones abiertas

Lista preguntas que NO se pueden responder solo con la documentación de AWS y requieren decisión del usuario antes de pasar al diseño del crawler. Ejemplos típicos:

- "¿Modelamos las inline policies como nodos separados o como propiedades del Role?"
- "¿Incluimos los managed policies de AWS (no las del cliente) o las omitimos?"
- "¿Las groups de IAM las modelamos en el MVP o las dejamos fuera?"

NO inventes decisiones — déjalas como preguntas para que el usuario las resuelva.

## Reglas de la investigación

1. **Usa la documentación oficial de AWS**, no blog posts. Si necesitas verificar algo, usa web_search/web_fetch sobre `docs.aws.amazon.com`.

2. **Sé específico, no general.** "RDS tiene instancias" no sirve. "RDS modela DBInstance, DBCluster, DBSubnetGroup como recursos separados, cada uno con su propio ARN" sí sirve.

3. **Cuando no sepas algo, dilo explícitamente** en lugar de inventar. Frases tipo "no encuentro información clara sobre X — confirmar con el usuario" son útiles.

4. **Pregunta antes de asumir** decisiones de modelado. Si el servicio tiene 5 sub-recursos y no sabes cuáles incluir en el MVP, pregunta antes de listarlos todos.

5. **Compara con servicios ya implementados** cuando sea relevante. "Esto es similar a cómo manejamos VPC en EC2Crawler" es más útil que descripciones aisladas.

6. **No escribas código Go.** Esta fase es solo investigación. El código vendrá en SKILLs posteriores.

## Ejemplo de output esperado

Mira `examples/sqs-research.md` en este directorio para ver un ejemplo completo aplicado a SQS (servicio ya implementado, sirve como referencia).

## Cómo proceder

1. Pregunta al usuario qué servicio AWS quiere investigar (si no lo dijo).
2. Usa web_search y web_fetch contra `docs.aws.amazon.com` para reunir información.
3. Produce el documento siguiendo la estructura exacta de arriba.
4. Presenta las decisiones abiertas al usuario (sección 8) y **espera sus respuestas**.
5. Una vez que el usuario responda las decisiones, **actualiza el documento agregando una sección final "9. Decisiones tomadas"** con las respuestas confirmadas y un breve razonamiento si lo dieron.
6. Sugiere guardar el documento final en `docs/research/{servicio}-research.md`.

El archivo final debe ser self-contained: cualquier persona (o el siguiente SKILL) que lo lea debe entender qué se va a implementar y con qué decisiones, sin necesidad de contexto adicional.