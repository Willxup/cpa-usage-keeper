<p align="center">
  <a href="./REPORT.md"><strong>English</strong></a> ｜ <a href="./REPORT.zh.md">简体中文</a>
</p>

# CPA Usage Keeper Capacity Benchmark Report

Test date: 2026-08-10 (Asia/Shanghai)

Suite: `capacity-v1`

Dataset: `reference-3m`

Platform: Linux amd64

## Executive Summary

Every formal point reused the same validated SQLite database and changed only the CPU available to Keeper: 1C, 2C, or 4C. The database contains 3,205,740 active events across 90 days; the validation anchor used by this campaign placed 1,201,775 events in the queried 30-day window. Keeper memory was unlimited, and the reported cgroup peak includes Keeper, SQLite pages, and database cache charged to that cgroup.

Capacity uses five Core Dashboard endpoints under a three-second aggregate p99 gate. Analysis Latency 30d is measured separately every 30 seconds because it is a heavier diagnostic query. Its latency does not decide Core Dashboard capacity, while errors, OOM, panic, and SQLite failures remain visible.

| Keeper CPU | Five-minute pass / lowest fail | 70% sustained recommendation | Core Dashboard p99 at pass | Analysis Latency p99 at pass | Peak memory at pass | Deployment guidance |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1C | 150 / 200 events/s | 105 events/s | 858.4ms | 5109.1ms | 472.9 MiB | 1C / 768 MiB, up to 105 sustained events/s |
| 2C | 200 / 250 events/s | 140 events/s | 627.1ms | 3075.9ms | 522.2 MiB | 2C / 1 GiB, up to 140 sustained events/s |
| 4C | 500 / 600 events/s | 350 events/s | 1323.0ms | 3044.6ms | 995.7 MiB | 4C / 2 GiB, up to 350 sustained events/s |

The strongest verified profile is **4C / 2 GiB at no more than 350 sustained events/s**. The measured 500 events/s point durably stored 149,998 of 150,000 offered events and caught up in 14.63 seconds. At 600 events/s, all 179,998 published events were durable, but 12,266 aggregation checkpoint rows remained after the full 30-second drain window.

The 1C and 2C failure boundaries were durable-throughput failures rather than Dashboard failures. All six formal points kept Core Dashboard p99 below 1.6 seconds, including the failing ingestion boundaries. Additional CPU therefore improves capacity, but SQLite ingestion and derived-state catch-up limit scaling before the assigned CPU quota is saturated.

These are five-minute sustained measurements, not instantaneous peaks or exact absolute maxima. The recommendation is 70% of the highest verified full-stack pass.

## Test Machine

| Item | Specification |
| --- | --- |
| Operating system | Debian GNU/Linux 13 |
| Kernel | 6.12.90+deb13.1-cloud-amd64 |
| Architecture | Linux amd64 |
| Virtualization | KVM, full virtualization |
| CPU | Intel Xeon Gold 6138 @ 2.00GHz |
| Topology | 1 socket, 4 cores, 1 thread per core |
| Online logical CPUs | 4 vCPU |
| Visible memory | 9,948,040 KiB (about 9.49 GiB) |
| Go | 1.26.2 |
| GCC | 14.2.0 |
| SQLite CLI | 3.46.1 |
| Redis | 8.0.2 |

Keeper received a cgroup quota equivalent to 1, 2, or 4 cores and was bound to CPU `0`, `0-1`, or `0-3`. Every profile used `memory.max=max` and `memory.swap.max=0`.

Dataset preparation was unrestricted. Database cloning, Redis publishing, and result collection ran outside the Keeper cgroup. The 1C and 2C drivers used CPUs outside Keeper's binding; the 4C profile shared all four vCPUs with the load driver and is therefore a conservative whole-machine measurement.

CPU utilization is normalized to Keeper's assigned quota. The passing profiles averaged 49.3% of 1C, 35.7% of 2C, and 27.2% of 4C, equivalent to approximately 0.49, 0.71, and 1.09 logical cores.

## Reference Dataset

| Item | Validated value |
| --- | ---: |
| Database size | 2,044,776,448 bytes (about 1.90 GiB) |
| Active 90-day events | 3,205,740 |
| Events in the formal 30-day query window | 1,201,775 |
| Archived events | 0 |
| Failed events | 31,831 (0.993%) |
| Identities | 500 used / 500 total |
| Models | 50 used / 50 total |
| API keys | 50 used / 50 total |
| Orphan identity/model/API-key references | 0 / 0 / 0 |
| `PRAGMA quick_check` | `ok` |
| Semantic fingerprint | `4b2b14e41bf7aaf91455fc1c3d9a2fe95ca45403d5448e2de17f08d969316f0b` |

The active table covers a complete 90-day history. The archive is intentionally empty because the production Dashboard paths in this suite do not query cold events. Every identity, model, and API key is referenced by events.

| API-key tier | API keys | Key share | Events | Event share |
| --- | ---: | ---: | ---: | ---: |
| High usage | 15 | 30% | 2,051,847 | 64.01% |
| Medium usage | 25 | 50% | 1,018,187 | 31.76% |
| Low usage | 10 | 20% | 135,706 | 4.23% |

