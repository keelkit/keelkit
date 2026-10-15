#!/usr/bin/env bash
# Эвалы скиллов плагина keelkit: агент выполняет скилл в чистом каталоге, затем проверки решают, получилось ли.
# Запускается через make: make evals SKILL=create-site [CASE=blog] [MODEL=…] [BUDGET=…] [TIMEOUT=…]
#
#   run.sh <скилл> [кейс]
#
# Устройство скилла <s> — каталог skills/evals/<s>/:
#   setup.sh --list            — имена кейсов, по одному в строке;
#   setup.sh <кейс> <каталог>  — подготовить исходное состояние в пустом <каталог>/site (агент работает в нём);
#   prompt.md                  — запрос агенту (весь файл);
#   check.sh <кейс> <каталог>  — проверки результата в <каталог>/site; строки «  ok …» / «  FAIL …», выход 0 — всё ok.
# Проверки запускает раннер, а не агент: итогу агента эвал не доверяет.
#
# Агент — claude -p с плагином из рабочего дерева (skills/), без пользовательских настроек и плагинов
# (--setting-sources project,local), без вопросов человеку (AskUserQuestion запрещён). Эталон и скрипты скиллов берут
# монорепо из KEELKIT_REPO=<этот клон> на KEELKIT_REF (по умолчанию HEAD): проверяется то, что будет выпущено тегом.
# Результаты — $EVALS_OUT (по умолчанию $TMPDIR/keelkit-evals)/<время>-<скилл>/: report.md, по кейсу — agent.jsonl (журнал агента), checks.log,
# site/ (KEEP=0 — удалить каталоги сайтов после проверки). Выход 0 — все кейсы прошли. Совместим с bash 3.2 (macOS).
set -u

die() { printf 'evals: %s\n' "$1" >&2; exit 1; }

SKILL="${1:-}"
ONLY="${2:-}"
EVALS="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$EVALS/../.." && pwd)"
[ -n "$SKILL" ] && [ -f "$EVALS/$SKILL/setup.sh" ] ||
  die "укажите скилл: make evals SKILL=<$(cd "$EVALS" && ls -d */ | tr -d / | tr '\n' '|' | sed 's/|$//')>"
command -v claude >/dev/null || die "нет claude (Claude Code) в PATH"

MODEL="${MODEL:-}"
BUDGET="${BUDGET:-40}"
TIMEOUT="${TIMEOUT:-10800}"
KEEP="${KEEP:-1}"
REF="${KEELKIT_REF:-HEAD}"
REF_SHA="$(git -C "$ROOT" rev-parse --verify "$REF^{commit}")" || die "нет ref $REF"
[ -z "$(git -C "$ROOT" status --porcelain -- reference skills)" ] ||
  echo "evals: внимание — незакоммиченные правки в reference/ или skills/: эталон берётся с $REF, скиллы — из рабочего дерева" >&2

CASES="$("$EVALS/$SKILL/setup.sh" --list)" || die "$SKILL/setup.sh --list"
if [ -n "$ONLY" ]; then
  echo "$CASES" | grep -qx "$ONLY" || die "у $SKILL нет кейса $ONLY; есть: $(echo $CASES)"
  CASES="$ONLY"
fi

# Вне монорепо: иначе go.work монорепо подхватит сайт, и он соберётся на неопубликованном коде модулей.
OUT="${EVALS_OUT:-${TMPDIR:-/tmp}/keelkit-evals}/$(date +%Y%m%d-%H%M%S)-$SKILL"
mkdir -p "$OUT"
case "$(cd "$OUT" && pwd -P)/" in "$(cd "$ROOT" && pwd -P)/"*) die "EVALS_OUT внутри монорепо: сайт подхватит его go.work";; esac
REPORT="$OUT/report.md"
{
  echo "# Эвалы $SKILL — $(date '+%Y-%m-%d %H:%M')"
  echo
  echo "Эталон: $REF ($REF_SHA); скиллы — рабочее дерево; модель: ${MODEL:-по умолчанию}; бюджет на кейс: \$$BUDGET."
  echo
  echo "| Кейс | Итог | Агент | Минут | \$ | Ходов | Проверки FAIL |"
  echo "|---|---|---|---|---|---|---|"
} > "$REPORT"

