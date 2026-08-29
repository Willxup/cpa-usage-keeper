<p align="center">
  <a href="./REPORT.md">English</a> ｜ <a href="./REPORT.zh.md"><strong>简体中文</strong></a>
</p>

# CPA Usage Keeper 容量 Benchmark 报告

测试日期：2026-08-10（Asia/Shanghai）

套件：`capacity-v1`

数据集：`reference-3m`

平台：Linux amd64

## 结论摘要

每个正式点均复用同一份已验证 SQLite 数据库，只改变 Keeper 可用 CPU：1C、2C 或 4C。数据库包含最近 90 天的 3,205,740 条活跃 events；本轮数据校验锚点对应的 30 天查询窗口包含 1,201,775 条 events。Keeper 内存不设上限，报告中的 cgroup 峰值包含 Keeper、SQLite 页面以及归入该 cgroup 的数据库缓存。

容量门槛只覆盖五个核心 Dashboard 接口，整体 p99 上限为 3 秒。Analysis Latency 30d 属于更重的诊断查询，每 30 秒独立测量一次；其延迟不决定核心 Dashboard 容量，但错误、OOM、panic 与 SQLite 故障仍会明确记录。

| Keeper CPU | 五分钟通过 / 最低失败 | 70% 持续流量建议 | 通过点核心 Dashboard p99 | 通过点 Analysis Latency p99 | 通过点峰值内存 | 部署建议 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1C | 150 / 200 events/s | 105 events/s | 858.4ms | 5109.1ms | 472.9 MiB | 1C / 768 MiB，持续流量不超过 105 events/s |
| 2C | 200 / 250 events/s | 140 events/s | 627.1ms | 3075.9ms | 522.2 MiB | 2C / 1 GiB，持续流量不超过 140 events/s |
| 4C | 500 / 600 events/s | 350 events/s | 1323.0ms | 3044.6ms | 995.7 MiB | 4C / 2 GiB，持续流量不超过 350 events/s |

本轮最强的已验证档位为 **4C / 2 GiB，持续流量不超过 350 events/s**。实测 500 events/s 在 150,000 条目标 events 中发布并持久化 149,998 条，14.63 秒内完成追平。600 events/s 虽然 179,998 条已发布 events 全部持久化，但完整 30 秒 drain 后仍有 12,266 条聚合 checkpoint lag。

1C 与 2C 的失败边界均来自 durable throughput，而不是 Dashboard。六个正式点的核心 Dashboard p99 全部低于 1.6 秒，包括 ingestion 失败点。因此增加 CPU 能提升容量，但 SQLite ingestion 与派生聚合追平会在分配的 CPU quota 饱和前限制扩展。

这些数值是五分钟持续测量，不是瞬时峰值或绝对精确上限。持续流量建议取最高完整通过点的 70%。

## 测试机器

| 项目 | 配置 |
| --- | --- |
| 操作系统 | Debian GNU/Linux 13 |
| Kernel | 6.12.90+deb13.1-cloud-amd64 |
| 架构 | Linux amd64 |
| 虚拟化 | KVM，全虚拟化 |
| CPU | Intel Xeon Gold 6138 @ 2.00GHz |
| 拓扑 | 1 socket、4 cores、每 core 1 thread |
| 在线逻辑 CPU | 4 vCPU |
| 虚拟机可见内存 | 9,948,040 KiB（约 9.49 GiB） |
| Go | 1.26.2 |
| GCC | 14.2.0 |
| SQLite CLI | 3.46.1 |
| Redis | 8.0.2 |

Keeper 分别使用等价于 1、2、4 cores 的 cgroup CPU quota，并绑定到 CPU `0`、`0-1`、`0-3`。三档均为 `memory.max=max`、`memory.swap.max=0`。

数据准备阶段不限制资源。数据库 clone、Redis 发布器与结果采集位于 Keeper cgroup 外；1C、2C 的负载器使用 Keeper 绑定范围之外的 CPU，4C 与负载器共享全部四个 vCPU，因此4C属于偏保守的整机测量。

