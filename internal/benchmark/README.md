<p align="center">
  <a href="./README.md"><strong>English</strong></a> ｜ <a href="./README.zh.md">简体中文</a>
</p>

# Capacity Benchmark

`internal/benchmark` contains both code-level Go microbenchmarks and a production-style capacity suite that exercises the complete Keeper binary, SQLite, Redis, and real Dashboard HTTP queries. The formal suite is `capacity-v1` and targets `linux/amd64` only.

## Test Objectives

The capacity suite changes only the CPU available to Keeper: 1C, 2C, and 4C. All profiles reuse the same canonical database. Keeper memory is unlimited during a run, and the suite reads cgroup v2 `memory.peak` afterward. This includes the Keeper process, SQLite mmap/page cache, and database cache charged to the Keeper cgroup.

The formal results report:

- the maximum sustained ingestion rate over five minutes;
- the maximum rate that also keeps the aggregate p99 of six Dashboard endpoints within three seconds after the ingestion hard pass succeeds;
- CPU utilization and Keeper cgroup peak memory at the capacity point;
- a conservative sustained-rate recommendation set to 70% of Dashboard capacity.

Core p95 is retained as an experience indicator but is not a `capacity-v1` pass gate.

## Reference Dataset

The reference dataset ID is `reference-2m`.

| Item | Count |
| --- | ---: |
| Total events | 2,035,740 |
| Events in the latest 30 days | 1,226,326 |
| Hot events in the latest 90 days | 1,946,550 |
| Archived events | 89,190 |
| Identities | 323 |
| Models | 52 |
| API keys | 27 |
| Database size | 1,171,144,704 bytes (about 1.09 GiB) |

The dataset retains 90 days of active events plus older archived history to represent the storage, query, and aggregation load of a long-running deployment.

API keys are deterministically assigned to high-, medium-, and low-usage tiers in a 30%/50%/20% split. Per-key weights are 10:3:1, and `usage_events` are normalized across those weights. All 323 identities, 52 models, and 27 API keys are referenced by events.

The canonical database must pass row-count, cardinality, orphan-reference, token-semantics, derived rollup/checkpoint, `PRAGMA quick_check`, and semantic-fingerprint validation. Dataset generation is not CPU- or memory-limited. Each runtime probe creates an independent clone from the canonical database so backlog, WAL, GC, and cache state do not affect the next point.

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
├── datasets/reference-2m/
│   ├── app.db                  # or app.db.zst
│   └── dataset.json
└── runs/
    ├── <controller-id>/
    │   ├── controller.tsv
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
- Go, GCC, the SQLite CLI, Redis server, `jq`, and `taskset`;
- enough disk space for the canonical database, probe clones, WAL files, logs, and results;
- permission to use `systemd-run` and read the corresponding cgroup metrics.

## Running the Suite

Run directly from the repository on the benchmark host:

```bash
BENCHMARK_ROOT=/path/to/benchmark-data \
PREPARE_DATASET=1 \
internal/benchmark/scripts/run-capacity.sh
```

`PREPARE_DATASET=1` takes effect only when the canonical dataset is missing. After the first generation and validation, omit it to reuse the same dataset:

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

By default, the script builds Keeper and `benchctl` on the target, creates an immutable plan, validates the canonical dataset, performs a 20-second discrete search for each CPU profile, and runs five-minute fixed-rate validation only for the required candidates. Existing results are reused by run ID; the canonical dataset is not regenerated and completed cells are not overwritten.

## Pass Criteria

An ingestion hard pass requires all of the following:

- at least 99.9% of offered events are published successfully;
- the final durable ratio is at least 99%;
- Redis inbox backlog does not grow;
- Overview, Activity, and Latency checkpoints and Identity aggregation catch up;
- no OOM, panic, SQLite busy error, HTTP error, or load-driver publish error occurs.

A Dashboard pass first requires the ingestion hard pass, then requires the aggregate p99 across six endpoints to remain within 3000ms. Dashboard replay runs at 1 request/s, warms the endpoints first, then rotates through Realtime Overview, Overview 30d, Activity, Analysis, Request Events, and Analysis Latency 30d.

Each formal capacity point runs for 300 seconds. Short probes select candidates only and do not become final capacity results. `offered_events`, `published_events`, and `durable_events` are recorded separately so load-driver shortfalls cannot be reported as Keeper capacity.

## Result Files

Each run produces `run.json`, `results.jsonl`, `summary.json`, `summary.csv`, and `report.md`. The controller also produces `controller.tsv`, `environment.txt`, and a capacity `summary.md`.

See the current formal measurements in the [Capacity Benchmark Report](REPORT.md).
