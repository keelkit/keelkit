.DEFAULT_GOAL := help

# Совместимо с GNU Make 3.81 (по умолчанию на macOS): без .ONESHELL, $(file ...) и других возможностей 4.x.

# Приватные модули Keelkit (github.com/voffkin/keelkit): Go берёт их прямо из git, минуя публичный прокси
# и базу контрольных сумм, а git ходит в GitHub по SSH. Действует только на процессы make — глобальные
# настройки git и Go не меняются. Нужен SSH-ключ с доступом к репозиторию (см. «Доступ к модулям» в AGENTS.md).
export GOPRIVATE = github.com/voffkin/*
export GIT_CONFIG_COUNT = 1
export GIT_CONFIG_KEY_0 = url.git@github.com:voffkin/.insteadOf
export GIT_CONFIG_VALUE_0 = https://github.com/voffkin/

# Тег nodynamic: кодек WebP (gen2brain/webp) не подгружает системную libwebp, а всегда работает на коде,
# транслированном в Go. Результат обработки изображений одинаков на любой машине, бинарник не зависит от
# библиотек хоста. Действует на все go-команды make (run, test, build); e2e-browser добавляет его к тегу browser.
BUILD_TAGS = nodynamic
export GOFLAGS = -tags=$(BUILD_TAGS)

# Адрес HTTP-сервера, каталог данных (БД, загрузки) и каталог снимков БД.
ADDR ?= :9090
DATA_DIR ?= data
BACKUP_DIR ?= backups
# Флаг Secure у cookie админки. make run/dev — локальная разработка по http, поэтому false;
# в проде (бинарник под systemd) переменная не задаётся, и сайт ставит Secure.
COOKIE_SECURE ?= false
# Адрес сайта для canonical, Open Graph, sitemap, RSS и robots.txt (serve без него не стартует).
# Для разработки — localhost с портом из ADDR; в проде — настоящий адрес, например https://example.com.
BASE_URL ?= http://localhost:$(lastword $(subst :, ,$(ADDR)))
# Email пользователя админки для user-create.
EMAIL ?=
# Версия профиля для reprocess-images: обработать изображения с версией ниже BELOW.
BELOW ?=

# Версии инструментов закреплены здесь, а не берутся из окружения.
# Тулчейн для запуска инструментов через go run: должен совпадать с toolchain в go.mod.
GO_TOOLCHAIN ?= go1.27.2
GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
# sqlc без CGO: движок PostgreSQL отключается, SQLite работает.
SQLC_VERSION ?= v1.31.1
SQLC = GOTOOLCHAIN=$(GO_TOOLCHAIN) CGO_ENABLED=0 go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
# CLI templ обязан совпадать с библиотекой github.com/a-h/templ в go.mod: generate проверяет это перед запуском.
TEMPL_VERSION ?= v0.3.1020
TEMPL = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)
GOVULNCHECK_VERSION ?= v1.8.0
GOVULNCHECK = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
# Валидатор спецификации — опубликованная версия модуля spec (тег spec/vX.Y.Z), в код сайта не копируется.
SPEC_VERSION ?= v0.3.0
SPEC_VALIDATE = GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/voffkin/keelkit/spec/cmd/spec-validate@$(SPEC_VERSION)

SITE = ADDR=$(ADDR) BASE_URL=$(BASE_URL) DATA_DIR=$(DATA_DIR) BACKUP_DIR=$(BACKUP_DIR) COOKIE_SECURE=$(COOKIE_SECURE) go run ./cmd/site

.PHONY: help run dev migrate seed user-create reprocess-images reset generate generate-check test test-isolated e2e e2e-browser lint deps-check spec-validate

help: ## Показать список целей
	@grep -hE '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'

run: ## Запустить сайт (откажется при непримененных миграциях). Пример: make run ADDR=:9091 DATA_DIR=/tmp/site BASE_URL=https://example.com
	$(SITE) serve

dev: migrate seed ## Для разработки: миграции, seed и запуск
	$(SITE) serve

migrate: ## Снимок БД в BACKUP_DIR и применение миграций (модули, затем сайт)
	$(SITE) migrate

seed: ## Заполнить dev-данными (идемпотентно, только для разработки)
	$(SITE) seed

user-create: ## Создать пользователя админки (пароль спрашивается). Пример: make user-create EMAIL=me@example.com
	@if [ -z "$(EMAIL)" ]; then echo "user-create: укажите EMAIL, например make user-create EMAIL=me@example.com"; exit 1; fi
	$(SITE) user create "$(EMAIL)"

reprocess-images: ## Повторная обработка изображений с версией профиля ниже BELOW (идемпотентна). Пример: make reprocess-images BELOW=2
	@if [ -z "$(BELOW)" ]; then echo "reprocess-images: укажите BELOW, например make reprocess-images BELOW=2"; exit 1; fi
	$(SITE) reprocess-images -below "$(BELOW)"

reset: ## Удалить локальную БД и загрузки (DATA_DIR), чтобы начать с нуля
	rm -rf $(DATA_DIR)

generate: ## Кодогенерация (templ, sqlc)
	@lib="$$(go list -m -f '{{.Version}}' github.com/a-h/templ)"; \
	if [ "$$lib" != "$(TEMPL_VERSION)" ]; then echo "generate: TEMPL_VERSION=$(TEMPL_VERSION), а в go.mod github.com/a-h/templ $$lib — версии должны совпадать"; exit 1; fi
	$(TEMPL) generate
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

e2e: ## e2e-тесты ключевых сценариев (httptest + goquery; входят и в make test)
	go test -count=1 -run '^TestE2E' ./cmd/site

e2e-browser: ## Браузерные тесты htmx-взаимодействий (chromedp, нужен Chrome; тег сборки browser)
	go test -count=1 -tags browser,$(BUILD_TAGS) -run '^TestBrowser' ./cmd/site

lint: ## Линтер golangci-lint
	$(GOLANGCI_LINT) run ./...

deps-check: ## Доступные обновления прямых зависимостей и govulncheck (на опубликованных версиях, GOWORK=off)
	@upd="$$(GOWORK=off go list -m -u -f '{{if .Update}}{{if .Indirect}}indirect{{else}}  {{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}{{end}}' all)"; \
	echo "$$upd" | grep -v '^indirect$$' | grep . || echo "  прямые зависимости актуальны"; \
	n="$$(echo "$$upd" | grep -c '^indirect$$' || true)"; [ "$$n" = 0 ] || echo "  косвенных с обновлениями: $$n (GOWORK=off go list -m -u all)"
	GOWORK=off $(GOVULNCHECK) ./...

spec-validate: ## Проверить site.yaml опубликованным валидатором (SPEC_VERSION). Пример: make spec-validate SPEC_VERSION=v0.1.0
	$(SPEC_VALIDATE) site.yaml
