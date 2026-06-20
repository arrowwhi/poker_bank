.PHONY: dev prod build topicfinder migrate migrate-down migrate-status db stop logs lint test

COMPOSE := docker compose -f docker/docker-compose.yml

# ─── Dev ────────────────────────────────────────────────────────────────────

dev: ## Запустить бота с hot-reload (air) + postgres
	$(COMPOSE) --profile dev up --build -d

prod: ## Запустить бота из собранного образа + postgres
	$(COMPOSE) --profile prod up --build

build: ## Собрать бинарник локально
	go build -o ./tmp/bot ./cmd/poker-bank

topicfinder: ## Запустить утилиту поиска ID топика (останови основного бота!)
	go run ./cmd/topicfinder

migrate: ## Применить все миграции через контейнер
	$(COMPOSE) run --rm migrate up

migrate-down: ## Откатить последнюю миграцию
	$(COMPOSE) run --rm migrate down

lint: ## Запустить golangci-lint
	golangci-lint run ./...

test: ## Запустить тесты
	go test ./...

help: ## Показать этот список команд
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help