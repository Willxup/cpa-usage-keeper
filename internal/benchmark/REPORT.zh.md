<p align="center">
  <a href="./REPORT.md">English</a> ｜ <a href="./REPORT.zh.md"><strong>简体中文</strong></a>
</p>

# CPA Usage Keeper 容量 Benchmark 报告

测试日期：2026-08-07（Asia/Shanghai）

套件：`capacity-v1`

数据集：`reference-2m`

## 结论

本轮固定复用同一份约 203.6 万 events 的 SQLite 数据库，只限制 Keeper CPU，不限制 Keeper 内存。所有正式容量均来自 ingestion 与 Dashboard 并发运行的五分钟固定速率测试。

| Keeper CPU | 五分钟 ingestion 上限 | 3 秒 Dashboard 上限 | 保守持续流量建议 | ingestion 上限 CPU | Keeper cgroup 峰值内存 | 建议内存 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1C | 150 events/s | 100 events/s | Dashboard 70；ingestion-only 105 events/s | 57.3% | 304.4 MiB | 512 MiB |
| 2C | 200 events/s | 200 events/s | 140 events/s | 35.9% | 417.0 MiB | 1 GiB |
| 4C | 650 events/s | 650 events/s | 455 events/s | 29.5% | 1,078.2 MiB | 2 GiB |

默认生产建议为 **2C / 1 GiB、持续流量不超过 140 events/s**。它在当前参考数据集上同时达到 200 events/s ingestion 与 Dashboard 上限，页面延迟明显优于 4C 的极限容量点。

高流量部署可选 **4C / 2 GiB、持续流量不超过 455 events/s**。4C/650 的 overall p99 为 1970.0ms，满足 3 秒口径，但 core p95 已达到 1387.6ms，因此应理解为“3 秒内可用”，不是低延迟配置。

轻量部署可选 **1C / 512 MiB**。Dashboard 建议约 70 events/s；若不要求同时使用 Dashboard，ingestion-only 可保守使用约 105 events/s。

内存建议来自实测峰值加部署余量，不是 hard-cap 验证结果。正式峰值采用 Keeper cgroup v2 的 `memory.peak`，包含 Keeper 进程、SQLite mmap/page cache 以及归属于该 cgroup 的数据库缓存，没有从结果中扣除数据库占用。

## 测试机器

| 项目 | 配置 |
| --- | --- |
| 操作系统 | Debian GNU/Linux 13 |
| 平台 | Linux amd64 |
| 虚拟化 | KVM |
| CPU | Intel Xeon Gold 6138 |
| 在线逻辑 CPU | 4 vCPU |
| 可用物理内存 | 约 9.5 GiB |
| Go | 1.26.2 |
| GCC | 14.2.0 |
| SQLite CLI | 3.46.1 |
| Redis | 8.0.2 |

Keeper 的 1C/2C/4C 分别使用 cgroup `cpu.max=100000/200000/400000 100000`，并绑定 `AllowedCPUs=0/0-1/0-3`。三档均为 `memory.max=max`、`memory.swap.max=0`。合成、clone、负载器和结果汇总不计入 Keeper cgroup。

1C 和 2C 时负载器使用剩余 CPU；4C 时 Keeper 与负载器共享四个 vCPU，原始结果标记 `shared_driver=true`，因此 4C 数字可视为偏保守的整机结果。

## 数据集

| 项目 | 实测值 |
| --- | ---: |
| 数据集 ID | `reference-2m` |
| 数据库大小 | 1,171,144,704 bytes（约 1.09 GiB） |
| 全库 events | 2,035,740 |
| 最近 30 天 events | 1,226,326 |
| 最近 90 天 hot events | 1,946,550 |
| archive events | 89,190 |
| identities | 323 |
| models | 52 |
| API keys | 27 |
| `PRAGMA quick_check` | `ok` |
| semantic fingerprint | `eb9dc034942bd6fd16477e037452cb64324158cdf8b48d2671647e409d4ca8d2` |

数据集包含最近 30 天的 1,226,326 条事件、最近 90 天的 1,946,550 条活跃事件，以及 89,190 条归档事件，用于模拟持续运行后的存储、page cache、rollup 和查询负载。

27 个 API keys 按 30%/50%/20% 确定性分入高、中、低用量档，每 Key 权重为 10:3:1，events 根据权重归一化分配。323 identities、52 models 和 27 API keys 均被事件实际引用。数据集同时通过孤儿引用、token 语义、派生 rollup/checkpoint 与 semantic fingerprint 检查。

## 测试方法与通过条件

- 每个正式点从同一份 canonical 创建独立工作副本，重启 Keeper，顺序预热六个 Dashboard 接口，再固定运行 300 秒。
- offered load 通过 Redis `usage` 发布；Dashboard replay 为 1 req/s，在六个真实页面接口之间轮转。
- ingestion hard pass：至少 99.9% 目标事件成功发布、最终 durable ratio 至少 99%、backlog 不增长、Overview/Activity/Latency checkpoint 和 Identity 聚合追平、无 OOM、panic、SQLite busy、HTTP 或 publish error。
- Dashboard pass：先满足 hard pass，再要求六接口整体 p99 不超过 3000ms。
- core p95 作为体验指标保留，但不参与正式通过判定。
- 容量点均为五分钟实测；20 秒搜索只用于选择候选，不作为最终上限。
- 相邻边界收敛到 25 events/s，或不超过约 10% 的间隔。

