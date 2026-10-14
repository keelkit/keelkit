#!/usr/bin/env bash
# Релиз модуля монорепо: проверки и тег <каталог>/vX.Y.Z, затем push тега в origin.
# Запускается только через make: make release MODULE=images VERSION=v0.1.0 [DRY_RUN=1]
# MODULE=skills — релиз плагина keelkit (skills/): версия в skills/.claude-plugin/plugin.json должна совпадать
# с VERSION; тег фиксирует и эталон reference/, который create-site копирует по этому тегу, поэтому проверки — make check целиком.
# DRY_RUN=1 выполняет все проверки и показывает, что было бы сделано, ничего не меняя.
# Совместим с bash 3.2 (macOS).
set -u

MODULE="${1:-}"
VERSION="${2:-}"
DRY_RUN="${3:-}"

fail=0
ok()   { printf '  ok    %s\n' "$1"; }
bad()  { printf '  FAIL  %s\n' "$1"; fail=1; }
die()  { printf 'release: %s\n' "$1" >&2; exit 1; }

[ -n "$MODULE" ] && [ -n "$VERSION" ] || die "укажите MODULE и VERSION, например make release MODULE=images VERSION=v0.1.0"
PLUGIN=0
case "$MODULE" in
  spec) DIR=spec ;;
  skills) DIR=skills; PLUGIN=1 ;;
  reference) die "reference/ — эталонный сайт, он не выпускается (его версию фиксирует релиз плагина: MODULE=skills)" ;;
  *) DIR="modules/$MODULE" ;;
esac
[ "$PLUGIN" = 1 ] || [ -f "$DIR/go.mod" ] || die "нет $DIR/go.mod"
echo "$VERSION" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' \
  || die "VERSION=$VERSION: нужен semver без суффиксов, например v0.1.0"

TAG="$DIR/$VERSION"
NUM="${VERSION#v}"
MAJOR="${NUM%%.*}"
echo "release $TAG"

# 1. Тег неизменяем: существующий не переставляется (приватные модули не защищены sum.golang.org).
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
  bad "тег $TAG уже есть локально — опубликованный тег не переставляют, выпустите следующую версию"
else
  ok "тега $TAG нет локально"
fi
HAS_ORIGIN=0
if git remote get-url origin >/dev/null 2>&1; then
  HAS_ORIGIN=1
  if remote_tags="$(git ls-remote --tags origin "refs/tags/$TAG" 2>&1)"; then
    if [ -n "$remote_tags" ]; then bad "тег $TAG уже есть в origin"; else ok "тега $TAG нет в origin"; fi
  else
    bad "не удалось прочитать теги origin: $remote_tags"
  fi
else
  bad "нет remote origin (git remote add origin git@github.com:voffkin/keelkit.git)"
fi

# 2. Чистое дерево, ветка main, HEAD уже в origin/main (иначе CI для него не запускался).
if [ -z "$(git status --porcelain --untracked-files=all)" ]; then ok "рабочее дерево чистое"; else bad "есть незакоммиченные изменения (git status)"; fi
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$BRANCH" = main ]; then ok "ветка main"; else bad "ветка $BRANCH, релиз — только из main"; fi
HEAD_SHA="$(git rev-parse HEAD)"
if [ "$HAS_ORIGIN" = 1 ]; then
  git fetch -q origin main 2>/dev/null
  if [ "$(git rev-parse -q --verify origin/main 2>/dev/null)" = "$HEAD_SHA" ]; then ok "HEAD совпадает с origin/main"; else bad "HEAD не совпадает с origin/main — сначала git push"; fi
fi

# 3. Плагин: версия в plugin.json совпадает с VERSION. Модуль: путь соответствует мажорной версии (v2+ — суффикс /vN).
if [ "$PLUGIN" = 1 ]; then
  PVER="$(sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)".*/\1/p' skills/.claude-plugin/plugin.json | head -1)"
  if [ "$PVER" = "$NUM" ]; then ok "plugin.json: version $NUM"; else bad "skills/.claude-plugin/plugin.json: version «$PVER», а релиз $VERSION"; fi
