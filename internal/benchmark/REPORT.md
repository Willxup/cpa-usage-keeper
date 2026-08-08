<p align="center">
  <a href="./REPORT.md"><strong>English</strong></a> ｜ <a href="./REPORT.zh.md">简体中文</a>
</p>

# CPA Usage Keeper Capacity Benchmark Report

Test date: 2026-08-07 (Asia/Shanghai)

Suite: `capacity-v1`

Dataset: `reference-2m`

## Conclusions

This benchmark reused the same SQLite database containing about 2.036 million events. Only Keeper CPU was limited; Keeper memory remained unlimited. Every formal capacity result comes from a five-minute fixed-rate run with ingestion and Dashboard traffic active concurrently.

| Keeper CPU | Five-minute ingestion max | 3-second Dashboard max | Conservative sustained rate | CPU at ingestion max | Keeper cgroup peak memory | Recommended memory |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1C | 150 events/s | 100 events/s | Dashboard 70; ingestion-only 105 events/s | 57.3% | 304.4 MiB | 512 MiB |
| 2C | 200 events/s | 200 events/s | 140 events/s | 35.9% | 417.0 MiB | 1 GiB |
| 4C | 650 events/s | 650 events/s | 455 events/s | 29.5% | 1,078.2 MiB | 2 GiB |

The default production recommendation is **2C / 1 GiB with sustained traffic no higher than 140 events/s**. On the reference dataset, this profile reached both the 200 events/s ingestion and Dashboard limits while maintaining materially lower page latency than the 4C maximum-capacity point.

For higher traffic, use **4C / 2 GiB with sustained traffic no higher than 455 events/s**. At 4C/650, overall p99 was 1970.0ms and therefore met the three-second target, but core p95 reached 1387.6ms. This point should be treated as usable within three seconds, not as a low-latency configuration.

For lightweight deployments, use **1C / 512 MiB**. The Dashboard recommendation is about 70 events/s. If concurrent Dashboard use is not required, an ingestion-only deployment can conservatively use about 105 events/s.

Memory recommendations add deployment headroom to the observed peaks; they are not hard-cap validation results. Formal peak memory comes from cgroup v2 `memory.peak` and includes the Keeper process, SQLite mmap/page cache, and database cache charged to the Keeper cgroup.

## Test Machine

| Item | Configuration |
| --- | --- |
| Operating system | Debian GNU/Linux 13 |
| Platform | Linux amd64 |
| Virtualization | KVM |
| CPU | Intel Xeon Gold 6138 |
| Online logical CPUs | 4 vCPU |
| Available physical memory | About 9.5 GiB |
| Go | 1.26.2 |
| GCC | 14.2.0 |
| SQLite CLI | 3.46.1 |
| Redis | 8.0.2 |

The 1C, 2C, and 4C Keeper profiles used cgroup `cpu.max=100000/200000/400000 100000` and `AllowedCPUs=0/0-1/0-3`. All profiles used `memory.max=max` and `memory.swap.max=0`. Dataset generation, cloning, load generation, and result aggregation were outside the Keeper cgroup.

For 1C and 2C, the load driver used the remaining CPUs. At 4C, Keeper and the load driver shared all four vCPUs. Raw results mark this as `shared_driver=true`, so the 4C figures should be treated as conservative whole-machine results.

## Dataset

| Item | Measured value |
| --- | ---: |
| Dataset ID | `reference-2m` |
| Database size | 1,171,144,704 bytes (about 1.09 GiB) |
| Total events | 2,035,740 |
| Events in the latest 30 days | 1,226,326 |
| Hot events in the latest 90 days | 1,946,550 |
| Archived events | 89,190 |
| Identities | 323 |
| Models | 52 |
| API keys | 27 |
| `PRAGMA quick_check` | `ok` |
| Semantic fingerprint | `eb9dc034942bd6fd16477e037452cb64324158cdf8b48d2671647e409d4ca8d2` |

The dataset contains 1,226,326 events in the latest 30 days, 1,946,550 active events in the latest 90 days, and 89,190 archived events. This represents the storage, page-cache, rollup, and query load of a continuously running deployment.

The 27 API keys are deterministically assigned to high-, medium-, and low-usage tiers in a 30%/50%/20% split. Per-key weights are 10:3:1, and events are normalized across those weights. All 323 identities, 52 models, and 27 API keys are referenced by events. The dataset also passed orphan-reference, token-semantics, derived rollup/checkpoint, and semantic-fingerprint validation.

## Method and Pass Criteria

- Each formal point created an independent working copy from the same canonical database, restarted Keeper, warmed six Dashboard endpoints sequentially, and then ran at a fixed rate for 300 seconds.
- Offered load was published through Redis `usage`. Dashboard replay ran at 1 request/s and rotated through six real page endpoints.
- An ingestion hard pass required at least 99.9% of target events to publish successfully, a final durable ratio of at least 99%, no backlog growth, caught-up Overview/Activity/Latency checkpoints and Identity aggregation, and no OOM, panic, SQLite busy, HTTP, or publish errors.
- A Dashboard pass first required the hard pass and then required aggregate p99 across the six endpoints to remain within 3000ms.
- Core p95 was retained as an experience indicator but was not a formal pass gate.
- Capacity points were measured for five minutes. The 20-second search selected candidates only and did not become a final capacity limit.
- Adjacent boundaries converged to a 25 events/s interval or an interval of approximately 10% or less.

