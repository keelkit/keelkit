# Общее для check.sh эвалов (source). Совместим с bash 3.2.
# step <название> <команда…> — выполнить в текущем каталоге; вывод — в журнал под заголовком, итог — строкой ok/FAIL.

fail=0
ok()  { printf '  ok    %s\n' "$1"; }
bad() { printf '  FAIL  %s\n' "$1"; fail=1; }

step() {
  name="$1"; shift
  printf -- '---- %s: %s\n' "$name" "$*"
  if "$@" 2>&1 | sed 's/^/      /'; [ "${PIPESTATUS[0]}" = 0 ]; then ok "$name"; else bad "$name"; fi
}

# Типы контента — ключи второго уровня в content: (как в create-site/scripts/check-site.sh).
content_types() {
  awk '/^content:/ {on=1; next} on && /^[^[:space:]#]/ {on=0} on && /^  [a-z][a-z0-9_]*:/ {sub(/^  /, ""); sub(/:.*/, ""); print}' "$1" | sort | tr '\n' ' '
}

# Проверки, общие для любого сайта после скилла: структура, всё закоммичено, цели make как в шаге проверки скиллов.
site_checks() {
  step "check-site.sh" "$KEELKIT_REPO/skills/create-site/scripts/check-site.sh" .
  [ -z "$(git status --porcelain --untracked-files=all)" ] && ok "git: всё закоммичено" ||
    bad "git: незакоммиченные изменения: $(git status --porcelain | head -5 | tr '\n' ' ')"
  for t in spec-validate generate-check lint test test-isolated e2e e2e-browser; do
    step "make $t" make "$t"
  done
}
