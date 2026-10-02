# Pricing benchmark report

[简体中文](README.zh.md) · [Run guide](../../guides/pricing.md) · [All benchmarks](../../README.md)

## Final result

> **1m and 10m events: migration and historical recalculation passed.**
> At **50 incoming messages/s**, all messages were persisted: **zero drops and write errors**. Original request and token facts matched, with no unfilled event costs.

**At 10m: first pricing migration took about 60 minutes; recalculating 2.5m events took about 22 minutes.** These synthetic results apply to the recorded environment, not maximum ingestion capacity.

## Key measurements

| Measure | 1m events | 10m events |
| --- | ---: | ---: |
| **Migration / recalculation** | **Pass / pass** | **Pass / pass** |
| **First pricing migration** | **6.68 min** | **59.55 min** |
| Events recalculated over 30 days | 250,000 | 2,500,000 |
| **Recalculation time** | **2.49 min** | **21.68 min** |
| Incoming messages persisted and verified | 27,522 | 243,703 |
| Drops / write errors | **0 / 0** | **0 / 0** |
| Backlog processing after resumption | 4.07 s | 35.43 s |
| Whole-task cgroup memory peak | 2.79 GiB | **6.85 GiB** |
| Database + WAL/SHM + backup peak | 1.71 GiB | **12.93 GiB** |

**Measurement scope:** migration includes backup, event costs, rollup costs and validation, excluding dataset generation and full service startup. Recalculation covers the latest 30 days, not the entire database. Memory includes preparation and charged file cache; disk peaks are sampled.

## Query performance

For the 1m dataset and the same 119-day interval, each real HTTP endpoint received one warmup and five measured requests:

| Endpoint | Before, median | After, median | Observation |
| --- | ---: | ---: | --- |
| **Overview** | 1,301.07 ms | **71.02 ms** | **About 18.3 times faster at the median** |
| Analysis | 1,706.19 ms | 1,757.33 ms | About 3% higher; insufficient samples to establish a stable difference |

Both builds returned 991,361 requests and 2,672,835,362 tokens. This is neither a 10m nor concurrent-query test. The synthetic CPA returned 404; the interval excludes the injected-message day.

## Earlier-schema upgrade

A separate **1m-event historical-schema** test passed: upgrade **12.88 min**, recalculation of 333,333 events **3.14 min**. All 48,064 incoming messages were persisted and verified, with **zero** drops and write errors.

**Reception still queued: maximum delay was 26.00 s.** This scenario includes older schema upgrades and is not directly comparable with the pricing-only migration above.

## Method

1. Generate 120 days of history: 90 hot and 30 archived; 500 identities, 50 keys, 50 models, 50 price rules and 1% failed requests.
2. Run the first pricing migration while writing 50 messages/s to the real SQLite inbox through a 4,096-message test buffer.
3. Recalculate the latest 30 days, resume ordinary processing, and verify events, tokens, rollups, incoming counts and message hashes.

The earlier-schema scenario uses a separate 90-day all-hot dataset. Resource sampling every 50 ms can miss short peaks. A fixed input rate does not measure maximum capacity; inbox write delay is not CPA network end-to-end latency.

## Environment and reproduction

| Item | Configuration |
| --- | --- |
| System / CPU | Linux amd64 / 6 logical CPUs, no CPU quota |
| Host RAM / task limits | 9.49 GiB / 7 GiB memory, 512 MiB swap |
| Runtime | Go 1.26.2, go-sqlite3 v1.14.48, UTC |
| Dataset seed / anchor | `20260923` / `2026-09-23T00:00:00Z` |
| Code identity | Pricing baseline `4e41adc2`; historical upgrade uses the migration implementation in `94eef02c`; old query baseline `54a43cb3` |

**Reproduce:** [pricing run guide](../../guides/pricing.md). Exact binary and source identities are retained in the [environment](results/environment.json) and checksum inventories.

**Measurements:** [1m](results/latest-1000000.json) · [10m](results/latest-10000000.json) · [earlier-schema upgrade](results/five-dim-1000000.json) · [HTTP samples](results/query-comparison-1000000.json).

This report establishes no minimum-memory guarantee and covers neither higher input rates, long soaks nor production database upgrades.
