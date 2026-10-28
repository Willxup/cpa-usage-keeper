# 开发指南

[返回 README](../README.zh.md) · [English](./development.md)

项目结构和启动步骤见 README 的[项目结构](../README.zh.md#项目结构)与[本地开发](../README.zh.md#本地开发)。

## 测试

在仓库根目录运行完整验证：

```bash
make verify
```

也可以分别运行：

```bash
go test ./cmd/... ./internal/...
npm --prefix ./web run test
npm --prefix ./web run lint
npm --prefix ./web run typecheck
npm --prefix ./web run build
```

## 图表时间处理

### 基本原则

- 使用响应或配置指定的时区。自然日不一定是 24 小时；滚动窗口按实际经过时长计算，日均速率使用该自然日的实际分钟数。
- 用真实时刻或完整日期标识时间桶，按真实时间排序；格式化的钟面标签仅用于展示。
- 查询修复不应改变已有聚合的写入边界。Overview、Latency 的小时起点不同，Activity 的 364 格布局及已有桶结束时间也应保留，避免改变历史数据含义。

### 复用代码

- 自然日计算：`internal/timeutil.CivilDate`、`DayBounds`；聚合读取使用 repository 的桶适配方法。
- 原始事件过滤：`internal/repository/usage_event_time_read.go`。普通查询使用有界分段，近期缓存启动或重载使用开放上界；字段选择和缓存生命周期仍由调用方处理。
- 前端标签：`web/src/utils/usage/timeLabels.ts`。仅为存在歧义的当地钟面补充时区偏移，保留现有语言、精度、回退及补点规则。

### 验证要求

覆盖上海普通日期、夏令时跳时与回拨，以及浏览器和响应时区不同的情况。确认改动未影响图表样式和生命周期行为。
