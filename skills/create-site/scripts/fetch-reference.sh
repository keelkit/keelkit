#!/usr/bin/env bash
# Копирует эталонный сайт reference/ из монорепо Keelkit в каталог нового сайта и меняет путь Go-модуля.
#
#   fetch-reference.sh <каталог сайта> <путь модуля сайта>
#   fetch-reference.sh ./my-site github.com/acme/my-site
#
# Откуда берётся эталон:
#   - по умолчанию — клон github.com/voffkin/keelkit по SSH на теге плагина skills/v<версия>
#     (версия — из .claude-plugin/plugin.json плагина): скилл и эталон всегда одной версии;
#   - KEELKIT_REPO=<путь к клону монорепо> — локальный клон (разработка скиллов, эвалы); коммит — KEELKIT_REF
#     (по умолчанию HEAD: незакоммиченные правки не попадают).
# Каталог сайта должен быть пустым (допускаются .git и site.yaml — они не перезаписываются).
# Печатает строку «reference: <тег или ref> <коммит>» — её записывают в AGENTS.md сайта.
# Совместим с bash 3.2 (macOS).
set -eu

die() { printf 'fetch-reference: %s\n' "$1" >&2; exit 1; }

TARGET="${1:-}"
MODPATH="${2:-}"
[ -n "$TARGET" ] && [ -n "$MODPATH" ] || die "использование: fetch-reference.sh <каталог сайта> <путь модуля, например github.com/acme/my-site>"
echo "$MODPATH" | grep -Eq '^[a-z0-9.-]+(/[A-Za-z0-9._~-]+)+$' || die "путь модуля «$MODPATH»: нужен вид github.com/<владелец>/<сайт>"

mkdir -p "$TARGET"
TARGET="$(cd "$TARGET" && pwd)"
extra="$(cd "$TARGET" && ls -A | grep -vxE '\.git|site\.yaml' || true)"
[ -z "$extra" ] || die "каталог $TARGET не пустой: $(echo $extra)"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$SCRIPT_DIR/../..}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [ -n "${KEELKIT_REPO:-}" ]; then
  REPO="$(cd "$KEELKIT_REPO" && pwd)"
  REF="${KEELKIT_REF:-HEAD}"
else
  VERSION="$(sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)".*/\1/p' "$PLUGIN_ROOT/.claude-plugin/plugin.json" | head -1)"
  [ -n "$VERSION" ] || die "нет версии в $PLUGIN_ROOT/.claude-plugin/plugin.json"
  REF="skills/v$VERSION"
  REPO="$TMP/keelkit"
  git clone -q --depth 1 --branch "$REF" git@github.com:voffkin/keelkit.git "$REPO" \
    || die "не удалось клонировать git@github.com:voffkin/keelkit.git на теге $REF (нужен SSH-ключ с доступом: ssh -T git@github.com; тег ставит make release MODULE=skills)"
fi
COMMIT="$(git -C "$REPO" rev-parse --verify "$REF^{commit}")" || die "нет ref $REF в $REPO"

# Только файлы под git: data/, bin/, backups/ эталона не попадают.
mkdir -p "$TMP/out"
git -C "$REPO" archive "$COMMIT" reference | tar -x -C "$TMP/out"
[ -f "$TMP/out/reference/go.mod" ] || die "в $REF нет reference/go.mod"
[ ! -f "$TARGET/site.yaml" ] || rm "$TMP/out/reference/site.yaml"
(cd "$TMP/out/reference" && tar -cf - .) | (cd "$TARGET" && tar -xf -)

# Путь модуля: импорты в Go и templ (и сгенерированном коде), go.mod, конфиги.
OLD="github.com/voffkin/keelkit/reference"
grep -rlF "$OLD" --exclude-dir=.git "$TARGET" | while read -r f; do
  sed "s#$OLD#$MODPATH#g" "$f" > "$TMP/sed" && cat "$TMP/sed" > "$f"
done
! grep -rqF "$OLD" --exclude-dir=.git "$TARGET" || die "остались ссылки на $OLD"

echo "reference: $REF $COMMIT"
