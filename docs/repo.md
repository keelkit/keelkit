# Структура репозитория

Часть корневого `AGENTS.md`: читается, когда задача касается этой темы.

## Структура репозитория

```
/
├── README.md               # для людей: что это, требования, установка плагина, создание сайта, разработка
├── AGENTS.md               # оглавление: принципы, правила модулей, Makefile, карта docs/
├── docs/                   # правила по темам: модули, спецификация, скиллы, конвенции сайтов, зависимости, решения
├── CLAUDE.md               # @AGENTS.md — для Claude Code
├── LICENSE                 # MIT
├── Makefile                # единая точка входа для всех команд
├── go.work                 # коммитится; в корне нет go.mod
├── .claude-plugin/         # marketplace.json: монорепо — маркетплейс плагинов Claude Code (плагин keelkit — skills/)
├── .github/workflows/      # CI
├── scripts/release.sh      # проверки и тег релиза; вызывается только через make release
├── modules/
│   ├── images/             # первый модуль (свой go.mod)
│   │   ├── go.mod
│   │   ├── process.go, storage.go, service.go, reprocess.go, handler.go  # публичный API
│   │   ├── internal/       # exif, orient; cmd/images-demo — демо-отчёт (make images-demo)
│   │   ├── testdata/       # тестовые изображения; генератор — testdata_gen_test.go, см. testdata/README.md
│   │   ├── migrations/     # embed.FS (FS) и имя таблицы версий (Table), миграции только этого модуля
│   │   ├── testkit/        # контрактные тесты для сайтов: testkit.Run(t, setup)
│   │   ├── CHANGELOG.md
│   │   └── UPGRADE.md      # инструкции по обновлению для агентов
│   ├── richtext/           # Markdown → очищенный HTML (goldmark + bluemonday, типографика); testkit — XSS-проверки
│   ├── auth/               # пользователи, пароли (argon2id), сессии, CSRF, ограничение попыток входа; таблицы users, sessions
│   └── files/              # PDF, видео (Range), SVG (CSP sandbox) как есть; замена без смены адреса; таблицы files_*
├── spec/                   # модуль спецификации (свой go.mod, теги spec/vX.Y.Z)
│   ├── schema.json         # JSON Schema спецификации сайта (встраивается через embed)
│   ├── spec.go             # spec.Validate: схема + смысловые правила
│   ├── cmd/spec-validate/  # CLI-валидатор: make spec-validate FILE=...
│   ├── examples/           # примеры спецификаций; invalid/ — ошибочные, со строкой «# expect: …»
│   ├── CHANGELOG.md
│   └── UPGRADE.md          # что менять в site.yaml при обновлении валидатора
├── skills/                 # плагин keelkit: скиллы (create-site, add-section, upgrade-site, ...)
│   ├── .claude-plugin/     # plugin.json — манифест и версия плагина
│   ├── create-site/        # SKILL.md, references/, scripts/ (fetch-reference.sh, check-site.sh)
│   ├── upgrade-site/       # SKILL.md, scripts/ (upgrade-plan.sh, reference-diff.sh, migrate-check.sh)
│   ├── CHANGELOG.md
│   ├── README.md           # список скиллов плагина и их статус
│   └── evals/              # эвалы: run.sh, <скилл>/{setup.sh,prompt.md,check.sh}; make evals SKILL=…
└── reference/              # эталонный сайт: страницы + блог (свой go.mod)
```

**`go.work` и честность зависимостей.** `go.work` коммитится и связывает модули и `reference/` для локальной разработки.
`reference/` при этом — полноценный модуль с конкретными версиями модулей в `go.mod`, без `replace`,
и собирается и тестируется с `GOWORK=off` (`make test-isolated`), как настоящий сайт. Поэтому изменение API модуля — два шага:
релиз модуля с тегом, затем обновление версии в `reference/`.

Второй и последующие сайты живут в отдельных репозиториях, не в монорепо.
