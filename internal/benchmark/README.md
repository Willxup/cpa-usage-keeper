<p align="center">
  <a href="./README.md"><strong>English</strong></a> ｜ <a href="./README.zh.md">简体中文</a>
</p>

# Capacity Benchmark

`internal/benchmark` contains both code-level Go microbenchmarks and a production-style capacity suite that exercises the complete Keeper binary, SQLite, Redis, and real Dashboard HTTP queries. The formal suite is `capacity-v1` and targets `linux/amd64` only.

## Test Objectives

The capacity suite changes only the CPU available to Keeper: 1C, 2C, and 4C. All profiles reuse the same canonical database. Keeper memory is unlimited during a run, and the suite reads cgroup v2 `memory.peak` afterward. This includes the Keeper process, SQLite mmap/page cache, and database cache charged to the Keeper cgroup.

The formal results report:

- the highest verified five-minute ingestion pass and the lowest observed failure above it;
- the corresponding five-minute Core Dashboard pass/fail interval under the three-second aggregate p99 SLA;
- CPU utilization and Keeper cgroup peak memory at the capacity point;
- a separate Analysis Latency diagnostic summary with sample count, errors, status, and latency percentiles;
- a conservative sustained-rate recommendation set to 70% of Dashboard capacity.

Core p95 is retained as an experience indicator but is not a `capacity-v1` pass gate. Analysis Latency is a heavier diagnostic query and does not participate in the three-second Core Dashboard gate.

## Reference Dataset

The reference dataset ID is `reference-3m`.

| Item | Count |
| --- | ---: |
| Total events | 3,205,740 |
| Events in the 30 days ending at generation | 1,226,326 |
| Hot events in the latest 90 days | 3,205,740 |
| Archived events | 0 |
| Identities | 500 |
| Models | 50 |
| API keys | 50 |
| Database size | 2,044,776,448 bytes (about 1.90 GiB) |

The dataset retains 90 days of active events. The archive table is intentionally empty because the production Dashboard paths covered by this suite do not query cold events.

The 50 API keys are deterministically assigned to 15 high-, 25 medium-, and 10 low-usage keys. Per-key weights are 10:3:1, and `usage_events` are normalized across those weights. All 500 identities, 50 models, and 50 API keys are active and referenced by events. Failed events account for 1% of the generated workload.

The canonical database must pass row-count, cardinality, orphan-reference, token-semantics, derived rollup/checkpoint, `PRAGMA quick_check`, dimension-distribution, and semantic-fingerprint validation. Its metadata also binds the configured failure rate and traffic tiers, so a canonical generated for a different workload cannot be relabeled by a changed manifest. Generation uses the actual generation time as the dataset anchor. A formal run rejects a dataset whose newest event is more than seven days old and records the effective 30-day query count in `dataset-validation.json`.

Dataset generation is not CPU- or memory-limited. Each runtime probe creates an independent clone from the canonical database so backlog, WAL, GC, and cache state do not affect the next point. The clone is synced and evicted from the controller's page cache before Keeper starts, causing SQLite pages faulted during the probe to be charged to the Keeper cgroup.

## Directory Layout

```text
internal/benchmark/
├── README.md
├── README.zh.md
├── REPORT.md
├── REPORT.zh.md
├── legacy/                     # Existing Go microbenchmarks
├── capacity/                   # Dataset, load, cgroup runner, and summary logic
│   └── test/                   # Capacity-suite unit tests
├── cmd/benchctl/               # plan/generate/validate/run/resume/summarize
├── manifest/capacity-v1.json
├── schema/                     # JSON result contracts
└── scripts/run-capacity.sh
```

The runtime directory is selected with `BENCHMARK_ROOT`:

```text
<benchmark-root>/
├── benchmark.lock
├── bin/
├── config/
├── datasets/reference-3m/
│   ├── app.db                  # or app.db.zst
│   └── dataset.json
└── runs/
    ├── <controller-id>/
    │   ├── controller-inputs.sha256
    │   ├── controller.tsv
    │   ├── dataset-validation.json
    │   ├── environment.txt
    │   └── summary.md
    └── <controller-id>-<cell-run>/
        ├── run.json
        ├── results.jsonl
        ├── summary.json
        ├── summary.csv
        ├── report.md
        └── cells/<cell-id>/
```

## Requirements

- Linux amd64 with at least four online logical CPUs;
- cgroup v2 and systemd transient units;
- Go, GCC, the SQLite CLI, Redis server, `jq`, and `taskset`; `zstd` is also required when the canonical database is compressed;
- enough disk space for the canonical database, probe clones, WAL files, logs, and results;
- permission to use `systemd-run` and read the corresponding cgroup metrics.

## Running the Suite

