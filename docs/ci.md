# CI

Часть корневого `AGENTS.md`: читается, когда задача касается этой темы.

## CI

**Сейчас (до этапа 14 в `PLAN.md`) CI на GitHub запускается только вручную (`workflow_dispatch`).** Решение человека:
проверки выполняются локально — `make check` (без `MODULE` — всё) перед коммитом в `main`, и обязательно перед тегом:
`make release` сам запускает `make check MODULE=<name>` и не ставит тег, если она не прошла. Цена: нет проверки на
чистой Linux-машине — её вернёт этап 14 (автозапуск, path-фильтры). Для `reference/` `make check` запускает и
`e2e-browser` — нужен Chrome.

Целевое устройство (этап 14). GitHub Actions; джобы вызывают только цели `make`.
- Path-фильтры: изменения в `modules/<X>` или `spec/` запускают джобы для этого модуля и для `reference/`.
- Модуль: `make lint`, `make test`, `make test-isolated`, `make generate-check`, `make deps-check`.
- `reference/`: то же плюс `make e2e` и `make e2e-browser` (chromedp, Chrome есть на раннере), всё с `GOWORK=off`.
- Эвалы скиллов в CI не запускаются — только локально `make evals`.
