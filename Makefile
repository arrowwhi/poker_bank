.PHONY: dev prod build migrate migrate-down migrate-status db stop logs lint test

COMPOSE := docker compose -f docker/docker-compose.yml

# ─── Dev ────────────────────────────────────────────────────────────────────

dev: ## Запустить бота с hot-reload (air) + postgres
	$(COMPOSE) --profile dev up --build -d

prod: ## Запустить бота из собранного образа + postgres
	$(COMPOSE) --profile prod up --build

build: ## Собрать бинарник локально
	go build -o ./tmp/bot ./cmd/poker-bank

GOOSE_DBSTRING ?= host=localhost port=5432 dbname=poker_bank user=poker password=poker sslmode=disable

migrate: ## Применить все миграции
	goose -dir ./migrations postgres "$(GOOSE_DBSTRING)" up

migrate-down: ## Откатить последнюю миграцию
	goose -dir ./migrations postgres "$(GOOSE_DBSTRING)" down

lint: ## Запустить golangci-lint
	golangci-lint run ./...

test: ## Запустить тесты
	go test ./...

help: ## Показать этот список команд
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help