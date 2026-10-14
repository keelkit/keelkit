#!/usr/bin/env bash
# Проверка структуры сайта Keelkit на соответствие конвенциям эталона (reference/AGENTS.md, «Структура»).
# Не заменяет тесты сайта (make test, e2e): проверяет то, что тесты не видят, — файлы, каталоги, цели make,
# соответствие типов контента в site.yaml пакетам internal/content/<тип>/.
#
#   check-site.sh [каталог сайта]      (по умолчанию — текущий)
#
# Выход 0 — всё в порядке; иначе — список нарушений и выход 1. Совместим с bash 3.2 (macOS).
set -u

SITE="${1:-.}"
cd "$SITE" || { echo "check-site: нет каталога $SITE" >&2; exit 1; }

fail=0
bad() { printf '  FAIL  %s\n' "$1"; fail=1; }
ok()  { printf '  ok    %s\n' "$1"; }

for f in site.yaml AGENTS.md CLAUDE.md Makefile go.mod go.sum sqlc.yaml .golangci.yml Dockerfile .dockerignore \
  cmd/site/main.go cmd/site/server.go cmd/site/seed.go cmd/site/user.go cmd/site/backup.go \
  cmd/site/spec_test.go cmd/site/schema_test.go cmd/site/e2e_test.go cmd/site/e2e_admin_test.go cmd/site/browser_test.go \
  db/migrations/embed.go db/schema.sql \
  deploy/site.service deploy/site-migrate.service deploy/site-backup.service deploy/site-backup.timer deploy/site.env.example \
  static/static.go static/style.css static/admin.css; do
  [ -f "$f" ] && ok "$f" || bad "нет файла $f"
done
for d in internal/admin internal/blocks internal/database internal/listfield internal/media internal/menu \
  internal/seo internal/seofields internal/settings internal/slug internal/web; do
  [ -d "$d" ] && ok "$d/" || bad "нет каталога $d/"
done

[ "$(cat CLAUDE.md 2>/dev/null)" = "@AGENTS.md" ] && ok "CLAUDE.md = @AGENTS.md" || bad "CLAUDE.md должен состоять из одной строки @AGENTS.md"

# Цели Makefile — те же, что у эталона.
for t in help build docker run dev migrate backup seed user-create reprocess-images reset generate generate-check \
  schema test test-isolated e2e e2e-browser lint deps-check spec-validate; do
  grep -Eq "^$t:.*## " Makefile && ok "make $t" || bad "в Makefile нет цели $t с описанием ##"
done
grep -q '^export GOPRIVATE' Makefile && ok "Makefile: GOPRIVATE" || bad "Makefile: нет export GOPRIVATE"

# go.mod: без replace, путь модуля — не эталона, модули Keelkit — опубликованные версии.
grep -Eq '^[[:space:]]*replace([[:space:]]|\()' go.mod && bad "в go.mod есть replace" || ok "в go.mod нет replace"
if [ "$(awk '$1 == "module" {print $2; exit}' go.mod)" != github.com/voffkin/keelkit/reference ] &&
  grep -rqF "github.com/voffkin/keelkit/reference" --include='*.go' --include='*.templ' --include=go.mod . ; then
  bad "остались импорты github.com/voffkin/keelkit/reference"
else
  ok "нет импортов эталона"
fi
[ ! -f go.work ] && ok "нет go.work" || bad "go.work в сайте не нужен: модули — опубликованные версии"

# Типы контента: ключи второго уровня в content: файла site.yaml (стиль эталона — отступ 2 пробела).
types="$(awk '/^content:/ {on=1; next} on && /^[^[:space:]#]/ {on=0} on && /^  [a-z][a-z0-9_]*:/ {sub(/^  /, ""); sub(/:.*/, ""); print}' site.yaml)"
[ -n "$types" ] && ok "типы в site.yaml: $(echo $types)" || bad "в site.yaml не найдены типы контента (content:)"
for t in $types; do
  d="internal/content/$t"
  [ -d "$d" ] || { bad "нет пакета $d/ для типа $t"; continue; }
  for f in queries.sql admin.go admin.templ; do [ -f "$d/$f" ] || bad "нет $d/$f"; done
  grep -q "queries: $d/queries.sql" sqlc.yaml || bad "нет записи $d в sqlc.yaml"
  LC_ALL=C grep -q '[^ -~	]' "$d/queries.sql" 2>/dev/null && bad "$d/queries.sql: не ASCII (sqlc режет запросы)"
done
for d in internal/content/*/; do
  t="$(basename "$d")"
  echo " $types " | tr '\n' ' ' | grep -q " $t " || bad "пакет internal/content/$t/ — типа $t нет в site.yaml"
done

# Миграции: последовательная нумерация с 00001, только Up.
n=0
for f in db/migrations/*.sql; do
  n=$((n + 1))
  want="$(printf '%05d' $n)"
  case "$(basename "$f")" in "$want"_*) ;; *) bad "миграция $f: ожидался номер $want";; esac
  grep -q -- '-- +goose Down' "$f" && bad "$f: down-секции не пишем"
done
[ "$n" -gt 0 ] && ok "миграций: $n" || bad "нет миграций в db/migrations/"

# Сгенерированный код закоммичен рядом с исходниками.
for f in $(find . -name '*.templ' -not -path './.git/*'); do
  [ -f "${f%.templ}_templ.go" ] || bad "нет ${f%.templ}_templ.go (make generate)"
done

if [ "$fail" != 0 ]; then echo "check-site: есть нарушения"; exit 1; fi
echo "check-site: структура соответствует конвенциям"