Every result table uses the ingestion hard pass and Dashboard overall p99 <=3000ms criteria above.

## All Five-Minute Formal Points

Each of the following 24 points independently restored the canonical database, restarted Keeper, and ran for 300 seconds. `Hard` covers ingestion, durability, checkpoint, and aggregation integrity. `Dashboard` additionally requires the three-second SLA.

### 1C / Unlimited Memory

| Rate | Hard | Dashboard | Durable / Offered | Durable | CPU | Peak memory | Core p95 | Overall p99 | Failure reason |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 25 | Pass | Pass | 7,500 / 7,500 | 100% | 47.4% | 174.0 MiB | 274.8ms | 2,586.7ms | — |
| 50 | Pass | Pass | 15,000 / 15,000 | 100% | 49.4% | 193.1 MiB | 316.3ms | 2,602.0ms | — |
| 75 | Pass | Pass | 22,500 / 22,500 | 100% | 50.0% | 248.0 MiB | 376.3ms | 2,717.1ms | — |
| 100 | Pass | Pass | 30,000 / 30,000 | 100% | 53.0% | 290.7 MiB | 465.7ms | 2,892.5ms | — |
| 150 | Pass | Fail | 45,000 / 45,000 | 100% | 57.3% | 304.4 MiB | 593.0ms | 3,917.1ms | overall p99 >3s |
| 200 | Fail | Fail | 53,972 / 60,000 | 89.95% | 58.2% | 328.7 MiB | 613.6ms | 3,549.3ms | durable, overall p99 |
| 250 | Fail | Fail | 59,894 / 75,000 | 79.86% | 59.8% | 417.2 MiB | 718.1ms | 3,751.1ms | durable, overall p99 |
| 500 | Fail | Fail | 74,441 / 150,000 | 49.63% | 63.1% | 453.0 MiB | 891.0ms | 5,061.7ms | durable, overall p99 |
| 1,000 | Fail | Fail | 117,279 / 300,000 | 39.09% | 79.8% | 756.5 MiB | 2,187.7ms | 6,200.1ms | durable, overall p99 |
| 3,000 | Fail | Fail | 264,819 / 900,000 | 29.42% | 86.2% | 1,705.7 MiB | 4,834.3ms | 8,793.5ms | errors, durable, backlog, checkpoint, Identity, overall p99 |

At 1C, 25, 50, 75, and 100 events/s all met the three-second target. The highest Dashboard pass was 100 events/s. At 150 events/s, all events were durable, but overall p99 increased to 3917.1ms.

### 2C / Unlimited Memory

| Rate | Hard | Dashboard | Durable / Offered | Durable | CPU | Peak memory | Core p95 | Overall p99 | Failure reason |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 200 | Pass | Pass | 60,000 / 60,000 | 100% | 35.9% | 417.0 MiB | 449.3ms | 2,085.8ms | — |
| 225 | Fail | Fail | 60,741 / 67,500 | 89.99% | 35.6% | 378.2 MiB | 447.8ms | 2,065.0ms | durable |
| 250 | Fail | Fail | 72,522 / 75,000 | 96.70% | 37.9% | 479.1 MiB | 548.3ms | 2,102.9ms | durable |
| 300 | Fail | Fail | 80,969 / 90,000 | 89.97% | 39.2% | 454.2 MiB | 589.2ms | 2,196.9ms | durable |

The 2C boundary was clear: 200 events/s passed all criteria, while 225 events/s already failed durable throughput. The failed points did not OOM, so additional memory alone would not move this boundary.

### 4C / Unlimited Memory

| Rate | Hard | Dashboard | Durable / Offered | Durable | CPU | Peak memory | Core p95 | Overall p99 | Failure reason |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 200 | Pass | Pass | 60,000 / 60,000 | 100% | 18.9% | 326.4 MiB | 434.6ms | 1,947.4ms | — |
| 250 | Pass | Pass | 74,999 / 75,000 | 99.999% | 20.2% | 383.5 MiB | 498.8ms | 1,947.8ms | — |
| 275 | Pass | Pass | 82,500 / 82,500 | 100% | 20.7% | 435.2 MiB | 556.7ms | 1,973.6ms | — |
| 300 | Pass | Pass | 90,000 / 90,000 | 100% | 21.4% | 499.8 MiB | 588.4ms | 1,953.7ms | — |
| 400 | Pass | Pass | 120,000 / 120,000 | 100% | 24.1% | 654.3 MiB | 744.7ms | 1,945.0ms | — |
| 500 | Pass | Pass | 150,000 / 150,000 | 100% | 25.2% | 661.5 MiB | 978.3ms | 1,974.7ms | — |
| 600 | Pass | Pass | 180,000 / 180,000 | 100% | 28.4% | 985.9 MiB | 1,130.8ms | 1,964.7ms | — |
| 650 | Pass | Pass | 195,000 / 195,000 | 100% | 29.5% | 1,078.2 MiB | 1,387.6ms | 1,970.0ms | — |
| 675 | Fail | Fail | 182,212 / 202,500 | 89.98% | 30.1% | 979.5 MiB | 1,229.5ms | 1,991.5ms | durable |
| 750 | Fail | Fail | 225,000 / 225,000 | 100% | 30.0% | 1,321.2 MiB | 1,527.8ms | 1,987.1ms | checkpoint lag=39,762 |