Per-key generation weights are 10:3:1. The intentionally skewed distribution represents a minority of high-usage keys rather than concentrating traffic on one key.

Before every point, the suite created a new byte-for-byte clone of the canonical database, synced it, and evicted it from the controller page cache. Failed points could not carry backlog, WAL, or warmed SQLite pages into the next run.

## Method and Pass Criteria

- Each formal point started a fresh Keeper process against an independent database clone, warmed all Dashboard paths, and sustained the selected rate for 300 seconds.
- Events were published through Redis. Core Dashboard replay ran at 1 request/s across Realtime Overview 60m, Overview 30d, Activity 30d, Analysis 30d, and Request Events 30d.
- Analysis Latency 30d was warmed once and then requested every 30 seconds. Each formal point produced nine successful diagnostic samples.
- An ingestion hard pass required at least 99.9% successful publication, at least 99% final durable throughput, no growing backlog, caught-up Overview/Activity/Latency checkpoints and Identity aggregation, no OOM, panic, or publisher error, and catch-up within 15 seconds.
- A Core Dashboard pass first required the ingestion hard pass, then required zero Core HTTP errors and aggregate p99 at or below 3000ms.
- Analysis Latency errors and percentiles were evaluated separately. A diagnostic error did not relabel ingestion or Core Dashboard capacity.
- Short probes selected candidates only. Only the six five-minute boundary points below contribute to capacity conclusions.
- The sustained recommendation is 70% of the highest verified full-stack pass, rounded down to an integer events/s.

## Capacity Boundaries

| CPU | Highest pass | Lowest fail | Durable at pass | Catch-up at pass | CPU at pass | Peak memory at pass | Failure evidence |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1C | 150 | 200 | 45,000 / 45,000 | 5.37s | 49.3% | 472.9 MiB | 200 stored 53,969 / 60,000 |
| 2C | 200 | 250 | 60,000 / 60,000 | 3.24s | 35.7% | 522.2 MiB | 250 stored 71,597 / 75,000 |
| 4C | 500 | 600 | 149,998 / 150,000 | 14.63s | 27.2% | 995.7 MiB | 600 retained 12,266 checkpoint lag after 30.07s |

No formal point OOMed. Increasing memory alone would not resolve these observed boundaries: 1C/2C failures came from durable throughput, while the 4C failure came from derived-state catch-up.

## Core Dashboard Assessment

| CPU | Pass rate | Samples | Aggregate p50 | Aggregate p95 | Aggregate p99 | Slowest endpoint p99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1C | 150 events/s | 299 | 299.9ms | 639.4ms | 858.4ms | Overview 30d: 961.8ms |
| 2C | 200 events/s | 299 | 297.9ms | 532.1ms | 627.1ms | Analysis 30d: 649.7ms |
| 4C | 500 events/s | 299 | 339.6ms | 938.2ms | 1323.0ms | Overview 30d: 1469.6ms |

### Endpoint latency at each passing boundary

| Endpoint | 1C/150 p50 / p95 / p99 | 2C/200 p50 / p95 / p99 | 4C/500 p50 / p95 / p99 |
| --- | ---: | ---: | ---: |
| Realtime Overview 60m | 204.5 / 585.3 / 668.9ms | 202.6 / 389.0 / 438.5ms | 422.0 / 895.9 / 1160.1ms |
| Overview 30d | 353.7 / 710.3 / 961.8ms | 338.0 / 565.5 / 592.8ms | 604.8 / 1165.5 / 1469.6ms |
| Activity 30d | 262.9 / 523.4 / 582.2ms | 309.0 / 345.4 / 364.7ms | 309.9 / 348.6 / 393.3ms |
| Analysis 30d | 454.5 / 771.9 / 889.3ms | 497.5 / 623.0 / 649.7ms | 504.7 / 578.0 / 593.3ms |
| Request Events 30d | 148.2 / 333.2 / 444.5ms | 146.6 / 173.2 / 247.4ms | 152.8 / 182.7 / 190.1ms |

Core Dashboard was not the limiting gate in this campaign. Every passing and failing ingestion boundary stayed well below the three-second Core p99 threshold.

## Analysis Latency Diagnostics

| CPU / pass rate | Successful samples | Errors | Status | p50 | p95 | p99 | Max |
| --- | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 1C / 150 | 9 | 0 | Passed | 4453.6ms | 5109.1ms | 5109.1ms | 5109.1ms |
| 2C / 200 | 9 | 0 | Passed | 2706.1ms | 3075.9ms | 3075.9ms | 3075.9ms |
| 4C / 500 | 9 | 0 | Passed | 2802.1ms | 3044.6ms | 3044.6ms | 3044.6ms |

Analysis Latency is intentionally outside the Core Dashboard three-second gate. It merges retained latency sketches and sample points across the selected 30-day range, so it is materially heavier than the other Dashboard paths at this dataset size.

