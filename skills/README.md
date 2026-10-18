# Плагин keelkit для Claude Code

Скиллы для создания и обновления сайтов Keelkit. Установка и требования — [`README.md`](../README.md) монорепо;
правила для скиллов — раздел «Скиллы» в [`docs/skills.md`](../docs/skills.md).

| Скилл | Что делает | Статус |
|---|---|---|
| [`create-site`](create-site/SKILL.md) | новый сайт по шагам: интервью → `site.yaml` и `PLAN.md` → по шагу плана за сессию: копия эталона `reference/` → основа → разделы → оформление → проверки | готов, эвалы — `make evals` |
| [`upgrade-site`](upgrade-site/SKILL.md) | обновить сайт: модули и `spec` по их `UPGRADE.md`, исправления эталона → тесты → миграции на копии БД → ветка | готов, эвалы — `make evals` |
| `add-section` | добавить тип контента на существующий сайт | запланирован |

- Версия плагина — `.claude-plugin/plugin.json`, изменения — [`CHANGELOG.md`](CHANGELOG.md), релиз —
  `make release MODULE=skills VERSION=vX.Y.Z` (тег `skills/vX.Y.Z` фиксирует и эталон, который копирует `create-site`).
- Проверка манифестов — `make plugin-validate`; эвалы — `make evals SKILL=<скилл>` ([`evals/README.md`](evals/README.md)).