else
MODPATH="$(awk '$1 == "module" {print $2; exit}' "$DIR/go.mod")"
SUFFIX="$(echo "$MODPATH" | grep -Eo '/v[0-9]+$' || true)"
if [ "$MAJOR" -ge 2 ]; then
  if [ "$SUFFIX" = "/v$MAJOR" ]; then ok "путь модуля $MODPATH"; else bad "для $VERSION путь модуля должен оканчиваться на /v$MAJOR: $MODPATH"; fi
else
  if [ -z "$SUFFIX" ]; then ok "путь модуля $MODPATH"; else bad "для $VERSION путь модуля без суффикса /vN: $MODPATH"; fi
fi
if grep -Eq '^[[:space:]]*replace([[:space:]]|\()' "$DIR/go.mod"; then bad "в $DIR/go.mod есть replace"; else ok "в go.mod нет replace"; fi
fi

# 4. CHANGELOG.md: раздел «## [X.Y.Z]».
SECTION=""
if [ -f "$DIR/CHANGELOG.md" ] && grep -Eq "^## \[$NUM\]" "$DIR/CHANGELOG.md"; then
  ok "CHANGELOG.md: раздел [$NUM]"
  SECTION="$(awk -v v="## [$NUM]" 'index($0, v) == 1 {on = 1; next} on && /^## \[/ {exit} on' "$DIR/CHANGELOG.md")"
else
  bad "$DIR/CHANGELOG.md: нет раздела «## [$NUM]»"
fi

# 5. UPGRADE.md: раздел «## vX.Y.Z», если изменения требуют действий (Changed/Removed/Deprecated или ссылка на UPGRADE.md).
if echo "$SECTION" | grep -Eq '^### (Changed|Removed|Deprecated)|UPGRADE\.md'; then
  if [ -f "$DIR/UPGRADE.md" ] && grep -Eq "^## $VERSION([^0-9.]|$)" "$DIR/UPGRADE.md"; then
    ok "UPGRADE.md: раздел $VERSION"
  else
    bad "изменения требуют действий, а в $DIR/UPGRADE.md нет раздела «## $VERSION»"
  fi
else
  ok "UPGRADE.md не требуется (в CHANGELOG нет Changed/Removed/Deprecated)"
fi

# 6. Локальные проверки CI (make check). CI на GitHub пока запускается только вручную (решение человека,
# см. «CI» в AGENTS.md), поэтому релиз проверяет HEAD сам. Запускаются, только если прошло всё остальное: это долго.
CHECK_MODULE="$MODULE"
[ "$PLUGIN" = 0 ] || CHECK_MODULE=""
if [ "$fail" = 0 ]; then
  echo "  ...   make check MODULE=$CHECK_MODULE"
  if make --no-print-directory check MODULE="$CHECK_MODULE"; then ok "make check MODULE=$CHECK_MODULE"; else bad "make check MODULE=$CHECK_MODULE"; fi
else
  echo "  skip  make check MODULE=$CHECK_MODULE — сначала исправьте ошибки выше"
fi

if [ "$fail" != 0 ]; then
  echo "release: проверки не пройдены, тег не поставлен"
  exit 1
fi
if [ -n "$DRY_RUN" ] && [ "$DRY_RUN" != 0 ]; then
  echo "DRY_RUN: все проверки пройдены; будет выполнено:"
  echo "  git tag -a $TAG -m \"$TAG\""
  echo "  git push origin refs/tags/$TAG"
  exit 0
fi
git tag -a "$TAG" -m "$TAG" || exit 1
git push origin "refs/tags/$TAG" || { echo "release: тег $TAG поставлен локально, но не отправлен — повторите git push origin refs/tags/$TAG"; exit 1; }
echo "release: $TAG опубликован"
