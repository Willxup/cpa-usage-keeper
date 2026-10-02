package app

import (
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/poller"
	"gorm.io/gorm"
)

// newPricingBootstrapIngestRunner 在同一接收生命周期内先写实际旧 inbox 列，业务就绪后移交标准写列与通知。
func newPricingBootstrapIngestRunner(cfg config.Config, db *gorm.DB) (*poller.RedisIngestRunner, *UsageIngestBridge) {
	bridge := NewUsageIngestBridge(poller.NewPricingBootstrapInboxWriter(db))
	return newUsageIngestRunner(cfg, bridge, bridge), bridge
}

// newUsageIngestRunner 复用订阅、Redis pull 和 HTTP pull 的既有优先级及降级合同。
// writer 决定启动引导或正常 schema 的 inbox 写列；observer 只在业务运行阶段注入。
func newUsageIngestRunner(cfg config.Config, writer poller.RedisInboxWriter, observer poller.RedisControlMessageObserver) *poller.RedisIngestRunner {
	redisPullSource := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		BaseURL: cfg.CPABaseURL, RedisAddr: cfg.RedisQueueAddr, ManagementKey: cfg.CPAManagementKey,
		Timeout: cfg.RequestTimeout, BatchSize: cfg.RedisQueueBatchSize,
		TLS: cfg.RedisQueueTLS, TLSSkipVerify: cfg.TLSSkipVerify,
	})
	httpPullSource := poller.NewHTTPPullSource(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify, cfg.RedisQueueBatchSize)
	redisSubscribeSource := poller.NewRedisSubscribeSource(poller.RedisSubscribeOptions{
		BaseURL: cfg.CPABaseURL, RedisAddr: cfg.RedisQueueAddr, ManagementKey: cfg.CPAManagementKey,
		Timeout: cfg.RequestTimeout, TLS: cfg.RedisQueueTLS, TLSSkipVerify: cfg.TLSSkipVerify,
	})
	filteredWriter := poller.NewControlAwareRedisInboxWriter(writer, observer)
	runner := poller.NewRedisIngestRunner(redisSubscribeSource, redisPullSource, httpPullSource, filteredWriter, poller.RedisIngestRunnerConfig{
		IdleInterval: cfg.RedisQueueIdleInterval, BatchSize: cfg.RedisQueueBatchSize,
		HTTPBackoffInitial: time.Second, HTTPBackoffMax: 30 * time.Second,
	})
	if observer != nil {
		runner.SetControlMessageObserver(observer)
	}
	return runner
}
