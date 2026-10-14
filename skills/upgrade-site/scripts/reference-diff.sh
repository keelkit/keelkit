#!/usr/bin/env bash
# Что изменилось в эталоне reference/ с того коммита, из которого создан сайт, до эталона текущей версии плагина.
#
#   reference-diff.sh <коммит эталона сайта> [файл для полного diff]
#   reference-diff.sh 6c5ffa0 /tmp/ref.diff
# Коммит — из строки «создан create-site из эталона <тег> <коммит>» в AGENTS.md сайта.
#
# Откуда берётся эталон, как в create-site/scripts/fetch-reference.sh:
#   - по умолчанию — клон https://github.com/keelkit/keelkit, новая сторона — тег плагина skills/v<версия>
#     (версия — из .claude-plugin/plugin.json плагина);
#   - KEELKIT_REPO=<путь к клону монорепо> — локальный клон, новая сторона — KEELKIT_REF (по умолчанию HEAD).
# Печатает строку «reference: <ref> <коммит>» (новая сторона — её пишут в AGENTS.md сайта после переноса), список
# коммитов, затронувших reference/, и diff --stat; полный diff — в файл (по умолчанию не пишется).
# Пути в diff — эталона (reference/…, модуль github.com/keelkit/keelkit/reference), не сайта. Совместим с bash 3.2.
set -eu

die() { printf 'reference-diff: %s\n' "$1" >&2; exit 1; }

FROM="${1:-}"
OUT="${2:-}"
[ -n "$FROM" ] || die "использование: reference-diff.sh <коммит эталона сайта> [файл для полного diff]"

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
  # Полная история нужна: старый коммит сайта может быть далеко от тега.
  git clone -q --filter=blob:none --no-checkout https://github.com/keelkit/keelkit.git "$REPO" \
    || die "не удалось клонировать https://github.com/keelkit/keelkit.git"
fi
TO="$(git -C "$REPO" rev-parse --verify "$REF^{commit}")" || die "нет ref $REF в $REPO (тег ставит make release MODULE=skills)"
OLD="$(git -C "$REPO" rev-parse --verify "$FROM^{commit}")" || die "нет коммита $FROM в $REPO"

echo "reference: $REF $TO"
if [ "$OLD" = "$TO" ]; then echo "эталон не изменился"; exit 0; fi
git -C "$REPO" merge-base --is-ancestor "$OLD" "$TO" || die "$FROM не предок $REF — эталон сайта новее плагина? Обновите плагин"

echo "коммиты, затронувшие reference/:"
git -C "$REPO" log --format='  %h %s' "$OLD..$TO" -- reference
echo "файлы:"
git -C "$REPO" diff --stat=160 "$OLD" "$TO" -- reference
if [ -n "$OUT" ]; then
  git -C "$REPO" diff "$OLD" "$TO" -- reference > "$OUT"
  echo "полный diff: $OUT"
fi
