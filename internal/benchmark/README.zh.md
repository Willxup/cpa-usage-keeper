<p align="center">
  <a href="./README.md">English</a> ｜ <a href="./README.zh.md"><strong>简体中文</strong></a>
</p>

# 容量 Benchmark

`internal/benchmark` 同时包含代码级 Go microbenchmark，以及使用完整 Keeper、SQLite、Redis 和真实 Dashboard HTTP 查询的生产型容量测试。正式容量套件为 `capacity-v1`，只测试 `linux/amd64`。

## 测试目标

容量测试只改变 Keeper 可用 CPU：1C、2C、4C。三档均使用同一份 canonical 数据库，Keeper 内存不设上限，测试结束后读取 cgroup v2 `memory.peak`。该数值包含 Keeper 进程、SQLite mmap/page cache 及归属于 Keeper cgroup 的数据库缓存。

正式结论同时给出：

- 五分钟持续 ingestion 上限；
- ingestion hard pass 前提下，六个 Dashboard 接口整体 p99 不超过 3 秒的上限；
- 容量点 CPU 利用率和 Keeper cgroup 峰值内存；
- 按 Dashboard 上限 70% 计算的保守持续流量建议。

core p95 继续记录，用于判断页面体验，但不参与 `capacity-v1` 的通过判定。

## 参考数据集

参考数据集 ID 为 `reference-2m`。

| 项目 | 数量 |
| --- | ---: |
| 全库 events | 2,035,740 |
| 最近 30 天 events | 1,226,326 |
| 最近 90 天 hot events | 1,946,550 |
| archive events | 89,190 |
| identities | 323 |
| models | 52 |
| API keys | 27 |
| 数据库大小 | 1,171,144,704 bytes（约 1.09 GiB） |

数据集保留最近 90 天的活跃事件及更早的归档历史，用于覆盖长期运行后的存储、查询和聚合负载。

API keys 按 30%/50%/20% 确定性分入高、中、低用量档，每 Key 权重为 10:3:1；`usage_events` 数量根据这些权重归一化分配。323 identities、52 models 和 27 API keys 都被事件实际引用。

canonical 必须通过行数、基数、孤儿引用、token 语义、派生 rollup/checkpoint、`PRAGMA quick_check` 和 semantic fingerprint 验证。生成阶段不限制 CPU 或内存；运行阶段每个 probe 都从 canonical 创建独立 clone，避免 backlog、WAL、GC 和缓存污染下一个点。

## 目录

```text
internal/benchmark/
├── README.md
├── README.zh.md
├── REPORT.md
├── REPORT.zh.md
├── legacy/                     # 原有 Go microbenchmark
├── capacity/                   # 数据生成、负载、cgroup runner、汇总
│   └── test/                   # 容量套件单元测试
├── cmd/benchctl/               # plan/generate/validate/run/resume/summarize
├── manifest/capacity-v1.json
├── schema/                     # JSON 结果协议
└── scripts/run-capacity.sh
```

运行目录由 `BENCHMARK_ROOT` 指定：

```text
<benchmark-root>/
├── benchmark.lock
├── bin/
├── config/
├── datasets/reference-2m/
│   ├── app.db                  # 或 app.db.zst
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

## 环境要求

- Linux amd64，至少 4 个在线逻辑 CPU；
- cgroup v2 和 systemd transient units；
- Go、GCC、SQLite CLI、Redis server、`jq`、`taskset`；
- 足够存放 canonical、probe clone、WAL、日志与结果的磁盘空间；
- 运行用户有权使用 `systemd-run` 和读取对应 cgroup 指标。

## 运行

直接在 benchmark 主机的仓库中运行：

```bash
BENCHMARK_ROOT=/path/to/benchmark-data \
PREPARE_DATASET=1 \
internal/benchmark/scripts/run-capacity.sh
```

`PREPARE_DATASET=1` 只在 canonical 不存在时生效。首次生成并验证完成后，后续运行应省略该变量以复用相同数据：

```bash
BENCHMARK_ROOT=/path/to/benchmark-data \
internal/benchmark/scripts/run-capacity.sh
```

也可以从控制机通过 SSH 调用，不需要把目标写入仓库：

```bash
BENCHMARK_HOST=<ssh-target> \
BENCHMARK_REMOTE_REPOSITORY=<repository-path-on-target> \
BENCHMARK_ROOT=<benchmark-data-path-on-target> \
internal/benchmark/scripts/run-capacity.sh
```

可选变量：

- `CPU_LIST=1,2,4`：选择正式 CPU 档；
- `CONTROLLER_ID=<neutral-run-id>`：指定可恢复的运行组 ID；
- `BENCHCTL_BINARY`、`KEEPER_BINARY`：复用已构建且可追溯的二进制；
- `REDIS_PORT`、`APP_PORT`：避免与同机其它测试冲突；
- `BENCHMARK_MANIFEST`：显式使用另一份兼容 manifest。

默认会在目标机上构建 Keeper 和 `benchctl`、生成不可变 plan、验证 canonical，然后对每个 CPU 档先做 20 秒离散搜索，只对必要候选执行五分钟固定速率验证。已存在的结果按 run ID 复用，不会重复生成 canonical 或覆盖完成的 cell。

## 判定规则

Ingestion hard pass 必须同时满足：

- 至少 99.9% 目标事件成功发布；
- 最终 durable ratio 至少 99%；
- Redis inbox backlog 不增长；
- Overview、Activity、Latency checkpoint 和 Identity 聚合追平；
- 无 OOM、panic、SQLite busy、HTTP 错误或负载器 publish error。

Dashboard pass 先要求 hard pass，再要求六接口整体 p99 不超过 3000ms。Dashboard replay 为 1 req/s，先预热，再在 Realtime Overview、Overview 30d、Activity、Analysis、Request Events、Analysis Latency 30d 之间轮转。

每个正式容量点持续 300 秒。短探测只选择候选，不进入最终容量结论。结果中的 `offered_events`、`published_events` 和 `durable_events` 分开记录，不能把负载器未达到目标速率误报为 Keeper 容量。

## 结果文件

每个运行点生成 `run.json`、`results.jsonl`、`summary.json`、`summary.csv` 和 `report.md`；控制器额外生成汇总表 `controller.tsv`、环境规格 `environment.txt` 和容量摘要 `summary.md`。

当前正式测量结果见 [REPORT.zh.md](REPORT.zh.md)。
