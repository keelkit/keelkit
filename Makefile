.DEFAULT_GOAL := help

# Совместимо с GNU Make 3.81 (по умолчанию на macOS): без .ONESHELL, $(file ...) и других возможностей 4.x.

# Модуль в modules/<MODULE>; MODULE=spec или MODULE=reference — каталог в корне; пусто — все.
MODULE ?=

# Спецификация сайта для spec-validate.
FILE ?= reference/site.yaml

# Версии инструментов закреплены здесь, а не берутся из окружения.
# Тулчейн для запуска инструментов через go run: должен совпадать с toolchain в go.work.
GO_TOOLCHAIN ?= go1.27.1
GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

ALL_MODULES := $(patsubst %/go.mod,%,$(wildcard modules/*/go.mod))

ifeq ($(MODULE),)
DIRS := $(ALL_MODULES) spec reference
else
ifneq ($(filter $(MODULE),spec reference),)
DIRS := $(MODULE)
else
DIRS := modules/$(MODULE)
endif
endif

.PHONY: help test test-isolated lint spec-validate run dev

help: ## Показать список целей
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'

test: ## Тесты (по умолчанию все). Пример: make test MODULE=images
	@set -e; for d in $(DIRS); do echo "==> test $$d"; (cd $$d && go test ./...); done

test-isolated: ## Тесты только на опубликованных зависимостях (GOWORK=off). Пример: make test-isolated MODULE=images
	@set -e; for d in $(DIRS); do echo "==> test-isolated $$d"; (cd $$d && GOWORK=off go test ./...); done

lint: ## Линтер golangci-lint (по умолчанию все). Пример: make lint MODULE=reference
	@set -e; for d in $(DIRS); do echo "==> lint $$d"; (cd $$d && $(GOLANGCI_LINT) run ./...); done

spec-validate: ## Проверить спецификацию сайта. Пример: make spec-validate FILE=spec/examples/blog.yaml
	@go run ./spec/cmd/spec-validate $(FILE)

run: ## Запустить эталонный сайт на http://localhost:8080. Пример: make run ADDR=:9090
	@$(MAKE) -C reference run

dev: ## Запустить эталонный сайт в режиме разработки
	@$(MAKE) -C reference dev
