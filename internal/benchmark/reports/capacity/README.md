# CPA Usage Keeper Capacity Benchmark Report

[简体中文](README.zh.md) · [Run guide](../../guides/capacity.md) · [All benchmarks](../../README.md)

Test date: 2026-08-10 (Asia/Shanghai)

Suite: `capacity-v1`

Dataset: `reference-3m`

Platform: Linux amd64

## Executive Summary

> **Highest measured five-minute pass: 500 events/s with 4 CPUs.**
> **Planning rate: 350 events/s**, calculated as 70% of that pass; this is not a separately tested memory-capped deployment.
> **Scope: this measurement predates persisted pricing and is not current-version capacity.**

These are the latest completed capacity measurements, taken before the persisted-pricing refactor. They have not been rerun for the current implementation. Binary and dataset hashes below identify the measured build; do not interpret them as current-version capacity.


Every formal point reused the same validated SQLite database and changed only the CPU available to Keeper: 1C, 2C, or 4C. The database contains 3,205,740 active events across 90 days; the validation anchor used by this campaign placed 1,201,775 events in the queried 30-day window. Keeper memory was unlimited, and the reported cgroup peak includes Keeper, SQLite pages, and database cache charged to that cgroup.

Capacity uses five Core Dashboard endpoints under a three-second aggregate p99 gate. Analysis Latency 30d is measured separately every 30 seconds because it is a heavier diagnostic query. Its latency does not decide Core Dashboard capacity, while errors, OOM, panic, and SQLite failures remain visible.

| Keeper CPU | Five-minute pass / lowest fail | 70% sustained recommendation | Core Dashboard p99 at pass | Analysis Latency p99 at pass | Peak memory at pass | Deployment guidance |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1C | **150 / 200 events/s** | **105 events/s** | 858.4ms | 5109.1ms | 472.9 MiB | 1C / 768 MiB, up to 105 sustained events/s |
| 2C | **200 / 250 events/s** | **140 events/s** | 627.1ms | 3075.9ms | 522.2 MiB | 2C / 1 GiB, up to 140 sustained events/s |
| 4C | **500 / 600 events/s** | **350 events/s** | 1323.0ms | 3044.6ms | 995.7 MiB | 4C / 2 GiB, up to 350 sustained events/s |

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

## Method and Pass Criteria

- Each formal point started a fresh Keeper process against an independent database clone, warmed all Dashboard paths, and sustained the selected rate for 300 seconds.
- Events were published through Redis. Core Dashboard replay ran at 1 request/s across Realtime Overview 60m, Overview 30d, Activity 30d, Analysis 30d, and Request Events 30d.
- Analysis Latency 30d was warmed once and then requested every 30 seconds. Each formal point produced nine successful diagnostic samples.
- An ingestion hard pass required at least 99.9% successful publication, at least 99% final durable throughput, no growing backlog, caught-up Overview/Activity/Latency checkpoints and Identity aggregation, no OOM, panic, or publisher error, and catch-up within 15 seconds.
- A Core Dashboard pass first required the ingestion hard pass, then required zero Core HTTP errors and aggregate p99 at or below 3000ms.
- Analysis Latency errors and percentiles were evaluated separately. A diagnostic error did not relabel ingestion or Core Dashboard capacity.
- Short probes selected candidates only. Only the six five-minute boundary points below contribute to capacity conclusions.
- The sustained recommendation is 70% of the highest verified full-stack pass, rounded down to an integer events/s.

## Boundary Evidence

| CPU | Rate | Hard pass | Core Dashboard pass | Diagnostic status | Durable / offered | Catch-up | Peak memory | Core p99 | Reason |
| --- | ---: | --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| 1C | 150 | Yes | Yes | Passed | 45,000 / 45,000 | 5.37s | 472.9 MiB | 858.4ms | — |
| 1C | 200 | No | No | Passed | 53,969 / 60,000 | 3.37s | 471.9 MiB | 786.7ms | `durable_throughput` |
| 2C | 200 | Yes | Yes | Passed | 60,000 / 60,000 | 3.24s | 522.2 MiB | 627.1ms | — |
| 2C | 250 | No | No | Passed | 71,597 / 75,000 | 0.00s | 589.6 MiB | 708.8ms | `durable_throughput` |
| 4C | 500 | Yes | Yes | Passed | 149,998 / 150,000 | 14.63s | 995.7 MiB | 1323.0ms | — |
| 4C | 600 | No | No | Passed | 179,998 / 180,000 | 30.07s | 1044.3 MiB | 1415.4ms | `drain_lag`, `checkpoint_lag` |

## Limitations

- Each reported boundary point has one five-minute formal run; the campaign did not repeat every boundary or perform a 24-hour soak.
- Analysis Latency has nine successful samples per formal point, so its high percentiles are observational.
- The 4C profile shares host CPUs with the load driver and is conservative but more host-dependent.
- No 256/512/768/1024 MiB memory hard cap was applied. Memory guidance is observed peak plus headroom, not a verified minimum.
- The generated database covers 90 active days. The Dashboard workload queries production 30-day and realtime paths; archive/cold-table performance is outside scope.
- Five-minute sustained events/s on a preloaded history cannot be multiplied by time to claim a contractual monthly capacity.
- A prior campaign that replayed Analysis Latency as frequently as Core Dashboard endpoints is retained as stress evidence but is superseded for production capacity recommendations.

## Reproducibility

- Canonical database SHA-256: `55805c8644d2a1dc9a2fc2fffb400e3ba74cbb7b777f1c3098516071add070af`
- Dataset semantic fingerprint: `4b2b14e41bf7aaf91455fc1c3d9a2fe95ca45403d5448e2de17f08d969316f0b`
- Dataset validation SHA-256: `d0a90239ef1590b30c36b5be51bb61a25b1fc00eda3f7e5cbd9a870ad3a9b557`
- Keeper binary SHA-256: `75ae2e29e0a4edd8ec7d29954cc9f037721c8c9173d2ff8e2f337f0ea5a68b0e`
- `benchctl` binary SHA-256: `9b2b8bb5bfcbd57d78ef4bdf95f714203718a5a84d495428ced3757896d89c6c`
- Manifest SHA-256: `e6513ff3cb50d352a2b1c325daec2d5cec57ea862af3837cdf75789e0419afa2`
- Expanded plan SHA-256: `0976acf6a94518f536ac84f1b6d7fb0502a1e2c8344966ec11c8241755117323`


[Run the capacity suite](../../guides/capacity.md) · [All benchmarks](../../README.md)