The 4C/750 result shows why counting durable `usage_events` alone overstates capacity: all 225,000 events were durable, but the rollup checkpoint still lagged by 39,762 events, so the hard pass failed.

## Ingestion Boundary

| CPU | Highest pass | First adjacent failure | Durable at pass | Failure reason | CPU at pass | Keeper cgroup peak memory |
| --- | ---: | ---: | ---: | --- | ---: | ---: |
| 1C | 150 | 200 | 45,000 / 45,000 | 200 persisted only 53,972 / 60,000 | 57.3% | 304.4 MiB |
| 2C | 200 | 225 | 60,000 / 60,000 | 225 persisted only 60,741 / 67,500 | 35.9% | 417.0 MiB |
| 4C | 650 | 675 | 195,000 / 195,000 | 675 persisted only 182,212 / 202,500 | 29.5% | 1,078.2 MiB |

CPU utilization is normalized by the CPUs assigned to Keeper. Therefore, 29.5% at 4C/650 corresponds to approximately 1.18 logical CPUs of total CPU time. Every formal point used unlimited memory. The failed points did not OOM; the primary limits were durable/rollup throughput and Dashboard latency under higher traffic.

## Dashboard Assessment

| CPU | Evaluated rate | Hard | Keeper cgroup peak memory | Core p95 | Overall p99 | Slowest endpoint p99 | Conclusion |
| --- | ---: | --- | ---: | ---: | ---: | ---: | --- |
| 1C | 100 events/s | Pass | 290.7 MiB | 465.7ms | 2,892.5ms | Analysis Latency 2,946.8ms | Pass; p99 at 150 was 3,917.1ms |
| 2C | 200 events/s | Pass | 417.0 MiB | 449.3ms | 2,085.8ms | Analysis Latency 2,114.9ms | Pass; ingestion already failed at 225 |
| 4C | 650 events/s | Pass | 1,078.2 MiB | 1,387.6ms | 1,970.0ms | Analysis Latency 2,008.3ms | Passes within three seconds, but is not a low-latency point |

Steady-state per-endpoint latency at 4C/650:

| Endpoint | p50 | p95 | p99 |
| --- | ---: | ---: | ---: |
| Analysis | 1.6ms | 1.8ms | 1.8ms |
| Request Events | 147.1ms | 171.5ms | 193.8ms |
| Activity | 171.0ms | 209.0ms | 245.9ms |
| Realtime Overview | 589.5ms | 1,447.2ms | 1,729.2ms |
| Overview 30d | 717.0ms | 1,498.4ms | 1,550.7ms |
| Analysis Latency 30d | 1,902.8ms | 1,970.0ms | 2,008.3ms |

If the deployment goal is faster pages rather than merely staying within three seconds, retain more headroom below the capacity limit and continue monitoring core p95.

## Scale-up Gains

- 1C → 2C: ingestion max increased from 150 to 200 (+33%), and the three-second Dashboard max increased from 100 to 200. Peak memory increased from 304.4 MiB to 417.0 MiB.
- 2C → 4C: ingestion and Dashboard max increased from 200 to 650 (3.25x). Peak memory increased to about 1.05 GiB.
- Increasing available memory alone does not automatically increase capacity. The formal points already used unlimited memory; failures came from durable throughput, checkpoint lag, or the Dashboard SLA.
- For the reference dataset, 2C is the default balance between throughput, latency, and memory. 4C provides higher throughput but materially increases core page p95.

## Limitations

- Each final boundary has one five-minute formal run. The suite did not repeat each point or run a 24-hour soak. These results support capacity planning but do not prove a long-term SLO.
- At 4C, Keeper shared the host CPUs with the load driver, making the result conservative.
- No 256/512/768/1024 MiB hard cap was applied. The report can provide observed peak memory and recommendations with headroom, but it cannot claim a verified minimum startup memory.
- events/s represents five-minute sustained traffic on a database preloaded with about 2.036 million historical events. It cannot be multiplied directly by 30 days as a safe monthly capacity because query and aggregation costs will change as the database grows.
- Conclusions apply only to the recorded hardware specification, dataset fingerprint, binary, and method.

## Reproducibility

- Keeper binary SHA-256: `4782fc7bfacc1c72667436e4d4471cd26755ed2ae192173beff77bcae4bd27d6`
- Dataset semantic fingerprint: `eb9dc034942bd6fd16477e037452cb64324158cdf8b48d2671647e409d4ca8d2`
