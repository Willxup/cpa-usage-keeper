# Pricing benchmark run guide

[简体中文](pricing.zh.md) · [Report](../reports/pricing/README.md) · [All benchmarks](../README.md)

## Requirements

Linux amd64, Go and GCC for the SQLite build; Python 3 for the HTTP comparison. Use a dedicated test environment with space for the generated database, WAL and backup. The runner generates its own synthetic data.

## Run

Run from the repository root into a fresh directory outside the repository. Provision the resource limits in the report separately; the script does not impose them. Run scenarios sequentially and allow space for generated data, WAL and a full backup.

```bash
export TZ=UTC GOMEMLIMIT=5GiB
export PRICING_BENCHMARK_REVISION=$(git rev-parse HEAD)
internal/benchmark/scripts/run-pricing-benchmark.sh <new-output-dir> 1000000 latest 2026-09-23T00:00:00Z
```

Use `10000000 latest` for 10m, or `1000000 five-dim` for the pending historical migrations. The script builds the current checkout; `PRICING_BENCHMARK_REVISION` records identity but does not check out that revision. Exact original source and binary checksums remain in [report results](../reports/pricing/results/). The original runs used built binaries; later removal of hostname collection does not change the original inventories. The 1m startup inventory predates the final comparison script; its actual hash is recorded in [environment.json](../reports/pricing/results/environment.json).

For the matched HTTP comparison, build both product revisions and use the old backup and upgraded database from the 1m run:

```bash
python3 internal/benchmark/scripts/compare-pricing-queries.py \
  --old-binary <old-binary> --new-binary <new-binary> \
  --backup <M1-backup.db> --database <upgraded-database.db> \
  --root <new-comparison-dir> --anchor 2026-09-23T00:00:00Z --samples 5
```

## Output

Each fresh output directory contains `pricing-report.json`, `environment.txt`, the benchmark binary and the synthetic database with its migration backup. The script rejects conflicting existing output files and does not remove them after a run. Review results before replacing the published report; keep database files and raw logs outside the repository.
