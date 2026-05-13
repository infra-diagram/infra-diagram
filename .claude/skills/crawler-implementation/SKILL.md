---
name: crawler-implementation
description: "Implementar el código Go de un crawler/handler para un servicio AWS en el proyecto infra-diagram, basándose en documentos de research y design previos. Activar cuando el usuario menciona implementar, codear, escribir el código, generar los archivos, o pasar del diseño a la implementación. Ejemplos: 'vamos a implementar IAM', 'escribe el código del crawler de RDS', 'genera los archivos siguiendo el design', 'pasemos a la implementación'."
---

# Crawler Implementation

Tu tarea es producir el **código Go completo** para integrar un nuevo servicio AWS al proyecto infra-diagram, siguiendo exactamente las decisiones tomadas en los documentos de research y design.

Esta fase es **mecánica por diseño**: las decisiones ya están tomadas en las fases anteriores. Tu trabajo es traducir esas decisiones a código Go que compila, pasa `go vet`, y sigue el estilo casero del proyecto.

## Inputs requeridos

Debes tener acceso a ambos archivos antes de proceder:

1. `docs/research/{servicio}-research.md` — sección "Decisiones tomadas" debe estar presente
2. `docs/design/{servicio}-design.md` — sección "Decisiones de implementación tomadas" debe estar presente

**Si cualquiera de los dos archivos no existe o le falta la sección de decisiones**, detente y pide al usuario que complete el paso anterior. NO inventes decisiones que faltan.

## Contexto del proyecto

Antes de escribir código, **lee estos archivos** del repo para entender el patrón actual:

- `internal/crawler/queue.go` — caso simple (referencia para servicios sin relaciones complejas)
- `internal/crawler/lambda.go` — caso complejo (referencia para crawlers con cache, relaciones cambiantes, etc.)
- `internal/crawler/relations.go` o `helpers.go` — helpers existentes que probablemente reusarás
- `internal/events/handlers/lambda.go` — handler complejo (referencia para HandleChange con RebuildRelations)
- `internal/events/handlers/sqs.go` — handler simple (referencia para HandleChange sin RebuildRelations)
- `internal/models/nodes.go` y `relations.go` — estilo de structs
- `cmd/discover/main.go` y `cmd/worker/main.go` — composition roots
- `infra/terraform/main.tf` — event_pattern actual

Cualquier patrón nuevo que no exista en los archivos anteriores debe estar justificado por el design. NO inventes.

## Output: archivos a producir/modificar

Sigue **estrictamente** el checklist de la sección 7 del documento de design. Para cada archivo en el checklist:

1. **Si es nuevo:** créalo completo.
2. **Si es modificación:** muestra el diff exacto (qué líneas se agregan, dónde, qué se borra).

Procesa los archivos en este orden (importa porque las dependencias van en cascada):

### Paso 1: Modelos

- `internal/models/nodes.go` — agregar los structs definidos en design §1
- `internal/models/relations.go` — agregar los structs definidos en design §2

### Paso 2: Helpers (si el design los requiere)

- `internal/crawler/helpers.go` — agregar parsers o constructores de ARN específicos
- `internal/crawler/relations.go` (o donde vivan los rebuild helpers) — agregar los rebuild helpers nuevos del design §4

### Paso 3: Crawler

- `internal/crawler/{servicio}.go` — implementar completo siguiendo design §3

Estructura obligatoria del archivo:

