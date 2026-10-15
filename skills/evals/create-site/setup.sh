#!/usr/bin/env bash
# Эвал create-site: кейс — пример спецификации spec/examples/<кейс>.yaml, он лежит в пустом каталоге сайта как site.yaml.
set -eu
EVALS="$(cd "$(dirname "$0")/.." && pwd)"
EXAMPLES="$EVALS/../../spec/examples"
if [ "${1:-}" = --list ]; then
  for f in "$EXAMPLES"/*.yaml; do basename "$f" .yaml; done
  exit 0
fi
mkdir -p "$2/site"
cp "$EXAMPLES/$1.yaml" "$2/site/site.yaml"
