# CPA Usage Keeper 容量 Benchmark 报告

[English](README.md) · [运行指南](../../guides/capacity.zh.md) · [全部 Benchmark](../../README.zh.md)

测试日期：2026-08-10（Asia/Shanghai）

套件：`capacity-v1`

数据集：`reference-3m`

平台：Linux amd64

这是最近一次完成的容量测量，执行于费用持久化重构之前，尚未针对当前实现重测。下文的二进制及数据集哈希标识实际测量对象，不能将结果视为当前版本的容量结论。


## 结论摘要

> **最高实测五分钟通过档位：4 核、每秒 500 条。**
> **建议规划流量：每秒 350 条**，取通过档位的 70%；不是另行验证过的限内存部署结果。
> **适用范围：费用持久化重构前的测量，不代表当前版本容量。**

每个正式点均复用同一份已验证 SQLite 数据库，只改变 Keeper 可用 CPU：1C、2C 或 4C。数据库包含最近 90 天的 3,205,740 条活跃 events；本轮数据校验锚点对应的 30 天查询窗口包含 1,201,775 条 events。Keeper 内存不设上限，报告中的 cgroup 峰值包含 Keeper、SQLite 页面以及归入该 cgroup 的数据库缓存。

容量门槛只覆盖五个核心 Dashboard 接口，整体 p99 上限为 3 秒。Analysis Latency 30d 属于更重的诊断查询，每 30 秒独立测量一次；其延迟不决定核心 Dashboard 容量，但错误、OOM、panic 与 SQLite 故障仍会明确记录。

| Keeper CPU | 五分钟通过 / 最低失败 | 70% 持续流量建议 | 通过点核心 Dashboard p99 | 通过点 Analysis Latency p99 | 通过点峰值内存 | 部署建议 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1C | **150 / 200 events/s** | **105 events/s** | 858.4ms | 5109.1ms | 472.9 MiB | 1C / 768 MiB，持续流量不超过 105 events/s |
| 2C | **200 / 250 events/s** | **140 events/s** | 627.1ms | 3075.9ms | 522.2 MiB | 2C / 1 GiB，持续流量不超过 140 events/s |
| 4C | **500 / 600 events/s** | **350 events/s** | 1323.0ms | 3044.6ms | 995.7 MiB | 4C / 2 GiB，持续流量不超过 350 events/s |

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

## 测试方法与通过条件

- 每个正式点都使用独立数据库 clone 启动全新 Keeper，预热全部 Dashboard 路径后，以选定速率持续运行 300 秒。
- Events 通过 Redis 发布。核心 Dashboard replay 为 1 req/s，轮询 Realtime Overview 60m、Overview 30d、Activity 30d、Analysis 30d 和 Request Events 30d。
- Analysis Latency 30d 先预热一次，随后每 30 秒请求一次；每个正式点得到9个成功诊断样本。
- Ingestion hard pass 要求：发布成功率至少99.9%、最终 durable ratio 至少99%、backlog不增长、Overview/Activity/Latency checkpoint与Identity聚合追平、无OOM、panic或发布器错误，并在15秒内完成追平。
- 核心 Dashboard pass 先要求 ingestion hard pass，再要求核心HTTP错误为0且整体p99不超过3000ms。
- Analysis Latency 的错误和分位数独立判定；诊断错误不会把 ingestion 或核心 Dashboard 容量重新标记为失败。
- 短测只用于选择候选。容量结论仅使用下方六个五分钟正式边界点。
- 持续流量建议取最高完整通过点的70%，向下取整到整数 events/s。

## 边界证据

| CPU | 速率 | Hard pass | 核心 Dashboard pass | 诊断状态 | Durable / offered | 追平 | 峰值内存 | 核心 p99 | 原因 |
| --- | ---: | --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| 1C | 150 | 是 | 是 | 通过 | 45,000 / 45,000 | 5.37s | 472.9 MiB | 858.4ms | — |
| 1C | 200 | 否 | 否 | 通过 | 53,969 / 60,000 | 3.37s | 471.9 MiB | 786.7ms | `durable_throughput` |
| 2C | 200 | 是 | 是 | 通过 | 60,000 / 60,000 | 3.24s | 522.2 MiB | 627.1ms | — |
| 2C | 250 | 否 | 否 | 通过 | 71,597 / 75,000 | 0.00s | 589.6 MiB | 708.8ms | `durable_throughput` |
| 4C | 500 | 是 | 是 | 通过 | 149,998 / 150,000 | 14.63s | 995.7 MiB | 1323.0ms | — |
| 4C | 600 | 否 | 否 | 通过 | 179,998 / 180,000 | 30.07s | 1044.3 MiB | 1415.4ms | `drain_lag`、`checkpoint_lag` |

## 限制

- 每个报告边界点只有一次五分钟正式运行；本轮没有重复全部边界或执行24小时soak。
- 每个正式点只有9个Analysis Latency成功样本，其高分位数属于观测值。
- 4C与负载器共享宿主CPU，因此结果偏保守，也更依赖当前主机。
- 本轮没有施加256/512/768/1024 MiB内存hard cap；内存建议是observed peak加余量，不是已验证最低限制。
- 生成数据库覆盖90天活跃数据；Dashboard负载查询生产使用的30天和realtime路径，archive/冷表性能不在范围内。
- 预装历史数据库上的五分钟 sustained events/s 不能直接乘以时间，作为契约型月容量。
- 早期把Analysis Latency按核心接口频率回放的测试仍保留为压力证据，但不再用于生产容量建议。

## 可复现信息

- Canonical database SHA-256：`55805c8644d2a1dc9a2fc2fffb400e3ba74cbb7b777f1c3098516071add070af`
- Dataset semantic fingerprint：`4b2b14e41bf7aaf91455fc1c3d9a2fe95ca45403d5448e2de17f08d969316f0b`
- Dataset validation SHA-256：`d0a90239ef1590b30c36b5be51bb61a25b1fc00eda3f7e5cbd9a870ad3a9b557`
- Keeper binary SHA-256：`75ae2e29e0a4edd8ec7d29954cc9f037721c8c9173d2ff8e2f337f0ea5a68b0e`
- `benchctl` binary SHA-256：`9b2b8bb5bfcbd57d78ef4bdf95f714203718a5a84d495428ced3757896d89c6c`
- Manifest SHA-256：`e6513ff3cb50d352a2b1c325daec2d5cec57ea862af3837cdf75789e0419afa2`
- Expanded plan SHA-256：`0976acf6a94518f536ac84f1b6d7fb0502a1e2c8344966ec11c8241755117323`


[容量运行指南](../../guides/capacity.zh.md) · [全部 Benchmark](../../README.zh.md)
