.DEFAULT_GOAL := help

# Совместимо с GNU Make 3.81 (по умолчанию на macOS): без .ONESHELL, $(file ...) и других возможностей 4.x.

# Адрес HTTP-сервера, каталог данных (БД, загрузки) и каталог снимков БД.
ADDR ?= :8080
DATA_DIR ?= data
BACKUP_DIR ?= backups

# Версии инструментов закреплены здесь, а не берутся из окружения.
# Тулчейн для запуска инструментов через go run: должен совпадать с toolchain в go.mod.
GO_TOOLCHAIN ?= go1.27.1
GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
# sqlc без CGO: движок PostgreSQL отключается, SQLite работает.
SQLC_VERSION ?= v1.31.1
SQLC = GOTOOLCHAIN=$(GO_TOOLCHAIN) CGO_ENABLED=0 go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

SITE = ADDR=$(ADDR) DATA_DIR=$(DATA_DIR) BACKUP_DIR=$(BACKUP_DIR) go run ./cmd/site

.PHONY: help run dev migrate seed reset generate generate-check test test-isolated lint

help: ## Показать список целей
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'

run: ## Запустить сайт (откажется при непримененных миграциях). Пример: make run ADDR=:9090 DATA_DIR=/tmp/site
	$(SITE) serve

dev: migrate seed ## Для разработки: миграции, seed и запуск
	$(SITE) serve

migrate: ## Снимок БД в BACKUP_DIR и применение миграций (модули, затем сайт)
	$(SITE) migrate

seed: ## Заполнить dev-данными (идемпотентно, только для разработки)
	$(SITE) seed

reset: ## Удалить локальную БД и загрузки (DATA_DIR), чтобы начать с нуля
	rm -rf $(DATA_DIR)

generate: ## Кодогенерация (sqlc)
	$(SQLC) generate

generate-check: ## Проверить, что кодогенерация ничего не меняет
	@before="$$(git status --porcelain --untracked-files=all -- . && git diff -- .)"; \
	$(MAKE) generate; \
	after="$$(git status --porcelain --untracked-files=all -- . && git diff -- .)"; \
	if [ "$$before" != "$$after" ]; then echo "generate-check: генерация изменила файлы, выполните make generate и закоммитьте"; git status --short -- .; exit 1; fi

test: ## Тесты сайта
	go test ./...

test-isolated: ## Тесты только на опубликованных версиях модулей (GOWORK=off)
	GOWORK=off go test ./...

lint: ## Линтер golangci-lint
	$(GOLANGCI_LINT) run ./...
