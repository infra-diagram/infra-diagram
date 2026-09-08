# infra-diagram

Análisis de solo lectura sobre infraestructura AWS: un motor de discovery
puebla un grafo (Neo4j) con recursos y relaciones, una capa de insights corre
queries de seguridad/higiene sobre ese grafo, y una API + frontend exponen los
resultados como findings navegables y su subgrafo asociado.

Ver [CLAUDE.md](CLAUDE.md) para arquitectura, contratos de API y decisiones de
diseño en detalle. Esto es solo la guía para levantar el MVP en local.

## Requisitos

- **Go** ≥ 1.25 (`go.mod`)
- **Node** ≥ 20 y npm (para `web/`)
- **Docker** (para Neo4j y Postgres — o instalalos nativamente si preferís)
- **Credenciales AWS** configuradas (`~/.aws/credentials` o variables de
  entorno) — solo necesarias para `make discover`/`make worker` (poblar el
  grafo desde una cuenta real). No hacen falta para correr la API/frontend
  contra un grafo ya poblado.

## 1. Levantar Neo4j y Postgres

```bash
docker compose up -d
```

Esto levanta:

| Servicio | Puerto | Credenciales |
|---|---|---|
| Neo4j (bolt) | `7687` | `neo4j` / `password` |
| Neo4j (browser HTTP) | `7474` | idem |
| Postgres | `5432` | `infra_diagram` / `password`, db `infra_diagram` |

Si ya tenés algo corriendo en esos puertos, cambiá el mapeo en
[docker-compose.yml](docker-compose.yml) antes de levantar.

## 2. Migraciones de Postgres

```bash
make migrate-up
```

Crea la tabla `aws_accounts` (mapea el `account_id` UUID que usa la API al
`aws_account_id` de 12 dígitos que vive en Neo4j).

## 3. Registrar una cuenta AWS

La API resuelve todo `account_id` de la URL contra esta tabla — **sin una fila
acá, cualquier insight devuelve 404 "account not found"**, aunque el grafo
tenga datos.

```bash
docker exec -it infra-diagram-postgres psql -U infra_diagram -d infra_diagram -c \
  "INSERT INTO aws_accounts (aws_account_id, alias) VALUES ('123456789012', 'mi-cuenta') RETURNING id;"
```

Reemplazá `123456789012` por el AWS account ID real que vas a discoverar (o el
que ya está en tu grafo, si vas a saltar el paso 4). Guardá el UUID que
devuelve el `INSERT` — es el `account_id` que vas a pegar en el frontend.

## 4. Poblar el grafo (Discovery)

Necesita credenciales AWS válidas para la cuenta que registraste en el paso 3.

```bash
make discover
```

Corre una vez: fase 1 (nodos) + fase 2 (relaciones) contra los 7 servicios
soportados (EC2, VPC, Subnet, Lambda, SQS, S3, IAM).

Si ya tenés un Neo4j con datos de una corrida anterior, podés saltar este paso
— solo asegurate de que el `aws_account_id` del paso 3 coincida con el que
está en los nodos del grafo.

El worker (`make worker`) mantiene el grafo actualizado en vivo consumiendo
eventos de una cola SQS alimentada por EventBridge (infra en
`infra/terraform`). No hace falta para probar el MVP contra un snapshot
estático.

## 5. Levantar la API

```bash
make api
```

Corre en `:8080`. Variables de entorno (todas con default para desarrollo
local, ver [cmd/api/main.go](cmd/api/main.go)):

| Variable | Default |
|---|---|
| `API_ADDR` | `:8080` |
| `NEO4J_URI` | `bolt://localhost:7687` |
| `NEO4J_USER` | `neo4j` |
| `NEO4J_PASSWORD` | `password` |
| `POSTGRES_URL` | `postgres://infra_diagram:password@localhost:5432/infra_diagram` |

Verificar:

```bash
curl localhost:8080/health
```

## 6. Levantar el frontend

```bash
cd web
npm install
npm run dev
```

Abrí `http://localhost:5173`. Pegá en la barra superior el `account_id` UUID
que guardaste en el paso 3 (queda en `localStorage`, no hace falta reingresarlo
cada vez). El proxy de Vite reenvía las rutas de API hacia `:8080` — no hay
que setear `VITE_API_BASE_URL` en dev.

## Comandos útiles

```bash
make help    # lista todos los targets del Makefile con descripción
make test    # go test ./...
make build   # compila discover/worker/inspect a ./bin/ (no incluye cmd/api, correr con `go run`/`make api`)
```

## Troubleshooting

- **"account not found"** al ejecutar un insight → no hiciste el paso 3, o el
  UUID pegado en el frontend no coincide con el de `aws_accounts`.
- **Findings en 0 / grafo vacío** → no corriste `make discover`, o lo corriste
  contra una cuenta AWS distinta a la registrada en el paso 3 (revisá
  `aws_account_id` en la tabla vs. la propiedad `accountId` de los nodos en
  Neo4j).
- **`iam.blast_radius` no arranca** → es el único insight con parámetro
  requerido (`role_arn`); completalo en el form de la página del insight.
- **Puerto ocupado** (5432/7687/7474/8080/5173) → algún otro proceso/proyecto
  ya lo usa; cambiá el mapeo en `docker-compose.yml` o la variable de entorno
  correspondiente.
