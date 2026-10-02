# 价格测试运行指南

[English](pricing.md) · [测试报告](../reports/pricing/README.zh.md) · [全部 Benchmark](../README.zh.md)

## 环境要求

Linux amd64、Go，以及编译 SQLite 所需的 GCC；HTTP 对照另需 Python 3。使用独立测试环境，为生成的数据库、WAL 和备份预留空间。运行器自行生成合成数据。

## 运行

从仓库根目录运行，输出到仓库外的新目录。报告中的资源限制需另行配置，脚本不会自动施加；各场景顺序执行，为生成数据、WAL 和完整备份预留磁盘。

```bash
export TZ=UTC GOMEMLIMIT=5GiB
export PRICING_BENCHMARK_REVISION=$(git rev-parse HEAD)
internal/benchmark/scripts/run-pricing-benchmark.sh <new-output-dir> 1000000 latest 2026-09-23T00:00:00Z
```

千万场景使用 `10000000 latest`，待执行旧迁移的场景使用 `1000000 five-dim`。脚本构建当前检出代码；`PRICING_BENCHMARK_REVISION` 只记录标识，不切换版本。原始源码及二进制校验值保留在 [报告结果](../reports/pricing/results/)。原实验直接使用已构建二进制；后来移除主机名采集不改写原清单。百万场启动清单早于最终查询对比脚本，对比实际使用的脚本哈希见 [environment.json](../reports/pricing/results/environment.json)。

查询对照需构建两个产品版本，使用百万实验的旧备份及已升级数据库：

```bash
python3 internal/benchmark/scripts/compare-pricing-queries.py \
  --old-binary <old-binary> --new-binary <new-binary> \
  --backup <M1-backup.db> --database <upgraded-database.db> \
  --root <new-comparison-dir> --anchor 2026-09-23T00:00:00Z --samples 5
```

## 输出

每个新输出目录包含 `pricing-report.json`、`environment.txt`、测试二进制及合成数据库和迁移备份。脚本拒绝覆盖已有同名输出，运行后不自动删除。核验结果后再替换公开报告，数据库和原始日志不入仓库。
