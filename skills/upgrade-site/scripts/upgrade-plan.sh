#!/usr/bin/env bash
# Что можно обновить на сайте Keelkit: модули Keelkit из go.mod и валидатор spec (SPEC_VERSION в Makefile).
#
#   upgrade-plan.sh [каталог сайта]      (по умолчанию — текущий)
#
# Для каждого модуля печатает текущую и последнюю опубликованную версию (без pre-release), новую мажорную версию
# (путь /vN), если она вышла, и — для тех, что можно поднять, — пути к UPGRADE.md и CHANGELOG.md последней версии
# (из кэша модулей Go, то есть ровно опубликованные файлы) и заголовки разделов UPGRADE.md, попадающие в диапазон
# (текущая; последняя]: их и надо выполнить, от старых к новым.
# Ничего не меняет: go.mod и go.sum сайта не трогаются. Работает с GOWORK=off. Совместим с bash 3.2 (macOS).
set -eu

die() { printf 'upgrade-plan: %s\n' "$1" >&2; exit 1; }

SITE="${1:-.}"
cd "$SITE" || die "нет каталога $SITE"
[ -f go.mod ] || die "в $(pwd) нет go.mod"
[ -f site.yaml ] || die "в $(pwd) нет site.yaml — это не сайт Keelkit"

export GOWORK=off
PREFIX=github.com/keelkit/keelkit

# vA.B.C → число для сравнения (v0.10.0 > v0.9.0).
vnum() { echo "$1" | sed 's/^v//; s/[-+].*//' | awk -F. '{ printf "%d%06d%06d\n", $1, $2, $3 }'; }
# Последняя версия без pre-release; пусто, если модуля нет.
latest() {
  go list -m -versions "$1" 2>/dev/null | tr ' ' '\n' | sed 1d | grep -v -- '-' | tail -1 || true
}
# Каталог модуля версии в кэше (скачивает при необходимости).
moddir() { go mod download -json "$1@$2" 2>/dev/null | sed -n 's/^[[:space:]]*"Dir":[[:space:]]*"\(.*\)",*$/\1/p'; }
# Заголовки «## vX.Y.Z…» из UPGRADE.md в диапазоне (from; to].
sections() {
  f="$1"; from="$(vnum "$2")"; to="$(vnum "$3")"
  [ -f "$f" ] || return 0
  grep -E '^## v[0-9]+\.[0-9]+\.[0-9]+' "$f" | while read -r line; do
    v="$(echo "$line" | sed -E 's/^## (v[0-9]+\.[0-9]+\.[0-9]+).*/\1/')"
    n="$(vnum "$v")"
    if [ "$n" -gt "$from" ] && [ "$n" -le "$to" ]; then echo "      $line"; fi
  done
}
# Следующая мажорная версия пути: .../images → .../images/v2, .../images/v2 → .../images/v3.
nextmajor() {
  case "$1" in
    */v[0-9]*) base="${1%/v*}"; n="${1##*/v}"; echo "$base/v$((n + 1))" ;;
    *) echo "$1/v2" ;;
  esac
}

report() { # путь, текущая версия, что делать с файлами
  path="$1"; cur="$2"
  last="$(latest "$path")"
  [ -n "$last" ] || { echo "$path $cur: не удалось узнать версии (сеть, прокси Go?)"; return; }
  major="$(nextmajor "$path")"
  mlast="$(latest "$major")"
  if [ "$(vnum "$last")" -le "$(vnum "$cur")" ]; then
    echo "$path $cur: последняя"
  else
    echo "$path $cur → $last"
    dir="$(moddir "$path" "$last")"
    if [ -n "$dir" ]; then
      echo "    CHANGELOG: $dir/CHANGELOG.md"
      echo "    UPGRADE:   $dir/UPGRADE.md"
      s="$(sections "$dir/UPGRADE.md" "$cur" "$last")"
      if [ -n "$s" ]; then echo "    разделы UPGRADE.md к выполнению (от старых к новым — снизу вверх):"; echo "$s"
      else echo "    в UPGRADE.md разделов для этих версий нет — действий не требуется (сверь CHANGELOG)"; fi
    else
      echo "    не удалось скачать $path@$last"
    fi
  fi
  [ -z "$mlast" ] || echo "    вышла новая мажорная версия: $major $mlast — ломающие изменения, переход только с человеком"
}

echo "Сайт: $(pwd)"
echo "Модули Keelkit (go.mod):"
found=0
# Прямые зависимости из go.mod: «путь версия» в строке require или в блоке require ( … ).
for line in $(awk -v pre="$PREFIX/modules/" '{ if ($1 == "require") { $1 = "" ; $0 = $0 } } index($1, pre) == 1 && $2 ~ /^v/ && $0 !~ /\/\/ indirect/ { print $1 "@" $2 }' go.mod); do
  found=1
  report "${line%@*}" "${line#*@}"
done
[ "$found" = 1 ] || echo "  (нет модулей $PREFIX/modules/* — это не сайт Keelkit?)"

echo "Валидатор спецификации (SPEC_VERSION в Makefile):"
spec="$(sed -n 's/^SPEC_VERSION[[:space:]]*?=[[:space:]]*\(v[0-9.]*\).*/\1/p' Makefile 2>/dev/null | head -1)"
if [ -n "$spec" ]; then report "$PREFIX/spec" "$spec"; else echo "  в Makefile нет SPEC_VERSION ?= vX.Y.Z"; fi

echo "Эталон, из которого создан сайт (AGENTS.md):"
grep -n -E 'create-site.*эталон|эталон.*(skills/v|[0-9a-f]{7})' AGENTS.md 2>/dev/null | head -3 || echo "  строки об эталоне в AGENTS.md нет"
