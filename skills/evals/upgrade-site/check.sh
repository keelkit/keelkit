#!/usr/bin/env bash
# Проверки upgrade-site: ветка обновления (main не тронут), images — последняя версия и её шаги UPGRADE.md,
# строка эталона в AGENTS.md, check-site.sh и цели make сайта, migrate-check.sh от исходного коммита (запускает эвал).
set -u
. "$(dirname "$0")/../lib.sh"
W="$2"
BASE="$(cat "$W/base")"
cd "$W/site" || { bad "нет каталога сайта"; exit 1; }

branch="$(git rev-parse --abbrev-ref HEAD)"
case "$branch" in keelkit-upgrade-*) ok "ветка $branch" ;; *) bad "текущая ветка $branch, ожидалась keelkit-upgrade-*" ;; esac
[ "$(git rev-parse main)" = "$BASE" ] && ok "main не изменён" || bad "main сдвинут с исходного коммита"
n="$(git rev-list --count "$BASE..HEAD")"
[ "$n" -ge 2 ] && ok "коммитов в ветке: $n" || bad "коммитов в ветке: $n (ожидался коммит на каждый шаг)"

latest="$(GOWORK=off go list -m github.com/keelkit/keelkit/modules/images@latest | awk '{print $2}')"
have="$(awk '$1 == "github.com/keelkit/keelkit/modules/images" {print $2}' go.mod)"
[ -n "$latest" ] && [ "$have" = "$latest" ] && ok "images $have" || bad "images $have, последняя — $latest"
grep -q 'replace' go.mod && bad "в go.mod есть replace" || ok "в go.mod нет replace"
# images v0.3.0 (UPGRADE.md): заглушка Placeholder выводится в web.Picture.
grep -rq 'Placeholder' internal/web && ok "web.Picture: Placeholder" || bad "internal/web не использует Image.Placeholder"
short="$(echo "$KEELKIT_REF" | cut -c1-7)"
grep -q "обновлён \`upgrade-site\` до эталона.*$short" AGENTS.md && ok "AGENTS.md: обновлён до эталона $short" ||
  bad "AGENTS.md: нет строки «обновлён upgrade-site до эталона … ${short}»"

site_checks
step "migrate-check.sh $(echo "$BASE" | cut -c1-7) REPROCESS_BELOW=2" \
  env REPROCESS_BELOW=2 "$KEELKIT_REPO/skills/upgrade-site/scripts/migrate-check.sh" "$BASE"
exit "$fail"