所有结果表格统一按上述 ingestion hard pass 和 Dashboard overall p99 <=3000ms 标准判定。

## 全部五分钟正式点

以下 24 个点均独立恢复 canonical、重启 Keeper 并运行 300 秒。`Hard` 表示 ingestion、durable、checkpoint 等完整性；`Dashboard` 表示在 Hard 基础上满足 3 秒 SLA。

### 1C / 不限制内存

| Rate | Hard | Dashboard | Durable / Offered | Durable | CPU | Peak memory | Core p95 | Overall p99 | 失败原因 |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 25 | 通过 | 通过 | 7,500 / 7,500 | 100% | 47.4% | 174.0 MiB | 274.8ms | 2,586.7ms | — |
| 50 | 通过 | 通过 | 15,000 / 15,000 | 100% | 49.4% | 193.1 MiB | 316.3ms | 2,602.0ms | — |
| 75 | 通过 | 通过 | 22,500 / 22,500 | 100% | 50.0% | 248.0 MiB | 376.3ms | 2,717.1ms | — |
| 100 | 通过 | 通过 | 30,000 / 30,000 | 100% | 53.0% | 290.7 MiB | 465.7ms | 2,892.5ms | — |
| 150 | 通过 | 失败 | 45,000 / 45,000 | 100% | 57.3% | 304.4 MiB | 593.0ms | 3,917.1ms | overall p99 >3s |
| 200 | 失败 | 失败 | 53,972 / 60,000 | 89.95% | 58.2% | 328.7 MiB | 613.6ms | 3,549.3ms | durable、overall p99 |
| 250 | 失败 | 失败 | 59,894 / 75,000 | 79.86% | 59.8% | 417.2 MiB | 718.1ms | 3,751.1ms | durable、overall p99 |
| 500 | 失败 | 失败 | 74,441 / 150,000 | 49.63% | 63.1% | 453.0 MiB | 891.0ms | 5,061.7ms | durable、overall p99 |
| 1,000 | 失败 | 失败 | 117,279 / 300,000 | 39.09% | 79.8% | 756.5 MiB | 2,187.7ms | 6,200.1ms | durable、overall p99 |
| 3,000 | 失败 | 失败 | 264,819 / 900,000 | 29.42% | 86.2% | 1,705.7 MiB | 4,834.3ms | 8,793.5ms | errors、durable、backlog、checkpoint、Identity、overall p99 |

1C 的 25、50、75、100 events/s 均满足 3 秒口径；100 是最高 Dashboard 通过点。150 虽完整落盘，但 overall p99 已升至 3917.1ms。

### 2C / 不限制内存

| Rate | Hard | Dashboard | Durable / Offered | Durable | CPU | Peak memory | Core p95 | Overall p99 | 失败原因 |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 200 | 通过 | 通过 | 60,000 / 60,000 | 100% | 35.9% | 417.0 MiB | 449.3ms | 2,085.8ms | — |
| 225 | 失败 | 失败 | 60,741 / 67,500 | 89.99% | 35.6% | 378.2 MiB | 447.8ms | 2,065.0ms | durable |
| 250 | 失败 | 失败 | 72,522 / 75,000 | 96.70% | 37.9% | 479.1 MiB | 548.3ms | 2,102.9ms | durable |
| 300 | 失败 | 失败 | 80,969 / 90,000 | 89.97% | 39.2% | 454.2 MiB | 589.2ms | 2,196.9ms | durable |

2C 的边界清晰：200 全部通过，225 已因 durable throughput 失败。失败点没有 OOM，单独增加内存不能改变这条边界。

### 4C / 不限制内存

| Rate | Hard | Dashboard | Durable / Offered | Durable | CPU | Peak memory | Core p95 | Overall p99 | 失败原因 |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 200 | 通过 | 通过 | 60,000 / 60,000 | 100% | 18.9% | 326.4 MiB | 434.6ms | 1,947.4ms | — |
| 250 | 通过 | 通过 | 74,999 / 75,000 | 99.999% | 20.2% | 383.5 MiB | 498.8ms | 1,947.8ms | — |
| 275 | 通过 | 通过 | 82,500 / 82,500 | 100% | 20.7% | 435.2 MiB | 556.7ms | 1,973.6ms | — |
| 300 | 通过 | 通过 | 90,000 / 90,000 | 100% | 21.4% | 499.8 MiB | 588.4ms | 1,953.7ms | — |
| 400 | 通过 | 通过 | 120,000 / 120,000 | 100% | 24.1% | 654.3 MiB | 744.7ms | 1,945.0ms | — |
| 500 | 通过 | 通过 | 150,000 / 150,000 | 100% | 25.2% | 661.5 MiB | 978.3ms | 1,974.7ms | — |
| 600 | 通过 | 通过 | 180,000 / 180,000 | 100% | 28.4% | 985.9 MiB | 1,130.8ms | 1,964.7ms | — |
| 650 | 通过 | 通过 | 195,000 / 195,000 | 100% | 29.5% | 1,078.2 MiB | 1,387.6ms | 1,970.0ms | — |
| 675 | 失败 | 失败 | 182,212 / 202,500 | 89.98% | 30.1% | 979.5 MiB | 1,229.5ms | 1,991.5ms | durable |
| 750 | 失败 | 失败 | 225,000 / 225,000 | 100% | 30.0% | 1,321.2 MiB | 1,527.8ms | 1,987.1ms | checkpoint lag=39,762 |