\```go
package crawler

import (
    // imports en orden: stdlib, AWS SDK, neo4j, models del proyecto
)

// Type y constructor

// =============================================================================
// FASE 1 — Nodos
// =============================================================================

// IngestNodes, IngestSingle, IngestSingleWithDriver, upsertNodeFromX, fetchX

// =============================================================================
// FASE 2 — Relaciones
// =============================================================================

// IngestRelations, RebuildRelations, RebuildRelationsWithDriver, rebuildRelationsFromX

// =============================================================================
// DELETE
// =============================================================================

// DeleteByArn
\```

Las separaciones de "FASE 1 — Nodos", "FASE 2 — Relaciones", "DELETE" son **literales** — deben aparecer como comentarios de bloque en el código. Es el estilo del proyecto.

### Paso 4: Handler

- `internal/events/handlers/{servicio}.go` — implementar completo siguiendo design §5

### Paso 5: Composition roots

- `cmd/discover/main.go` — agregar instanciación del crawler nuevo + agregarlo al slice
- `cmd/worker/main.go` — agregar instanciación de crawler + handler + registros del router

### Paso 6: Infraestructura Neo4j

- `internal/graph/setup.go` — agregar índices nuevos si el design los requiere

### Paso 7: Infraestructura AWS (Terraform)

- `infra/terraform/main.tf` — agregar el source y eventNames nuevos al `event_pattern` según design §6

### Paso 8: Permisos IAM del worker (si aplica)

Si el documento de research §7 lista permisos IAM que el worker actualmente no tiene, alertar al usuario que debe agregarlos a la policy del rol que ejecuta el worker. **No modificar la policy automáticamente** — eso es trabajo de operaciones, no de código.

## Reglas estrictas

### Sobre estilo

1. **Manejo de nil pointers:** siempre verificar antes de desreferenciar. AWS SDK retorna `*string`/`*time.Time`/`*int32` y muchos pueden ser nil.

   \```go
   // ❌ Mal
   name := *resource.Name
   
   // ✅ Bien
   name := ""
   if resource.Name != nil {
       name = *resource.Name
   }
   \```

2. **`time.Time` se serializa a ISO 8601:**
   \```go
   createDate := ""
   if r.CreateDate != nil {
       createDate = r.CreateDate.UTC().Format(time.RFC3339)
   }
   \```

3. **Errores en bulk vs single:**
   - En `IngestNodes` (bulk): `log.Printf` + `continue` para no abortar todo el discovery por un recurso problemático
   - En `IngestSingle` y handlers: `return error` para que el worker reintente vía SQS

4. **Sesiones Neo4j:** método público que recibe `driver` abre sesión y la cierra con defer. Método interno recibe `session` ya abierta. Sigue el patrón `IngestSingle` (recibe session) vs `IngestSingleWithDriver` (recibe driver).

5. **Doble label en MERGE:** todos los nodos llevan `:Type:AWSResource`. Ejemplo: `MERGE (r:IAMRole:AWSResource {arn: $arn})`.

6. **Imports ordenados:** stdlib primero, después AWS SDK, después neo4j driver, después packages del proyecto. Línea en blanco entre grupos.

### Sobre la lógica

7. **Idempotencia:** todos los upserts usan `MERGE`, no `CREATE`. Todos los deletes usan `DETACH DELETE` (idempotente — no falla si el nodo no existe).

8. **Rebuild de relaciones:** sigue el patrón "DELETE outgoing + MERGE new". Nunca usar `MATCH ... SET` para relaciones — siempre delete + recreate.

9. **Construcción de ARN para deletes:** EventBridge no siempre propaga `Resources[]`. Construye el ARN desde `event.Detail.AwsRegion`, `event.Account`, y el identificador del recurso desde `requestParameters`. Patrón ya establecido en handlers existentes.

10. **Cache `sync.Once` solo si se justifica:** úsalo cuando `IngestNodes` e `IngestRelations` consumen la misma lista. Si solo `IngestNodes` la usa, no agregues cache.

### Sobre lo que NO debes hacer

11. **NO inventes campos en los structs.** Solo los listados en el design §1.

12. **NO inventes eventNames.** Solo los del design §5. Si descubres un evento adicional necesario, detente y pregunta antes de agregarlo.

13. **NO refactorices código existente.** Si tocar `lambda.go` para agregar un nuevo método es necesario según el design, hazlo. Pero NO "limpies" el código existente "de paso".

14. **NO modifiques el `Engine`, `Router`, ni `Worker`.** Esos son piezas estables. Si tu cambio requiere modificarlos, detente — algo del design está mal.

15. **NO agregues tests.** Tests están fuera del scope de este SKILL.

## Validación post-implementación

Al terminar de generar todos los archivos, ejecuta:

\```bash
go build ./...
go vet ./...
\```

Si hay errores, corrígelos. NO digas "el código está listo" si no compila.

Después, lista al usuario los pasos manuales que debe ejecutar:

1. \`terraform plan\` y \`terraform apply\` en `infra/terraform/` para actualizar EventBridge
2. Agregar permisos IAM al rol del worker (de research §7)
3. Reiniciar el worker (`make worker`) para que tome los nuevos handlers registrados
4. Limpiar Neo4j y correr `make discover` para validar el discovery del servicio nuevo
5. Probar end-to-end creando un recurso del servicio con AWS CLI

## Cómo proceder

1. Pregunta al usuario qué servicio quiere implementar (si no lo dijo).
2. **Lee `docs/research/{servicio}-research.md` y `docs/design/{servicio}-design.md`.** Si falta alguno o le falta sección de decisiones, detente.
3. **Lee los archivos del proyecto** mencionados en "Contexto del proyecto".
4. Procede archivo por archivo en el orden del checklist (Paso 1 al 8).
5. Para cada archivo:
   - Si es nuevo, créalo completo con `create_file`
   - Si es modificación, usa `str_replace` para mostrar el diff
   - Después de cada archivo, dile al usuario qué hiciste en una línea
6. Al final, ejecuta `go build ./...` y `go vet ./...`.
7. Si todo compila, presenta el checklist de pasos manuales finales.
8. **NO declares la implementación "completa" hasta que el build pase.**