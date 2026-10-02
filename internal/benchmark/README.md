# Benchmarks

[简体中文](README.zh.md)

Choose a report to see measured results, or a guide to reproduce a test. All datasets are synthetic.

| Suite | Measures | Latest report | Run guide |
| --- | --- | --- | --- |
| Capacity | Sustained ingestion and Dashboard latency at 1, 2 and 4 CPUs | [Results](reports/capacity/README.md) | [Capacity guide](guides/capacity.md) |
| Pricing | First migration, historical recalculation and query costs at 1m / 10m events | [Results](reports/pricing/README.md) | [Pricing guide](guides/pricing.md) |

The capacity report predates persisted pricing and is not a capacity measurement of the current implementation. The pricing report tests migration and recalculation at a fixed incoming rate; it does not replace the capacity suite.

## Layout

```text
internal/benchmark/
├── capacity/          # Capacity implementation
├── guides/            # Capacity and pricing run guides
├── cmd/               # benchctl and pricingbench commands
├── legacy/test/       # Go microbenchmarks
├── manifest/          # Dataset and workload configuration
├── schema/            # Result contracts
├── scripts/           # Run and comparison scripts
└── reports/
    ├── capacity/      # Latest completed capacity report
    └── pricing/       # Latest completed pricing report and results/
```

## Updating results

Run into a fresh directory outside the repository. Keep databases, WAL, binaries and raw logs there. After the complete run is checked, replace the corresponding report and its result files together. Keep the previous report until the replacement is complete; Git retains the published history. Do not create date, release or run-number directories in `reports/`.

Each report includes the conclusion, environment and limits, dataset, measured code or binary identity, results, and reproduction steps. Keep English and Chinese versions aligned. Report measured outcomes and limitations accurately; keep investigation notes and superseded intermediate runs in local records. Do not present unexecuted fields as successful zero values.

Publish only reviewed Markdown, structured measurements and source checksums. Remove hostnames, SSH aliases, private addresses, usernames and local absolute paths. Preserve hardware specifications, software versions, workload parameters and measured values. Source inventories describe the original run, not later documentation or privacy-only edits.
