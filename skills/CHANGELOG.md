# Changelog — плагин keelkit

Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver. Тег релиза `skills/vX.Y.Z`
(`make release MODULE=skills VERSION=vX.Y.Z`) фиксирует и эталон `reference/`, который копирует `create-site`.

## [Unreleased]

## [0.2.0] — 2026-10-18

### Added
- Смена пароля в эталоне: `site user password EMAIL` и `make user-password EMAIL=…` (`auth v0.3.0`,
  `Service.SetPassword`: сессии пользователя завершаются); `check-site.sh` требует цель `user-password`.
- Эвалы скиллов: `make evals SKILL=create-site|upgrade-site [CASE=…]` (`skills/evals/`). Агент выполняет скилл в
  чистом каталоге, раннер проверяет результат: `check-site.sh`, `make` сайта (включая браузерные тесты), у
  `upgrade-site` — ветка, версии, `migrate-check.sh`. Кейсы: `create-site` — `spec/examples/*.yaml`; `upgrade-site` —
  эталон на `images v0.2.0`.

### Changed
- `AGENTS.md` эталона разбит на короткое оглавление (версии, команды, БД и миграции, карта «Документация») и
  `docs/` по темам: он загружается в каждую сессию, а подробности нужны лишь отдельным шагам. `create-site` читает
  из `docs/` только файлы шага и приводит к сайту и `AGENTS.md`, и `docs/`; `upgrade-site` переносит конвенции в тот
  же файл сайта (сайт без `docs/` сначала разбивается так же); `check-site.sh` требует файлы `docs/` и ссылку на
  каждый в `AGENTS.md`.
- `create-site` строит сайт по шагам: фаза «План» — интервью, `site.yaml` и `PLAN.md` сайта с чекбоксами
  (`references/site-plan.md`: размер шага, формат, типовые шаги), затем один шаг плана за вызов — код, seed для
  проверки, тесты, коммит, «Как проверить» человеку и остановка. Без подагентов и фоновых задач; читать только
  нужное шагу. `fetch-reference.sh` допускает `PLAN.md` в каталоге сайта.
- `create-site` без готового `site.yaml` проводит интервью (`references/interview.md`) вместо одного вопроса:
  вопросы о содержании по одной теме, с умолчанием в каждом, пока не ясны разделы и поля записей; старый сайт —
  образец для разделов; то, чего в Keelkit нет, — в план как «позже»; в конце — подтверждение плана. С готовым
  `site.yaml` вопросов по-прежнему нет.

### Fixed
- `upgrade-site/scripts/migrate-check.sh`: обновление без новых миграций (нечего применять — снимка нет) больше не
  считается ошибкой.

## [0.1.0] — 2026-10-15

### Added
- Маркетплейс `.claude-plugin/marketplace.json` и плагин `keelkit` (`skills/.claude-plugin/plugin.json`).
- Скилл `create-site`: спецификация `site.yaml` с человеком, копия эталона по тегу плагина
  (`scripts/fetch-reference.sh`), приведение кода к спецификации, тесты, проверка структуры (`scripts/check-site.sh`).
- Скилл `upgrade-site`: версии модулей и `spec` по их `UPGRADE.md` (`scripts/upgrade-plan.sh`), перенос изменений
  эталона с коммита, из которого создан сайт (`scripts/reference-diff.sh`), проверки, миграции и повторная обработка
  изображений на копии данных (`scripts/migrate-check.sh`: ещё и страницы из `sitemap.xml` и файлы `/media/`,
  `/files/` на них — 200), ветка с коммитом на каждый шаг. Проверен на `images` v0.2.0 → v0.3.0.
