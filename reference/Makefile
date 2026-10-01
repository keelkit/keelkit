.DEFAULT_GOAL := help

# Совместимо с GNU Make 3.81 (по умолчанию на macOS): без .ONESHELL, $(file ...) и других возможностей 4.x.

# Адрес HTTP-сервера и каталог данных (БД, загрузки).
ADDR ?= :8080
DATA_DIR ?= data

# Версии инструментов закреплены здесь, а не берутся из окружения.
# Тулчейн для запуска инструментов через go run: должен совпадать с toolchain в go.mod.
GO_TOOLCHAIN ?= go1.27.1
GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: help run dev test test-isolated lint

help: ## Показать список целей
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'

run: ## Запустить сайт. Пример: make run ADDR=:9090 DATA_DIR=/tmp/site
	ADDR=$(ADDR) DATA_DIR=$(DATA_DIR) go run ./cmd/site

dev: ## Запустить сайт для разработки (позже: миграции и seed перед запуском)
	@$(MAKE) run

test: ## Тесты сайта
	go test ./...

test-isolated: ## Тесты только на опубликованных версиях модулей (GOWORK=off)
	GOWORK=off go test ./...

lint: ## Линтер golangci-lint
	$(GOLANGCI_LINT) run ./...