CPU 利用率按 Keeper 分配的 quota 归一化。通过点平均分别为1C的49.3%、2C的35.7%、4C的27.2%，约等于实际使用0.49、0.71、1.09个逻辑核心。

## 参考数据集

| 项目 | 验证值 |
| --- | ---: |
| 数据库大小 | 2,044,776,448 bytes（约 1.90 GiB） |
| 90 天活跃 events | 3,205,740 |
| 正式运行 30 天查询窗口 events | 1,201,775 |
| Archive events | 0 |
| Failed events | 31,831（0.993%） |
| Identities | 使用 500 / 总计 500 |
| Models | 使用 50 / 总计 50 |
| API keys | 使用 50 / 总计 50 |
| 孤儿 identity/model/API-key 引用 | 0 / 0 / 0 |
| `PRAGMA quick_check` | `ok` |
| Semantic fingerprint | `4b2b14e41bf7aaf91455fc1c3d9a2fe95ca45403d5448e2de17f08d969316f0b` |

活跃表覆盖完整 90 天历史。archive 刻意保持为空，因为本套件覆盖的生产 Dashboard 路径不会查询冷数据。所有 identities、models 和 API keys 都被 events 实际引用。

| API-key 档位 | API keys | Key 数量占比 | Events | Events 占比 |
| --- | ---: | ---: | ---: | ---: |
| 大用量 | 15 | 30% | 2,051,847 | 64.01% |
| 中用量 | 25 | 50% | 1,018,187 | 31.76% |
| 小用量 | 10 | 20% | 135,706 | 4.23% |

每 Key 生成权重为 10:3:1。最终流量刻意保持不均衡，用于模拟少量高用量 Key，而不是把 events 集中到单一 Key。

每个测试点开始前，套件都会从 canonical 创建新的字节级 clone，完成落盘并从 controller page cache 中清除。失败点不会把 backlog、WAL 或已预热 SQLite 页面带入下一轮。

## 测试方法与通过条件

- 每个正式点都使用独立数据库 clone 启动全新 Keeper，预热全部 Dashboard 路径后，以选定速率持续运行 300 秒。
- Events 通过 Redis 发布。核心 Dashboard replay 为 1 req/s，轮询 Realtime Overview 60m、Overview 30d、Activity 30d、Analysis 30d 和 Request Events 30d。
- Analysis Latency 30d 先预热一次，随后每 30 秒请求一次；每个正式点得到9个成功诊断样本。
- Ingestion hard pass 要求：发布成功率至少99.9%、最终 durable ratio 至少99%、backlog不增长、Overview/Activity/Latency checkpoint与Identity聚合追平、无OOM、panic或发布器错误，并在15秒内完成追平。
- 核心 Dashboard pass 先要求 ingestion hard pass，再要求核心HTTP错误为0且整体p99不超过3000ms。
- Analysis Latency 的错误和分位数独立判定；诊断错误不会把 ingestion 或核心 Dashboard 容量重新标记为失败。
- 短测只用于选择候选。容量结论仅使用下方六个五分钟正式边界点。
- 持续流量建议取最高完整通过点的70%，向下取整到整数 events/s。

## 容量边界

| CPU | 最高通过 | 最低失败 | 通过点 durable | 通过点追平 | 通过点 CPU | 通过点峰值内存 | 失败证据 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1C | 150 | 200 | 45,000 / 45,000 | 5.37s | 49.3% | 472.9 MiB | 200 仅落库 53,969 / 60,000 |
| 2C | 200 | 250 | 60,000 / 60,000 | 3.24s | 35.7% | 522.2 MiB | 250 仅落库 71,597 / 75,000 |
| 4C | 500 | 600 | 149,998 / 150,000 | 14.63s | 27.2% | 995.7 MiB | 600 在 30.07s 后仍有 12,266 checkpoint lag |

