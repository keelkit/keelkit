#!/usr/bin/env bash
# Эвал upgrade-site: кейс — исходный сайт, который нужно обновить.
#   reference-v0.2.0 — эталон с коммита 3ad749b (последний, где reference на images v0.2.0), скопированный так же,
#   как его копирует create-site (fetch-reference.sh): своё имя модуля и образа Docker, строка эталона в AGENTS.md,
#   локальный git с одним коммитом. Воспроизводится из монорепо на любой машине.
# Коммит исходного состояния — в <каталог>/base: от него migrate-check.sh строит базу «до обновления».
set -eu
if [ "${1:-}" = --list ]; then echo reference-v0.2.0; exit 0; fi
CASE="$1"; W="$2"
case "$CASE" in
  reference-v0.2.0) FROM=3ad749b ;;
  *) echo "нет кейса $CASE" >&2; exit 1 ;;
esac

SITE="$W/site"
line="$(KEELKIT_REF="$FROM" "$KEELKIT_REPO/skills/create-site/scripts/fetch-reference.sh" "$SITE" eval-site | grep '^reference: ')"
commit="$(echo "$line" | awk '{print $3}')"
cd "$SITE"
sed 's/^DOCKER_IMAGE ?= keelkit-reference$/DOCKER_IMAGE ?= eval-site/' Makefile > Makefile.new && mv Makefile.new Makefile
# Строка эталона — как её пишет create-site (раздел «Версии»), после строки о Go.
awk -v c="$commit" '{print} /^- Go: / && !done {print "- Keelkit: плагин `keelkit` ≥ 0.1.0; создан `create-site` из эталона `" substr(c, 1, 7) "` (коммит монорепо `github.com/keelkit/keelkit`)."; done=1}' \
  AGENTS.md > AGENTS.md.new && mv AGENTS.md.new AGENTS.md
grep -q 'создан `create-site` из эталона' AGENTS.md
git init -q -b main
git add -A
git -c user.name=keelkit-evals -c user.email=evals@keelkit.invalid commit -qm "Сайт из эталона $FROM (эвал upgrade-site)"
git rev-parse HEAD > "$W/base"
