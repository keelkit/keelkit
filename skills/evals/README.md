# Эвалы скиллов плагина keelkit

Эвал — настоящий прогон скилла агентом (`claude -p`) в чистом каталоге и проверки результата, которые запускает
раннер, а не агент. Вызовы LLM: прогон занимает от десятков минут до пары часов и стоит денег, поэтому эвалы
запускаются вручную — обязательно перед релизом плагина (`make release MODULE=skills`).

```sh
make evals SKILL=upgrade-site                         # все кейсы скилла
make evals SKILL=create-site CASE=blog                # один кейс
make evals SKILL=create-site MODEL=claude-sonnet-5-5 BUDGET=20 TIMEOUT=7200 KEEP=0
```

| Переменная | По умолчанию | Что |
|---|---|---|
| `SKILL` | — | `create-site`, `upgrade-site` |
| `CASE` | все | кейс скилла |
| `MODEL` | модель Claude Code по умолчанию | модель агента |
| `BUDGET` | `40` | предел в долларах на кейс (`--max-budget-usd`) |
| `TIMEOUT` | `10800` | предел в секундах на кейс |
| `KEEP` | `1` | `0` — удалить каталог сайта после проверок |
| `EVALS_OUT` | `$TMPDIR/keelkit-evals` | куда писать результаты; только вне монорепо (иначе сайт подхватит `go.work`) |
| `KEELKIT_REF` | `HEAD` | коммит монорепо, с которого скиллы берут эталон |

Что проверяется: скиллы — из рабочего дерева (`--plugin-dir skills`), эталон и скрипты — из этого клона
(`KEELKIT_REPO`) на `KEELKIT_REF`. Агент работает без пользовательских настроек, плагинов и MCP
(`--setting-sources project,local`) и без вопросов человеку (`AskUserQuestion` запрещён).

В GitHub Actions — workflow `Evals` (`.github/workflows/evals.yml`), только ручной запуск: скилл, кейс, модель и
бюджет — входные параметры; нужен секрет `ANTHROPIC_API_KEY`; Claude Code — закреплённая версия
(`CLAUDE_CODE_VERSION`); `report.md` — в сводке запуска, каталог результатов — артефактом.

Результат — `<EVALS_OUT>/<время>-<скилл>/`: `report.md` (кейс, итог, время, стоимость, ходы, число упавших
проверок) и по каталогу на кейс: `agent.jsonl` (журнал агента, stream-json), `checks.log` (вывод проверок),
`site/` (сайт после скилла). Выход `make evals` — 0, если все кейсы прошли.

## Кейсы

**create-site** — по кейсу на каждый `spec/examples/<кейс>.yaml`: файл лежит в пустом каталоге как `site.yaml`,
агент создаёт сайт. Проверки: типы контента — как в примере; в `AGENTS.md` — строка эталона с коммитом
`KEELKIT_REF`; общие проверки сайта (ниже).

**upgrade-site**, кейс `reference-v0.2.0` — эталон с коммита `3ad749b` (последний, где `reference/` на
`images v0.2.0`), скопированный, как его копирует `create-site`: своё имя модуля, строка эталона в `AGENTS.md`,
локальный git. Агент обновляет сайт. Проверки: ветка `keelkit-upgrade-*`, `main` не тронут, коммит на шаг;
`images` — последняя опубликованная версия, `web.Picture` выводит `Placeholder` (шаг `UPGRADE.md` v0.3.0);
в `AGENTS.md` — «обновлён `upgrade-site` до эталона» `KEELKIT_REF`; общие проверки сайта; `migrate-check.sh` от
исходного коммита с `REPROCESS_BELOW=2`.

Общие проверки сайта (`lib.sh`, `site_checks`): `create-site/scripts/check-site.sh`, всё закоммичено,
`make spec-validate generate-check lint test test-isolated e2e e2e-browser` (нужен Chrome).

## Устройство

```
run.sh                  # раннер: каталог → setup → claude -p → check → report.md
lib.sh                  # step, ok/bad, site_checks — для check.sh
<скилл>/setup.sh        # --list — кейсы; <кейс> <каталог> — исходное состояние в <каталог>/site
<скилл>/prompt.md       # запрос агенту
<скилл>/check.sh        # <кейс> <каталог> — строки «  ok …» / «  FAIL …», выход 0 — всё ok
```

Новый скилл — каталог с этими тремя файлами. Проверки — объективные (код возврата, файлы, git), без оценки LLM.
Встроенный `claude plugin eval` не подошёл: его грейдеры (regex, file_exists, llm…) не проверяют код возврата
команды, а главный критерий здесь — `make test e2e` сайта.