所有正式点均未 OOM。单独增加内存不能解决这些实测边界：1C/2C失败来自durable throughput，4C失败来自派生状态追平。

## 核心 Dashboard 评估

| CPU | 通过速率 | 样本数 | 整体 p50 | 整体 p95 | 整体 p99 | 最慢接口 p99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1C | 150 events/s | 299 | 299.9ms | 639.4ms | 858.4ms | Overview 30d：961.8ms |
| 2C | 200 events/s | 299 | 297.9ms | 532.1ms | 627.1ms | Analysis 30d：649.7ms |
| 4C | 500 events/s | 299 | 339.6ms | 938.2ms | 1323.0ms | Overview 30d：1469.6ms |

### 各通过边界的接口延迟

| 接口 | 1C/150 p50 / p95 / p99 | 2C/200 p50 / p95 / p99 | 4C/500 p50 / p95 / p99 |
| --- | ---: | ---: | ---: |
| Realtime Overview 60m | 204.5 / 585.3 / 668.9ms | 202.6 / 389.0 / 438.5ms | 422.0 / 895.9 / 1160.1ms |
| Overview 30d | 353.7 / 710.3 / 961.8ms | 338.0 / 565.5 / 592.8ms | 604.8 / 1165.5 / 1469.6ms |
| Activity 30d | 262.9 / 523.4 / 582.2ms | 309.0 / 345.4 / 364.7ms | 309.9 / 348.6 / 393.3ms |
| Analysis 30d | 454.5 / 771.9 / 889.3ms | 497.5 / 623.0 / 649.7ms | 504.7 / 578.0 / 593.3ms |
| Request Events 30d | 148.2 / 333.2 / 444.5ms | 146.6 / 173.2 / 247.4ms | 152.8 / 182.7 / 190.1ms |

本轮核心 Dashboard 不是限制门槛。所有通过和失败 ingestion 边界的核心 p99 都明显低于3秒。

## Analysis Latency 诊断

| CPU / 通过速率 | 成功样本 | 错误 | 状态 | p50 | p95 | p99 | Max |
| --- | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 1C / 150 | 9 | 0 | 通过 | 4453.6ms | 5109.1ms | 5109.1ms | 5109.1ms |
| 2C / 200 | 9 | 0 | 通过 | 2706.1ms | 3075.9ms | 3075.9ms | 3075.9ms |
| 4C / 500 | 9 | 0 | 通过 | 2802.1ms | 3044.6ms | 3044.6ms | 3044.6ms |

Analysis Latency 刻意不参与核心 Dashboard 的3秒门槛。该接口会在所选30天范围内合并已保留的Latency sketches与抽样点，因此在当前数据规模下明显重于其它 Dashboard 路径。

每个五分钟点按30秒间隔只能完成9个诊断请求。nearest-rank p95与p99因此等于最大观测值，应作为本轮有界证据理解，而不是统计上精确的生产SLO。

## Identity 基数方向（探索性）

另一次五分钟1C对照使用3,205,740条活跃events、50个models、50个API keys、内存不设上限及1 event/s，并将identities从500降至50。两次运行因时间锚点不同，30天查询窗口的数据量相差约1%。该测试早于当前正式诊断查询频率，因此只作为方向性证据，不构成新的容量边界。

| 指标 | 500 identities | 50 identities | 观测变化 |
| --- | ---: | ---: | ---: |
| 核心 Dashboard 整体 p99 | 521.3ms | 277.4ms | -46.8% |
| Analysis 30d p99 | 557.6ms | 223.0ms | -60.0% |
| Analysis Latency 30d p99 | 3384.8ms | 3225.9ms | -4.7% |
| CPU 利用率（按1C归一化） | 64.4% | 56.0% | -8.5个百分点 |
| 峰值内存 | 310.8 MiB | 251.3 MiB | -59.5 MiB |

