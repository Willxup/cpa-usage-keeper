<p align="center">
  <a href="./README.md">English</a> ｜ <a href="./README.zh.md"><strong>简体中文</strong></a>
</p>

# 容量 Benchmark

`internal/benchmark` 同时包含代码级 Go microbenchmark，以及使用完整 Keeper、SQLite、Redis 和真实 Dashboard HTTP 查询的生产型容量测试。正式容量套件为 `capacity-v1`，只测试 `linux/amd64`。

## 测试目标

容量测试只改变 Keeper 可用 CPU：1C、2C、4C。三档均使用同一份 canonical 数据库，Keeper 内存不设上限，测试结束后读取 cgroup v2 `memory.peak`。该数值包含 Keeper 进程、SQLite mmap/page cache 及归属于 Keeper cgroup 的数据库缓存。

正式结论同时给出：

- ingestion 的最高五分钟成功点及其上方最低已观察失败点；
- ingestion hard pass 前提下，五个核心 Dashboard 接口整体 p99 不超过 3 秒的五分钟成功/失败区间；
- 容量点 CPU 利用率和 Keeper cgroup 峰值内存；
- 单独列出的 Analysis Latency 样本数、错误数、状态与延迟分位数；
- 按 Dashboard 上限 70% 计算的保守持续流量建议。

core p95 继续记录，用于判断页面体验，但不参与 `capacity-v1` 的通过判定。Analysis Latency 属于更重的诊断查询，不参与核心 Dashboard 的 3 秒门槛。

## 参考数据集

参考数据集 ID 为 `reference-3m`。

| 项目 | 数量 |
| --- | ---: |
| 全库 events | 3,205,740 |
| 截至生成锚点的 30 天 events | 1,226,326 |
| 最近 90 天 hot events | 3,205,740 |
| archive events | 0 |
| identities | 500 |
| models | 50 |
| API keys | 50 |
| 数据库大小 | 2,044,776,448 bytes（约 1.90 GiB） |

数据集只保留最近 90 天的活跃事件。archive 表刻意保持为空，因为本套件覆盖的生产 Dashboard 路径不会查询冷数据。

50 个 API keys 确定性分为 15 个大用量、25 个中用量、10 个小用量 Key，每 Key 权重为 10:3:1；`usage_events` 数量根据这些权重归一化分配。500 identities、50 models 和 50 API keys 均有效且被事件实际引用，failed events 占生成负载的 1%。

canonical 必须通过行数、基数、孤儿引用、token 语义、派生 rollup/checkpoint、`PRAGMA quick_check`、维度分布和 semantic fingerprint 验证。metadata 还会绑定配置的错误率与流量分层，修改 manifest 后不能把不同负载配置生成的 canonical 重新标记为当前数据集。生成时以实际生成时间作为数据锚点；正式运行会拒绝最新事件已超过七天的数据集，并在 `dataset-validation.json` 中记录本次 30 天查询实际覆盖的 events 数量。

生成阶段不限制 CPU 或内存；运行阶段每个 probe 都从 canonical 创建独立 clone，避免 backlog、WAL、GC 和缓存污染下一个点。clone 会先落盘并从 controller page cache 中清除，再启动 Keeper，使 probe 期间读入的 SQLite 页面归入 Keeper cgroup。

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
├── datasets/reference-3m/
│   ├── app.db                  # 或 app.db.zst
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

## 环境要求

- Linux amd64，至少 4 个在线逻辑 CPU；
- cgroup v2 和 systemd transient units；
- Go、GCC、SQLite CLI、Redis server、`jq`、`taskset`；canonical 使用压缩数据库时还需要 `zstd`；
- 足够存放 canonical、probe clone、WAL、日志与结果的磁盘空间；
- 运行用户有权使用 `systemd-run` 和读取对应 cgroup 指标。

## 运行

直接在 benchmark 主机的仓库中运行：

