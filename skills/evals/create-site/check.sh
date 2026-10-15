#!/usr/bin/env bash
# Проверки create-site: типы контента — как в примере, строка эталона в AGENTS.md, check-site.sh и цели make сайта.
set -u
. "$(dirname "$0")/../lib.sh"
CASE="$1"
cd "$2/site" || { bad "нет каталога сайта"; exit 1; }

want="$(content_types "$KEELKIT_REPO/spec/examples/$CASE.yaml")"
got="$(content_types site.yaml)"
[ "$want" = "$got" ] && ok "типы контента как в примере: $got" || bad "типы контента: в примере «${want}», в site.yaml «${got}»"
short="$(echo "$KEELKIT_REF" | cut -c1-7)"
grep -q "эталона.*$short" AGENTS.md && ok "AGENTS.md: создан из эталона $short" || bad "AGENTS.md: нет строки «создан … из эталона … ${short}»"
site_checks
exit "$fail"