较低的identity基数可以明显减少轻量部署的核心Dashboard开销与内存占用。Analysis Latency改善较小，因为其保留统计按API group而非identity分桶。这个方向不改变上方正式容量边界与硬件建议。

## 边界证据

| CPU | 速率 | Hard pass | 核心 Dashboard pass | 诊断状态 | Durable / offered | 追平 | 峰值内存 | 核心 p99 | 原因 |
| --- | ---: | --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| 1C | 150 | 是 | 是 | 通过 | 45,000 / 45,000 | 5.37s | 472.9 MiB | 858.4ms | — |
| 1C | 200 | 否 | 否 | 通过 | 53,969 / 60,000 | 3.37s | 471.9 MiB | 786.7ms | `durable_throughput` |
| 2C | 200 | 是 | 是 | 通过 | 60,000 / 60,000 | 3.24s | 522.2 MiB | 627.1ms | — |
| 2C | 250 | 否 | 否 | 通过 | 71,597 / 75,000 | 0.00s | 589.6 MiB | 708.8ms | `durable_throughput` |
| 4C | 500 | 是 | 是 | 通过 | 149,998 / 150,000 | 14.63s | 995.7 MiB | 1323.0ms | — |
| 4C | 600 | 否 | 否 | 通过 | 179,998 / 180,000 | 30.07s | 1044.3 MiB | 1415.4ms | `drain_lag`、`checkpoint_lag` |

## 容量规划建议

- **轻量部署：**1C / 768 MiB，持续流量不超过 105 events/s。
- **中等部署：**2C / 1 GiB，持续流量不超过 140 events/s。
- **较高吞吐部署：**4C / 2 GiB，持续流量不超过 350 events/s。
- 内存按实测cgroup峰值加部署余量配置；这些不是有限hard-cap启动测试。
- 不要把单独增加内存视为吞吐升级；本轮没有容量失败来自OOM。
- 不要从4C或500 events/s继续线性外推；SQLite写入、Redis到SQLite的持久化、派生状态追平和共享负载器会先限制扩展。

## 限制

- 每个报告边界点只有一次五分钟正式运行；本轮没有重复全部边界或执行24小时soak。
- 每个正式点只有9个Analysis Latency成功样本，其高分位数属于观测值。
- 4C与负载器共享宿主CPU，因此结果偏保守，也更依赖当前主机。
- 本轮没有施加256/512/768/1024 MiB内存hard cap；内存建议是observed peak加余量，不是已验证最低限制。
- 生成数据库覆盖90天活跃数据；Dashboard负载查询生产使用的30天和realtime路径，archive/冷表性能不在范围内。
- 预装历史数据库上的五分钟 sustained events/s 不能直接乘以时间，作为契约型月容量。
- 早期把Analysis Latency按核心接口频率回放的测试仍保留为压力证据，但不再用于生产容量建议。
- Identity基数对照属于探索性测试，不修改正式容量边界。

## 可复现信息

- Canonical database SHA-256：`55805c8644d2a1dc9a2fc2fffb400e3ba74cbb7b777f1c3098516071add070af`
- Dataset semantic fingerprint：`4b2b14e41bf7aaf91455fc1c3d9a2fe95ca45403d5448e2de17f08d969316f0b`
- Dataset validation SHA-256：`d0a90239ef1590b30c36b5be51bb61a25b1fc00eda3f7e5cbd9a870ad3a9b557`
- Keeper binary SHA-256：`75ae2e29e0a4edd8ec7d29954cc9f037721c8c9173d2ff8e2f337f0ea5a68b0e`
- `benchctl` binary SHA-256：`9b2b8bb5bfcbd57d78ef4bdf95f714203718a5a84d495428ced3757896d89c6c`
- Manifest SHA-256：`e6513ff3cb50d352a2b1c325daec2d5cec57ea862af3837cdf75789e0419afa2`
- Expanded plan SHA-256：`0976acf6a94518f536ac84f1b6d7fb0502a1e2c8344966ec11c8241755117323`
