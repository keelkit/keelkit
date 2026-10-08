.DEFAULT_GOAL := help

# Совместимо с GNU Make 3.81 (по умолчанию на macOS): без .ONESHELL, $(file ...) и других возможностей 4.x.

# Приватные модули Keelkit (github.com/voffkin/keelkit): Go берёт их прямо из git, минуя публичный прокси
# и базу контрольных сумм, а git ходит в GitHub по SSH. Действует только на процессы make — глобальные
# настройки git и Go не меняются. Нужен SSH-ключ с доступом к репозиторию (см. «Доступ к модулям» в AGENTS.md).
export GOPRIVATE = github.com/voffkin/*
export GIT_CONFIG_COUNT = 1
export GIT_CONFIG_KEY_0 = url.git@github.com:voffkin/.insteadOf
export GIT_CONFIG_VALUE_0 = https://github.com/voffkin/

# Тег nodynamic: кодек WebP (gen2brain/webp) не подгружает системную libwebp — модули и reference/ тестируются
# на том же коде, что работает в бинарнике сайта (см. BUILD_TAGS в reference/Makefile).
export GOFLAGS = -tags=nodynamic

# Модуль в modules/<MODULE>; MODULE=spec или MODULE=reference — каталог в корне; пусто — все.
MODULE ?=

# Email пользователя админки для user-create.
EMAIL ?=

# Версия профиля для reprocess-images.
BELOW ?=

# Файл или папка с изображениями для images-demo.
IN ?= modules/images/testdata

# Спецификация сайта для spec-validate.
FILE ?= reference/site.yaml

# Версия для release (vX.Y.Z); DRY_RUN=1 — только проверки, без тега.
VERSION ?=
DRY_RUN ?=

# Версии инструментов закреплены здесь, а не берутся из окружения.
# Тулчейн для запуска инструментов через go run: должен совпадать с toolchain в go.work.
GO_TOOLCHAIN ?= go1.27.1
GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION ?= v1.8.0
GOVULNCHECK = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

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

.PHONY: help check test test-isolated lint deps-check release spec-validate images-demo run dev migrate seed user-create reprocess-images reset generate generate-check e2e e2e-browser

help: ## Показать список целей
	@grep -hE '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'

test: ## Тесты (по умолчанию все). Пример: make test MODULE=images
	@set -e; for d in $(DIRS); do echo "==> test $$d"; (cd $$d && go test ./...); done

test-isolated: ## Тесты только на опубликованных зависимостях (GOWORK=off). Пример: make test-isolated MODULE=images
	@set -e; for d in $(DIRS); do echo "==> test-isolated $$d"; (cd $$d && GOWORK=off go test ./...); done

lint: ## Линтер golangci-lint (по умолчанию все). Пример: make lint MODULE=reference
	@set -e; for d in $(DIRS); do echo "==> lint $$d"; (cd $$d && $(GOLANGCI_LINT) run ./...); done

# Как в CI: только опубликованные версии зависимостей (GOWORK=off). Обновления — для сведения, уязвимости — ошибка.
deps-check: ## Доступные обновления зависимостей и govulncheck (по умолчанию все). Пример: make deps-check MODULE=images
	@set -e; for d in $(DIRS); do echo "==> deps-check $$d"; \
	(cd $$d && upd="$$(GOWORK=off go list -m -u -f '{{if .Update}}{{if .Indirect}}indirect{{else}}  {{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}{{end}}' all)"; \
	echo "$$upd" | grep -v '^indirect$$' | grep . || echo "  прямые зависимости актуальны"; \
	n="$$(echo "$$upd" | grep -c '^indirect$$' || true)"; [ "$$n" = 0 ] || echo "  косвенных с обновлениями: $$n (GOWORK=off go list -m -u all)"; \
	GOWORK=off $(GOVULNCHECK) ./...); done

# Те же проверки, что в .github/workflows/ci.yml; пока CI на GitHub запускается вручную, это обязательная проверка
# перед релизом (её вызывает make release). Для reference/ — ещё generate-check и e2e, для spec/ — проверка reference/site.yaml.
check: lint test test-isolated deps-check ## Все проверки CI локально (по умолчанию все). Пример: make check MODULE=images
ifneq ($(filter reference,$(DIRS)),)
	@$(MAKE) --no-print-directory generate-check e2e
endif
ifneq ($(filter spec,$(DIRS)),)
	@$(MAKE) --no-print-directory spec-validate FILE=reference/site.yaml
endif

release: ## Проверки и тег <каталог>/VERSION с push в origin. Пример: make release MODULE=images VERSION=v0.1.0 DRY_RUN=1
	@./scripts/release.sh "$(MODULE)" "$(VERSION)" "$(DRY_RUN)"

spec-validate: ## Проверить спецификацию сайта. Пример: make spec-validate FILE=spec/examples/blog.yaml
	@go run ./spec/cmd/spec-validate $(FILE)

images-demo: ## Обработать изображения модулем images и построить отчёт modules/images/demo-out/index.html. Пример: make images-demo IN=~/Pictures/photo.jpg
	@cd modules/images && go run ./internal/cmd/images-demo -in "$(abspath $(IN))" -out demo-out

run: ## Запустить эталонный сайт на http://localhost:8080 (нужен make migrate). Пример: make run ADDR=:9090
	@$(MAKE) -C reference run

dev: ## Эталонный сайт для разработки: миграции, seed и запуск
	@$(MAKE) -C reference dev

migrate: ## Снимок БД и миграции эталонного сайта
	@$(MAKE) -C reference migrate

seed: ## Заполнить эталонный сайт dev-данными (идемпотентно)
	@$(MAKE) -C reference seed

user-create: ## Создать пользователя админки эталонного сайта. Пример: make user-create EMAIL=me@example.com
	@$(MAKE) -C reference user-create EMAIL="$(EMAIL)"

reprocess-images: ## Повторная обработка изображений эталонного сайта. Пример: make reprocess-images BELOW=2
	@$(MAKE) -C reference reprocess-images BELOW="$(BELOW)"

reset: ## Удалить локальную БД и загрузки эталонного сайта (reference/data/)
	@$(MAKE) -C reference reset

# Кодогенерация пока есть только в reference/; модули и spec/ ничего не генерируют.
generate: ## Кодогенерация (templ, sqlc) эталонного сайта
	@$(MAKE) -C reference generate

generate-check: ## Проверить, что кодогенерация ничего не меняет
	@$(MAKE) -C reference generate-check

e2e: ## e2e-тесты эталонного сайта (httptest + goquery)
	@$(MAKE) -C reference e2e

e2e-browser: ## Браузерные тесты эталонного сайта (chromedp, нужен Chrome)
	@$(MAKE) -C reference e2e-browser
