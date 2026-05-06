package repository

// 仓储层统一使用 900 行作为默认批量写入大小，避免 SQLite 变量数量超过限制。
const defaultRepositoryInsertBatchSize = 900