4C/750 证明只看 `usage_events` 落盘数会高估容量：225,000 条全部 durable，但 rollup checkpoint 仍落后 39,762 条，因此 Hard 必须失败。

## Ingestion 边界

| CPU | 最高通过 | 首个相邻失败 | 通过点 durable | 失败原因 | 通过点 CPU | Keeper cgroup 峰值内存 |
| --- | ---: | ---: | ---: | --- | ---: | ---: |
| 1C | 150 | 200 | 45,000 / 45,000 | 200 仅落盘 53,972 / 60,000 | 57.3% | 304.4 MiB |
| 2C | 200 | 225 | 60,000 / 60,000 | 225 仅落盘 60,741 / 67,500 | 35.9% | 417.0 MiB |
| 4C | 650 | 675 | 195,000 / 195,000 | 675 仅落盘 182,212 / 202,500 | 29.5% | 1,078.2 MiB |

CPU 利用率按分配给 Keeper 的 CPU 数量归一化，因此 4C/650 的 29.5% 约等于 1.18 个逻辑核心的总 CPU 时间。所有正式点均为 unlimited memory；失败点没有 OOM，主要边界来自 durable/rollup 路径以及高流量下的 Dashboard 延迟。

## Dashboard 评估

| CPU | 评估速率 | Hard | Keeper cgroup 峰值内存 | Core p95 | Overall p99 | 最慢接口 p99 | 结论 |
| --- | ---: | --- | ---: | ---: | ---: | ---: | --- |
| 1C | 100 events/s | 通过 | 290.7 MiB | 465.7ms | 2,892.5ms | Analysis Latency 2,946.8ms | 通过；150 的 p99=3,917.1ms |
| 2C | 200 events/s | 通过 | 417.0 MiB | 449.3ms | 2,085.8ms | Analysis Latency 2,114.9ms | 通过；225 的 ingestion 已失败 |
| 4C | 650 events/s | 通过 | 1,078.2 MiB | 1,387.6ms | 1,970.0ms | Analysis Latency 2,008.3ms | 3 秒内通过，但不是低延迟点 |

4C/650 的逐接口稳态延迟：

| Endpoint | p50 | p95 | p99 |
| --- | ---: | ---: | ---: |
| Analysis | 1.6ms | 1.8ms | 1.8ms |
| Request Events | 147.1ms | 171.5ms | 193.8ms |
| Activity | 171.0ms | 209.0ms | 245.9ms |
| Realtime Overview | 589.5ms | 1,447.2ms | 1,729.2ms |
| Overview 30d | 717.0ms | 1,498.4ms | 1,550.7ms |
| Analysis Latency 30d | 1,902.8ms | 1,970.0ms | 2,008.3ms |

若部署目标是“页面尽量快”而不仅是“3 秒内可用”，应在容量上限之外保留更大流量余量，并持续观察 core p95。

## 扩容收益

- 1C → 2C：ingestion 上限从 150 提升到 200（+33%），3 秒 Dashboard 上限从 100 提升到 200；峰值内存从 304.4 MiB 增至 417.0 MiB。
- 2C → 4C：ingestion 与 Dashboard 上限从 200 提升到 650（3.25x）；峰值内存增至约 1.05 GiB。
- 单独提高可用内存不会自动提高容量：正式点本来就是 unlimited memory，失败原因是 durable throughput、checkpoint lag 或 Dashboard SLA。
- 当前参考数据集下，2C 是吞吐、延迟和内存之间的默认平衡点；4C 提供更高吞吐，但核心页面 p95 明显上升。

## 限制

- 每个最终边界只有一次五分钟正式运行，没有多次重复或 24 小时 soak；结果适合容量规划，不等同于长期 SLO 证明。
- 4C Keeper 与负载器共享宿主 CPU，结果偏保守。
- 本轮没有施加 256/512/768/1024 MiB hard cap，只能给出 observed peak 和带余量建议，不能声称验证了最低可启动内存。
- events/s 是在预装约 203.6 万条全库历史上的五分钟持续流量，不能直接乘以 30 天当作长期安全月容量；数据库继续增长后，查询与聚合成本也会变化。
- 结论只适用于本报告记录的硬件规格、数据集指纹、二进制和测试方法。

## 可复现信息

- Keeper binary SHA-256：`4782fc7bfacc1c72667436e4d4471cd26755ed2ae192173beff77bcae4bd27d6`
- Dataset semantic fingerprint：`eb9dc034942bd6fd16477e037452cb64324158cdf8b48d2671647e409d4ca8d2`