Run directly from the repository on the benchmark host:

```bash
BENCHMARK_ROOT=/path/to/benchmark-data \
PREPARE_DATASET=1 \
internal/benchmark/scripts/run-capacity.sh
```

`PREPARE_DATASET=1` takes effect only when the canonical dataset is missing. After the first generation and validation, omit it to reuse the same dataset for the formal test campaign:

```bash
BENCHMARK_ROOT=/path/to/benchmark-data \
internal/benchmark/scripts/run-capacity.sh
```

The suite can also be invoked over SSH from a controller machine:

```bash
BENCHMARK_HOST=<ssh-target> \
BENCHMARK_REMOTE_REPOSITORY=<repository-path-on-target> \
BENCHMARK_ROOT=<benchmark-data-path-on-target> \
internal/benchmark/scripts/run-capacity.sh
```

Optional variables:

- `CPU_LIST=1,2,4`: select formal CPU profiles;
- `CONTROLLER_ID=<neutral-run-id>`: select a resumable controller run ID;
- `BENCHCTL_BINARY`, `KEEPER_BINARY`: reuse traceable prebuilt binaries;
- `REDIS_PORT`, `APP_PORT`: avoid conflicts with other tests on the same machine;
- `BENCHMARK_MANIFEST`: use another compatible manifest explicitly.

By default, the script builds Keeper and `benchctl` on the target, creates an immutable plan, and strictly validates the canonical dataset once. For each CPU profile, discovery starts at 25 events/s and ramps upward; if 25 events/s fails, the same search bisects the configured 20/15/10/5/1 events/s fallback range. It then bisects the resulting pass/fail interval and rechecks both sides of the ingestion boundary for 60 seconds. Only the resulting candidates proceed to five-minute fixed-rate validation. Five-minute ingestion refinement uses 25 events/s steps where possible and the configured manifest rates when an interval falls below that step. If no short Dashboard probe passes, the 1 event/s floor still receives one five-minute validation; after the first five-minute Dashboard pass, the controller bisects upward against the known five-minute failure until it finds the highest configured passing point. Every child run reuses the same validation proof instead of rescanning the full database. A controller ID is resumable only while its controller script, result contract, manifest, plan, binaries, dataset metadata, validation proof, CPU list, ports, search strategy, and durations remain unchanged.

Expected fixed-rate capacity failures remain terminal boundary evidence and are reused by `resume` when their provenance matches; they are not executed again after a controller restart. Missing results, stale validation, panic, runtime/sampler errors, and load-driver failures still stop the controller with a non-zero status. The controller writes a completed summary only after every requested CPU has exactly one formal row.

The controller fixes `TZ=Asia/Shanghai` for generation, validation, Keeper, and result collection. Dataset directories and cell IDs are resolved from the selected manifest and its expanded plan, so compatible custom manifests do not inherit the reference dataset name.

## Pass Criteria

An ingestion hard pass requires all of the following:

- at least 99.9% of offered events are published successfully;
- the final durable ratio is at least 99%;
- Redis inbox backlog does not grow;
- Overview, Activity, and Latency checkpoints and Identity aggregation catch up;
- post-load catch-up completes within 15 seconds; the remaining 30-second drain window is retained only for diagnostics;
- no OOM, panic, or load-driver publish error occurs.

A Core Dashboard pass first requires the ingestion hard pass, then requires zero Core HTTP errors and the aggregate p99 across Realtime Overview 60m, Overview 30d, Activity 30d, Analysis 30d, and Request Events 30d to remain within 3000ms. Core Dashboard replay runs at 1 request/s after warmup.

Analysis Latency 30d is warmed once and then requested every 30 seconds during the probe. Its successful sample count, error count, status, and p50/p95/p99/max are reported separately. A diagnostic error does not change ingestion or Core Dashboard capacity; OOM, panic, or another global Keeper failure still invalidates the run.

Each formal capacity point runs at a fixed rate for 300 seconds. Short ramp and boundary probes select candidates only and do not become final capacity results. The controller reports the highest five-minute passing rate together with the lowest observed failing rate, so the measured capacity is an interval rather than an unsupported claim of an exact absolute maximum. A zero failure boundary means no failure was observed within the configured rate matrix. `offered_events`, `published_events`, and `durable_events` are recorded separately so load-driver shortfalls cannot be reported as Keeper capacity.

## Result Files

Each run produces `run.json`, `results.jsonl`, `summary.json`, `summary.csv`, and `report.md`. The controller also produces `controller-inputs.sha256`, `dataset-validation.json`, `controller.tsv`, `environment.txt`, and a capacity `summary.md`.

See the current formal measurements in the [Capacity Benchmark Report](REPORT.md).
