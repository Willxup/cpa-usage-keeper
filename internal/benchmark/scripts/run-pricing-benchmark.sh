#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo 'usage: run-pricing-benchmark.sh OUTPUT_DIR EVENT_COUNT latest|five-dim [UTC_HOUR_RFC3339]' >&2
  exit 2
fi

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd -- "$script_dir/../../.." && pwd)
output_dir=$1
event_count=$2
scenario=$3
anchor=${4:-$(date -u +%Y-%m-%dT%H:00:00Z)}

if [[ $scenario != latest && $scenario != five-dim ]]; then
  echo 'scenario must be latest or five-dim' >&2
  exit 2
fi
if [[ ! $event_count =~ ^[0-9]+$ || $event_count -lt 120 ]]; then
  echo 'EVENT_COUNT must be at least 120' >&2
  exit 2
fi

mkdir -p -- "$output_dir"
output_dir=$(cd -- "$output_dir" && pwd)
for name in pricing-report.json pricing-old.db pricing-old.db-wal pricing-old.db-shm pricing-old.db-journal environment.txt pricingbench; do
  if [[ -e "$output_dir/$name" || -L "$output_dir/$name" ]]; then
    echo "benchmark output already exists: $output_dir/$name" >&2
    exit 2
  fi
done
cache_root=${PRICING_BENCHMARK_CACHE:-"$output_dir/.cache"}
mkdir -p -- "$cache_root/tmp" "$cache_root/go-cache"
export TZ=UTC GIN_MODE=release
export TMPDIR=${TMPDIR:-"$cache_root/tmp"}
export GOTMPDIR=${GOTMPDIR:-"$cache_root/tmp"}
export GOCACHE=${GOCACHE:-"$cache_root/go-cache"}

cd -- "$repo_root"
go build -tags sqlite_trace -o "$output_dir/pricingbench" ./internal/benchmark/cmd/pricingbench

{
  printf 'source_revision=%s\n' "${PRICING_BENCHMARK_REVISION:-unrecorded-see-source-hashes}"
  printf 'scenario=%s\nevents=%s\nanchor=%s\n' "$scenario" "$event_count" "$anchor"
  printf 'go=%s\n' "$(go version)"
  printf 'kernel=%s\n' "$(uname -srmo)"
  printf 'binary_sha256='; sha256sum "$output_dir/pricingbench"
  printf 'source_sha256:\n'
  sha256sum internal/benchmark/cmd/pricingbench/*.go internal/repository/pricing_legacy_*.go internal/service/pricing_recalculation*.go go.mod go.sum
} > "$output_dir/environment.txt"

"$output_dir/pricingbench" --root "$output_dir" --events "$event_count" --scenario "$scenario" --anchor "$anchor"