```bash
BENCHMARK_ROOT=/path/to/benchmark-data \
PREPARE_DATASET=1 \
internal/benchmark/scripts/run-capacity.sh
```

`PREPARE_DATASET=1` 只在 canonical 不存在时生效。首次生成并验证完成后，本轮正式测试应省略该变量以复用相同数据：

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

默认会在目标机上构建 Keeper 和 `benchctl`、生成不可变 plan，并只对 canonical 做一次严格验证。每个 CPU 档从 25 events/s 开始向上递增；若 25 events/s 失败，同一搜索才在配置的 20/15/10/5/1 events/s 回退区间内二分。之后继续在成功/失败区间二分，并对 ingestion 边界两侧各做 60 秒复核；只有由此选出的候选才进入五分钟固定速率验证。五分钟 ingestion 细分在可行时使用 25 events/s 步进，区间小于该步进时改用 manifest 中配置的真实速率。如果所有 Dashboard 短测均未通过，最低 1 event/s 仍会接受一次五分钟正式验证；找到首个五分钟 Dashboard 通过点后，controller 会针对已知五分钟失败上界向上二分，直到找到配置中最高的通过点。各子运行复用同一份验证凭据，不会反复扫描完整数据库。只有 controller 脚本、结果契约、manifest、plan、二进制、数据 metadata、验证凭据、CPU 列表、端口、搜索策略和时长均未变化时，原 controller ID 才可恢复。

预期的固定速率容量失败属于终态边界证据；provenance 一致时，`resume` 会直接复用，不会在 controller 重启后再次执行。缺失结果、过期验证、panic、runtime/sampler 错误和负载器故障仍会让 controller 非零退出。只有每个请求 CPU 都恰好生成一行正式结果后，controller 才写入完成摘要。

controller 会为生成、验证、Keeper 与结果采集统一固定 `TZ=Asia/Shanghai`。数据集目录和 cell ID 从所选 manifest 及展开后的 plan 读取，因此兼容的自定义 manifest 不会继承参考数据集名称。

## 判定规则

Ingestion hard pass 必须同时满足：

- 至少 99.9% 目标事件成功发布；
- 最终 durable ratio 至少 99%；
- Redis inbox backlog 不增长；
- Overview、Activity、Latency checkpoint 和 Identity 聚合追平；
- 停止发送后 15 秒内完成追平；剩余 30 秒 drain 窗口只保留作诊断；
- 无 OOM、panic 或负载器 publish error。

核心 Dashboard pass 先要求 hard pass，再要求核心 HTTP 错误为 0，并要求 Realtime Overview 60m、Overview 30d、Activity 30d、Analysis 30d 和 Request Events 30d 的整体 p99 不超过 3000ms；预热后以 1 req/s 轮询这五个接口。

Analysis Latency 30d 先预热一次，probe 期间每 30 秒请求一次，单独报告成功样本数、错误数、状态以及 p50/p95/p99/max。诊断错误不改变 ingestion 或核心 Dashboard 容量；OOM、panic 或其它 Keeper 全局故障仍会使本轮无效。

每个正式容量点都以固定速率持续 300 秒。短时升压和边界探测只选择候选，不进入最终容量结论。controller 同时报告最高五分钟成功速率和最低已观察失败速率，因此容量结论是一个区间，而不是未经证明的绝对精确上限；失败边界为零表示在配置的速率矩阵内没有观察到失败。结果中的 `offered_events`、`published_events` 和 `durable_events` 分开记录，不能把负载器未达到目标速率误报为 Keeper 容量。

## 结果文件

每个运行点生成 `run.json`、`results.jsonl`、`summary.json`、`summary.csv` 和 `report.md`；控制器额外生成 `controller-inputs.sha256`、`dataset-validation.json`、汇总表 `controller.tsv`、环境规格 `environment.txt` 和容量摘要 `summary.md`。

当前正式测量结果见 [REPORT.zh.md](REPORT.zh.md)。