Only nine diagnostic requests complete in a five-minute point at a 30-second interval. Nearest-rank p95 and p99 therefore equal the maximum observation and should be read as bounded run evidence, not as a statistically precise production SLO.

## Identity Cardinality Direction (Exploratory)

A separate five-minute 1C comparison used 3,205,740 active events, 50 models, 50 API keys, unlimited memory, and 1 event/s while reducing identities from 500 to 50. The queried 30-day windows differed by about 1% because the runs used different time anchors. It predates the formal diagnostic cadence and is directional evidence, not a new capacity boundary.

| Metric | 500 identities | 50 identities | Observed change |
| --- | ---: | ---: | ---: |
| Core Dashboard aggregate p99 | 521.3ms | 277.4ms | -46.8% |
| Analysis 30d p99 | 557.6ms | 223.0ms | -60.0% |
| Analysis Latency 30d p99 | 3384.8ms | 3225.9ms | -4.7% |
| CPU utilization, normalized to 1C | 64.4% | 56.0% | -8.5 percentage points |
| Peak memory | 310.8 MiB | 251.3 MiB | -59.5 MiB |

Lower identity cardinality can materially reduce Core Dashboard work and memory for lighter installations. Analysis Latency changes much less because its retained statistics are keyed by API group rather than identity. This direction does not change the formal capacity or hardware recommendations above.

## Boundary Evidence

| CPU | Rate | Hard pass | Core Dashboard pass | Diagnostic status | Durable / offered | Catch-up | Peak memory | Core p99 | Reason |
| --- | ---: | --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| 1C | 150 | Yes | Yes | Passed | 45,000 / 45,000 | 5.37s | 472.9 MiB | 858.4ms | — |
| 1C | 200 | No | No | Passed | 53,969 / 60,000 | 3.37s | 471.9 MiB | 786.7ms | `durable_throughput` |
| 2C | 200 | Yes | Yes | Passed | 60,000 / 60,000 | 3.24s | 522.2 MiB | 627.1ms | — |
| 2C | 250 | No | No | Passed | 71,597 / 75,000 | 0.00s | 589.6 MiB | 708.8ms | `durable_throughput` |
| 4C | 500 | Yes | Yes | Passed | 149,998 / 150,000 | 14.63s | 995.7 MiB | 1323.0ms | — |
| 4C | 600 | No | No | Passed | 179,998 / 180,000 | 30.07s | 1044.3 MiB | 1415.4ms | `drain_lag`, `checkpoint_lag` |

## Capacity Planning Guidance

- **Light deployment:** 1C / 768 MiB, no more than 105 sustained events/s.
- **Medium deployment:** 2C / 1 GiB, no more than 140 sustained events/s.
- **Higher-throughput deployment:** 4C / 2 GiB, no more than 350 sustained events/s.
- Size memory from the observed cgroup peak plus deployment headroom. These are not finite hard-cap startup tests.
- Do not treat additional memory alone as a throughput upgrade; no capacity failure was caused by OOM.
- Do not linearly extrapolate beyond 4C or 500 events/s. SQLite writes, Redis-to-SQLite durability, derived-state catch-up, and shared-driver contention limit scaling first.

## Limitations

- Each reported boundary point has one five-minute formal run; the campaign did not repeat every boundary or perform a 24-hour soak.
- Analysis Latency has nine successful samples per formal point, so its high percentiles are observational.
- The 4C profile shares host CPUs with the load driver and is conservative but more host-dependent.
- No 256/512/768/1024 MiB memory hard cap was applied. Memory guidance is observed peak plus headroom, not a verified minimum.
- The generated database covers 90 active days. The Dashboard workload queries production 30-day and realtime paths; archive/cold-table performance is outside scope.
- Five-minute sustained events/s on a preloaded history cannot be multiplied by time to claim a contractual monthly capacity.
- A prior campaign that replayed Analysis Latency as frequently as Core Dashboard endpoints is retained as stress evidence but is superseded for production capacity recommendations.
- The identity-cardinality comparison is exploratory and does not revise the formal capacity boundaries.

## Reproducibility

- Canonical database SHA-256: `55805c8644d2a1dc9a2fc2fffb400e3ba74cbb7b777f1c3098516071add070af`
- Dataset semantic fingerprint: `4b2b14e41bf7aaf91455fc1c3d9a2fe95ca45403d5448e2de17f08d969316f0b`
- Dataset validation SHA-256: `d0a90239ef1590b30c36b5be51bb61a25b1fc00eda3f7e5cbd9a870ad3a9b557`
- Keeper binary SHA-256: `75ae2e29e0a4edd8ec7d29954cc9f037721c8c9173d2ff8e2f337f0ea5a68b0e`
- `benchctl` binary SHA-256: `9b2b8bb5bfcbd57d78ef4bdf95f714203718a5a84d495428ced3757896d89c6c`
- Manifest SHA-256: `e6513ff3cb50d352a2b1c325daec2d5cec57ea862af3837cdf75789e0419afa2`
- Expanded plan SHA-256: `0976acf6a94518f536ac84f1b6d7fb0502a1e2c8344966ec11c8241755117323`