# Значение поля из последней строки result журнала stream-json (без jq).
result_field() { grep '"type":"result"' "$1" 2>/dev/null | tail -1 | sed -n "s/.*\"$2\":\([0-9.]*\).*/\1/p"; }

failed=0
for c in $CASES; do
  W="$OUT/$c"
  mkdir -p "$W"
  echo "== $SKILL/$c → $W"
  if ! KEELKIT_REPO="$ROOT" KEELKIT_REF="$REF_SHA" "$EVALS/$SKILL/setup.sh" "$c" "$W" > "$W/setup.log" 2>&1; then
    echo "   setup не удался: $W/setup.log"
    echo "| $c | FAIL | setup | | | | см. setup.log |" >> "$REPORT"
    failed=1; continue
  fi

  start=$(date +%s)
  args="-p --plugin-dir $ROOT/skills --setting-sources project,local --strict-mcp-config --no-chrome
    --no-session-persistence --permission-mode bypassPermissions --disallowedTools AskUserQuestion
    --output-format stream-json --verbose --max-budget-usd $BUDGET"
  [ -z "$MODEL" ] || args="$args --model $MODEL"
  # CLAUDECODE: раннер могут запустить из сессии Claude Code — вложенный запуск тогда отказывается стартовать.
  ( cd "$W/site" && unset CLAUDECODE && KEELKIT_REPO="$ROOT" KEELKIT_REF="$REF_SHA" \
      claude $args < "$EVALS/$SKILL/prompt.md" > "$W/agent.jsonl" 2> "$W/agent.err" ) &
  pid=$!
  ( sleep "$TIMEOUT" && pkill -P "$pid" 2>/dev/null; kill "$pid" 2>/dev/null ) &
  dog=$!
  wait "$pid"; agent_rc=$?
  kill "$dog" 2>/dev/null; wait "$dog" 2>/dev/null
  minutes=$(( ($(date +%s) - start + 59) / 60 ))
  cost="$(result_field "$W/agent.jsonl" total_cost_usd)"
  turns="$(result_field "$W/agent.jsonl" num_turns)"
  [ "$agent_rc" = 0 ] && agent=ok || agent="код $agent_rc"
  # Агент оборван (лимит, бюджет, ошибка API) — причина из result: провал не скилла, а прогона.
  if grep '"type":"result"' "$W/agent.jsonl" 2>/dev/null | tail -1 | grep -q '"is_error":true'; then
    why="$(grep '"type":"result"' "$W/agent.jsonl" | tail -1 | sed -n 's/.*"result":"\([^"]*\)".*/\1/p' | cut -c1-80)"
    agent="прерван: ${why:-is_error}"
  fi
  echo "   агент: $agent, $minutes мин, \$${cost:-?}, ходов ${turns:-?}"

  KEELKIT_REPO="$ROOT" KEELKIT_REF="$REF_SHA" "$EVALS/$SKILL/check.sh" "$c" "$W" > "$W/checks.log" 2>&1
  check_rc=$?
  fails="$(grep -c '^  FAIL' "$W/checks.log")"
  grep '^  FAIL' "$W/checks.log" | sed 's/^/ /'
  if [ "$check_rc" = 0 ] && [ "$fails" = 0 ]; then res=ok; else res=FAIL; failed=1; fi
  echo "   итог: $res"
  echo "| $c | $res | $agent | $minutes | ${cost:-?} | ${turns:-?} | $fails |" >> "$REPORT"
  [ "$KEEP" = 1 ] || rm -rf "$W/site"
done

{
  echo
  echo "Подробности — \`<кейс>/checks.log\`, журнал агента — \`<кейс>/agent.jsonl\`."
} >> "$REPORT"
echo "отчёт: $REPORT"
exit "$failed"
