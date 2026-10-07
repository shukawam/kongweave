#!/usr/bin/env bash
set -euo pipefail
repo_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_dir"
command -v deck >/dev/null
command -v kongctl >/dev/null
verification_dir="$(mktemp -d "${TMPDIR:-/tmp}/kongweave-verify.XXXXXX")"
trap 'rm -rf "$verification_dir"' EXIT
go build -trimpath -o "$verification_dir/kongweave" ./cmd/kongweave
for format in deck kongctl; do
  for environment in dev prod; do
    "$verification_dir/kongweave" build "examples/$format/overlays/$environment" \
      --output "$verification_dir/$format-$environment"
  done
done
for environment in dev prod; do
  deck file validate --analytics=false "$verification_dir/deck-$environment/config.yaml"
  kongctl --no-telemetry --log-file "$verification_dir/kongctl.log" lint \
    -f "$verification_dir/kongctl-$environment/config.yaml" -r examples/kongctl/ruleset.yaml
done
"$verification_dir/kongweave" explain examples/deck/overlays/prod \
  --kind plugins --name rate-limiting --scope route=catalog-public --field /config/minute
"$verification_dir/kongweave" explain examples/kongctl/overlays/prod \
  --kind ai_gateway_models --ref example-chat --scope ai_gateway=example-ai \
  --field /config/route/paths
printf '%s\n' 'Offline checks passed (kongctl lint uses example rules, not full native validation).'
