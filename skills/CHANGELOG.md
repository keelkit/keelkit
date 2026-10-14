# Changelog — плагин keelkit

Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver. Тег релиза `skills/vX.Y.Z`
(`make release MODULE=skills VERSION=vX.Y.Z`) фиксирует и эталон `reference/`, который копирует `create-site`.

## [Unreleased]

### Added
- Маркетплейс `.claude-plugin/marketplace.json` и плагин `keelkit` (`skills/.claude-plugin/plugin.json`).
- Скилл `create-site`: спецификация `site.yaml` с человеком, копия эталона по тегу плагина
  (`scripts/fetch-reference.sh`), приведение кода к спецификации, тесты, проверка структуры (`scripts/check-site.sh`).
- Скилл `upgrade-site`: версии модулей и `spec` по их `UPGRADE.md` (`scripts/upgrade-plan.sh`), перенос изменений
  эталона с коммита, из которого создан сайт (`scripts/reference-diff.sh`), проверки, миграции и повторная обработка
  изображений на копии данных (`scripts/migrate-check.sh`), ветка с коммитом на каждый шаг.
