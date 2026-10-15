#!/usr/bin/env bash
# Проверка обновления на копии данных: база, какой она была до обновления, → миграции нового кода → сайт стартует.
#
#   migrate-check.sh <коммит до обновления> [DATA_DIR с данными]
#   migrate-check.sh main                     # данные — seed старого кода
#   migrate-check.sh main ~/copy-of-prod/data # данные — копия живого сайта (site.db, uploads/, files/)
#
# Запускать в каталоге сайта, на ветке с обновлением (незакоммиченные правки учитываются).
#   1. Каталог данных: копия переданного DATA_DIR или новая база — миграции и seed кода <коммит> (git worktree).
#      Переданный каталог только копируется и не меняется.
#   2. make migrate нового кода на копии (снимок БД — в BACKUP_DIR рядом с копией).
#   3. REPROCESS_BELOW=N — make reprocess-images BELOW=N на копии (если UPGRADE.md требует повторной обработки).
#   4. make run на 127.0.0.1:PORT (по умолчанию 9190): /healthz — status ok и pending 0 у всех схем, главная — 200;
#      главная и до PAGES (по умолчанию 50) страниц из /sitemap.xml — 200, и все файлы, на которые они ссылаются
#      (/media/… и /files/… в src, srcset, href), — 200: повторная обработка меняет адреса вариантов и удаляет старые.
# Печатает /healthz и каталог копии (KEEP=1 — не удалять его). Выход 0 — всё в порядке. Совместим с bash 3.2 (macOS).
set -eu

die() { printf 'migrate-check: %s\n' "$1" >&2; exit 1; }

BASE="${1:-}"
SRC="${2:-}"
[ -n "$BASE" ] || die "использование: migrate-check.sh <коммит до обновления> [DATA_DIR с данными]"
[ -f site.yaml ] && [ -f Makefile ] || die "запускать в каталоге сайта (нет site.yaml или Makefile)"
git rev-parse --verify -q "$BASE^{commit}" >/dev/null || die "нет коммита $BASE"
[ -z "$SRC" ] || [ -f "$SRC/site.db" ] || die "в $SRC нет site.db"

PORT="${PORT:-9190}"
! lsof -t -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1 || die "порт $PORT занят — задайте другой: PORT=…"
SITE_DIR="$(pwd)"
WORK="$(mktemp -d)"
DATA="$WORK/data"
BACKUPS="$WORK/backups"
TREE="$WORK/before"
PID=""
# make run → go run → бинарник сайта: останавливаем того, кто слушает порт, затем make.
stop() {
  [ -n "$PID" ] || return 0
  for p in $(lsof -t -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null); do kill "$p" 2>/dev/null || true; done
  kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
  PID=""
}
cleanup() {
  stop
  [ ! -d "$TREE" ] || git -C "$SITE_DIR" worktree remove --force "$TREE" 2>/dev/null || true
  if [ "${KEEP:-0}" = 1 ]; then echo "копия оставлена: $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

if [ -n "$SRC" ]; then
  echo "== копия данных $SRC"
  # Снимок без -wal и -shm живого процесса: копию снимают с остановленного сайта или make backup.
  cp -R "$SRC" "$DATA"
else
  echo "== база до обновления: миграции и seed кода $BASE"
  git worktree add -q --detach "$TREE" "$BASE"
  make -C "$TREE" migrate seed DATA_DIR="$DATA" BACKUP_DIR="$WORK/backups-before" >"$WORK/before.log" 2>&1 \
    || { tail -30 "$WORK/before.log"; die "старый код не создал базу (лог выше)"; }
fi

echo "== make migrate нового кода"
make migrate DATA_DIR="$DATA" BACKUP_DIR="$BACKUPS" 2>&1 | tee "$WORK/migrate.log"
[ "${PIPESTATUS[0]}" = 0 ] || die "миграции не прошли"
# Обновление без новых миграций: применять нечего — site migrate снимок не делает, и он не нужен.
if grep -q "no pending migrations" "$WORK/migrate.log"; then
  echo "новых миграций нет"
else
  ls "$BACKUPS"/*.db >/dev/null 2>&1 || die "make migrate не сделал снимок БД в ${BACKUPS}"
fi

if [ -n "${REPROCESS_BELOW:-}" ]; then
  echo "== make reprocess-images BELOW=$REPROCESS_BELOW"
  make reprocess-images BELOW="$REPROCESS_BELOW" DATA_DIR="$DATA" BACKUP_DIR="$BACKUPS" || die "повторная обработка не прошла"
  echo "== повторный запуск ничего не делает"
  make reprocess-images BELOW="$REPROCESS_BELOW" DATA_DIR="$DATA" BACKUP_DIR="$BACKUPS" || die "повторный запуск повторной обработки не прошёл"
fi

echo "== make run на 127.0.0.1:$PORT"
make run ADDR="127.0.0.1:$PORT" DATA_DIR="$DATA" BACKUP_DIR="$BACKUPS" >"$WORK/run.log" 2>&1 &
PID=$!
health=""
i=0
while [ $i -lt 120 ]; do
  health="$(curl -fsS "http://127.0.0.1:$PORT/healthz" 2>/dev/null)" && break
  kill -0 "$PID" 2>/dev/null || { tail -30 "$WORK/run.log"; die "сайт не стартовал (лог выше)"; }
  sleep 1; i=$((i + 1))
done
[ -n "$health" ] || { tail -30 "$WORK/run.log"; die "/healthz не ответил за 120 с"; }
echo "/healthz: $health"
echo "$health" | grep -q '"status":"ok"' || die "/healthz: status не ok"
! echo "$health" | grep -Eq '"pending":[1-9]' || die "/healthz: есть непримененные миграции"
code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/")"
[ "$code" = 200 ] || die "главная отдала $code"
echo "главная: 200"

# Страницы: главная и адреса из sitemap.xml (с BASE_URL сайта — берётся только путь).
URL="http://127.0.0.1:$PORT"
{
  echo /
  curl -fsS "$URL/sitemap.xml" 2>/dev/null | grep -oE '<loc>[^<]+</loc>' | sed -E 's#</?loc>##g; s#^https?://[^/]+##'     | head -n "${PAGES:-50}" || true
} | sort -u > "$WORK/pages"
: > "$WORK/assets"
while read -r page; do
  code="$(curl -s -o "$WORK/page.html" -w '%{http_code}' "$URL$page")"
  [ "$code" = 200 ] || die "страница $page отдала $code"
  grep -oE '/(media|files)/[^" ,?#]+' "$WORK/page.html" >> "$WORK/assets" || true
done < "$WORK/pages"
sort -u -o "$WORK/assets" "$WORK/assets"
while read -r a; do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$URL$a")"
  [ "$code" = 200 ] || die "файл $a отдал $code (страницы ссылаются на него)"
done < "$WORK/assets"
echo "страниц: $(wc -l < "$WORK/pages" | tr -d ' '), файлов на них: $(wc -l < "$WORK/assets" | tr -d ' ') — все 200"
stop
echo "migrate-check: ok"
