.PHONY: help discover worker build clean test fmt api migrate-up

# Cargar variables del .env si existe
ifneq (,$(wildcard .env))
	include .env
	export
endif

help: ## Mostrar este mensaje
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

discover: ## Correr el discovery (fase 1+2 una vez)
	go run ./cmd/discover

worker: ## Correr el worker (loop continuo de eventos)
	WORKER_QUEUE_URL=$$(cd infra/terraform && terraform output -raw queue_url) \
	go run ./cmd/worker

inspect: ## Correr el inspector
	go run ./cmd/inspect | jq '.[] | {insight: .insight_id, findings_count: (.findings | length)}'

build: ## Compilar ambos binarios a ./bin/
	mkdir -p bin
	go build -o bin/discover ./cmd/discover
	go build -o bin/worker ./cmd/worker
	go build -o bin/inspect ./cmd/inspect

test: ## Correr todos los tests
	go test ./...

fmt: ## Formatear código
	go fmt ./...
	go vet ./...

clean: ## Limpiar binarios
	rm -rf bin/


api: ## Run the API server
	go run ./cmd/api

migrate-up: ## Apply database migrations
	docker exec -i infra-diagram-postgres psql -U infra_diagram -d infra_diagram < internal/store/postgres/migrations/001_create_accounts.up.sql


# USAGE:
# make           muestra ayuda con todos los targets
# make discover  corre discovery
# make worker    corre worker (resuelve el queue URL automáticamente)
# make build     compila todos los binarios a ./bin/
# make test