package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	webui "cpa-usage-keeper/web"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Runner 是 App 后台任务的最小接口，具体语义由字段名和实现方法表达。
type Runner interface {
	Run(ctx context.Context) error
}

// StatusProvider 只提供运行状态，不作为后台 runner 启动。
type StatusProvider interface {
	Status() poller.Status
}

type Options struct {
	EnvFile string
	AppHost string
}

type QuotaRunner interface {
	SetRefreshContext(context.Context)
	StopRefreshTasks()
	WaitRefreshTasks()
	StartAutoRefresh(context.Context) error
}

// PricingTaskWaiter 只暴露关闭数据库前必须等待的重算生命周期边界。
type PricingTaskWaiter interface {
	WaitPricingRecalculation()
}

type App struct {
	Config *config.Config
	// StartupShell 先于数据库初始化对外服务，业务准备完成后原子切到 Router。
	StartupShell *api.StartupShell
	// DB 是统一 GORM 入口：普通查询由 dbresolver 路由到 reader，写入和默认事务留在 writer。
	DB *gorm.DB
	// ReadDB 只保留 reader 的生命周期和池状态入口；业务服务不得再自行选择数据库池。
	ReadDB       *gorm.DB
	Router       *gin.Engine
	Poller       StatusProvider
	RedisIngest  Runner
	RedisProcess Runner
	// CPAErrors 是完全独立的 best-effort errors 订阅；停止或失败不影响 Usage 与 HTTP。
	CPAErrors Runner
	// UsageAggregation 是唯一串行调度三类派生聚合事务的后台 runner。
	UsageAggregation  Runner
	Ranking           Runner
	LocalRanking      Runner
	Maintenance       *StorageCleanupRunner
	MetadataSync      *MetadataSyncRunner
	QuotaService      QuotaRunner
	QuotaAutoRefresh  QuotaRunner
	BackupMaintenance *DatabaseBackupRunner
	RecentUsageCache  *repository.UsageRecentEventCache
	CostReadGate      *service.CostReadGate
	PricingCatalog    *pricing.Catalog
	PricingService    PricingTaskWaiter
	LogCloser         io.Closer

	backgroundCancel  context.CancelFunc
	backgroundContext context.Context
	backgroundWG      sync.WaitGroup
	ingestBridge      *UsageIngestBridge
	serveStartup      bool
	ingestStarted     bool
}

// newUsageRecentEventCache 是最近事件缓存构造入口，测试可替换它来覆盖缓存初始化失败路径。
var newUsageRecentEventCache = repository.NewUsageRecentEventCache

func New() (*App, error) {
	return NewWithOptions(Options{})
}

func NewWithOptions(options Options) (*App, error) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: options.EnvFile, AppHost: options.AppHost})
	if err != nil {
		return nil, err
	}

	return NewWithConfig(*cfg)
}

func NewWithConfig(cfg config.Config) (*App, error) {
	logCloser, err := logging.Configure(cfg)
	if err != nil {
		return nil, err
	}
	return &App{
		Config:       &cfg,
		LogCloser:    logCloser,
		StartupShell: api.NewStartupShell(webui.Static, appAuthConfig(cfg), cfg.AppBasePath),
	}, nil
}

// buildReadyApp 只在首次迁移数据完成后构造完整业务对象，不负责打开或关闭启动阶段的数据库池。
func buildReadyApp(cfg config.Config, db, readDB *gorm.DB, logCloser io.Closer, lifecycleCtx context.Context, ingestRunner *poller.RedisIngestRunner) (*App, error) {
	// Ranking 完全复用现有 app_settings 和统一 DB；构造阶段不访问中心，默认 disabled 没有外部请求。
	rankingService, err := ranking.NewService(ranking.NewStore(db), ranking.NewAggregator(db), ranking.NewClient())
	if err != nil {
		return nil, err
	}
	rankingRunner, err := ranking.NewRunner(rankingService)
	if err != nil {
		return nil, err
	}
	// M8 必须先成功加载共享费用缓存；重算结束要在同一对象重载，不能以 nil 伪装业务就绪。
	recentUsageCache, err := newUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{})
	if err != nil {
		return nil, fmt.Errorf("initialize recent usage event cache: %w", err)
	}
	localRankingService, err := ranking.NewLocalRankingService(db, ranking.LocalRankingServiceOptions{})
	if err != nil {
		recentUsageCache.Close()
		return nil, err
	}
	localRankingRunner, err := ranking.NewLocalRankingRunner(localRankingService)
	if err != nil {
		recentUsageCache.Close()
		return nil, err
	}
	pricingSnapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		recentUsageCache.Close()
		return nil, fmt.Errorf("load pricing snapshot: %w", err)
	}
	pricingCatalog := pricing.NewCatalog(pricingSnapshot)
	// 单个 App 共享一把费用读取许可，供 HTTP 费用请求和后续手动重算协调。
	costReadGate := service.NewCostReadGate()

	cpaClient := cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify)
	quotaService := quota.NewServiceWithOptions(db, cpaClient, quota.ServiceOptions{
		RefreshWorkerLimit:            cfg.QuotaRefreshWorkerLimit,
		QuotaUpstreamResponsesEnabled: cfg.QuotaUpstreamResponsesEnabled,
	})
	prepared := false
	defer func() {
		if !prepared {
			// StopRefreshTasks 已同步等待刷新 worker，清理后才能关闭缓存和共享数据库。
			quotaService.StopRefreshTasks()
			recentUsageCache.Close()
		}
	}()
	// 单 writer aggregation runner 只维护 rollups/Identity，并在 App.Run 时主动追平。
	usageAggregationRunner := poller.NewUsageAggregationRunner(db)
	// syncService 仍然是 metadata 和 usage 处理共享的业务服务入口。
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:        cfg.CPABaseURL,
		Client:         cpaClient,
		PricingCatalog: pricingCatalog,
		// usage_events 事务提交后通过这个缓存做非阻塞增量追加，供 Overview realtime 和右边界补偿复用。
		RecentUsageEvents: recentUsageCache,
		// usage 与 metadata 提交后只唤醒单 writer runner，不在前台链路执行派生聚合。
		UsageAggregationNotifier: usageAggregationRunner,
		// Header 独立进入 Quota worker 的惰性一分钟窗口，不再等待 Overview 水位。
		UsageHeaderQuota: quotaService,
	})
	// metadataSyncRunner 提前创建，保证控制消息和后台任务使用同一个调度器实例。
	metadataSyncRunner := NewMetadataSyncRunner(syncService, cfg.MetadataSyncInterval)
	// 同一接收 runner 在迁移与业务阶段存活；桥接的 writer/observer 在库就绪后一次移交。
	// redisProcessRunner 仍然只处理本地 inbox 到 usage_events 的消费。
	redisProcessRunner := poller.NewRedisProcessRunner(syncService)
	// errorEventService 同时承担 Errors runner 的直接写入和详情 API 的分页读取。
	errorEventService := service.NewErrorEventService(db)
	// Errors 使用独立 RESP source，固定订阅 errors channel，不修改或复用 Usage subscriber 生命周期。
	redisErrorSubscribeSource := poller.NewRedisErrorSubscribeSource(poller.RedisSubscribeOptions{
		BaseURL:       cfg.CPABaseURL,
		RedisAddr:     cfg.RedisQueueAddr,
		ManagementKey: cfg.CPAManagementKey,
		Timeout:       cfg.RequestTimeout,
		TLS:           cfg.RedisQueueTLS,
		TLSSkipVerify: cfg.TLSSkipVerify,
	})
	redisErrorIngestRunner := poller.NewRedisErrorIngestRunner(redisErrorSubscribeSource, errorEventService)
	// backgroundPoller 继续组合远端 ingest 和本地 process 的状态展示。
	backgroundPoller := poller.NewRedisPoller(ingestRunner, redisProcessRunner)
	var backupMaintenance *DatabaseBackupRunner
	if cfg.BackupEnabled {
		// 备份继续借用唯一 writer 连接，保持旧版串行快照语义，避免独立连接持续写入时反复重启在线备份。
		sqlDB, err := db.DB()
		// 无法取得 writer 底层池时，M8 失败但外层仍持有数据库与接收生命周期。
		if err != nil {
			return nil, fmt.Errorf("initialize database backup maintenance: %w", err)
		}
		// 备份期间其它写入继续在 writer 池外排队；页面查询仍可使用独立 reader，不恢复旧版的全局读阻塞。
		backupStore := newDatabaseBackupStore(sqlDB, cfg.BackupDir)
		// runner 继续沿用原调度、备份内容与保留策略，本次不改变任何文件结果。
		backupMaintenance = NewDatabaseBackupRunner(backupStore, backupStore, cfg.BackupInterval, cfg.BackupRetentionDays)
	}

	// 所有服务统一接收同一个 DB；Query/Row 走 reader，Create/Update/Delete 和默认事务走 writer。
	usageService := service.NewUsageServiceWithOptions(db, service.UsageServiceOptions{
		RecentUsage:    recentUsageCache,
		PricingCatalog: pricingCatalog,
	})
	requestLogService := service.NewRequestLogService(db, cpaClient)
	usageIdentityService := service.NewUsageIdentityServiceWithOptions(db, recentUsageCache, service.UsageIdentityServiceOptions{
		OnDisplayNameChanged: quotaService.UpdateUsageIdentityDisplayNameSnapshot,
	})
	cpaAPIKeyService := service.NewCPAAPIKeyService(db)
	// 单条凭证开关成功后立即与 CPA 对齐；runner 自带合并窗口和 nil 保护。
	credentialMutationLocks := &service.CredentialMutationLocks{}
	authFilesManagementService := service.NewAuthFilesManagementService(cpaClient, credentialMutationLocks)
	credentialStatusService := service.NewCredentialStatusService(db, cpaClient, metadataSyncRunner, credentialMutationLocks)
	credentialPriorityService := service.NewCredentialPriorityService(db, cpaClient, metadataSyncRunner, credentialMutationLocks)
	if cfg.TLSSkipVerify {
		logrus.WithField("cpa_base_url", cfg.CPABaseURL).Warn("TLS certificate verification is disabled for CPA and Redis queue connections")
	}
	pricingService := service.NewPricingServiceWithRecalculation(db, pricingCatalog, service.PricingRecalculationDependencies{
		LifecycleCtx: lifecycleCtx, Sync: syncService, Aggregation: usageAggregationRunner,
		CostReadGate: costReadGate, Recent: recentUsageCache, Quota: quotaService,
	}, cpaClient)
	sessionManager := auth.NewSessionManager(cfg.AuthSessionTTL)
	if cfg.AuthEnabled {
		// Session Get/List 自动走 reader，Save/Delete 仍由写回调路由到唯一 writer。
		sessionManager = auth.NewPersistentSessionManager(cfg.AuthSessionTTL, auth.NewGormSessionStore(db))
	}
	authConfig := appAuthConfig(cfg)
	authHandler := api.NewAuthHandler(authConfig, sessionManager)

	ready := &App{
		Config: &cfg,
		// 对外保留单一 DB 入口，现有服务和后台任务不需要感知物理池。
		DB: db,
		// ReadDB 只负责文件 reader 的状态和关闭；内存库与 DB 相同，关闭时只处理一次。
		ReadDB: readDB,
		Poller: backgroundPoller,
		// Redis ingest/process 分成两个后台 runner，避免远端订阅拉取和本地 SQLite 处理互相等待。
		RedisIngest:       ingestRunner,
		RedisProcess:      redisProcessRunner,
		CPAErrors:         redisErrorIngestRunner,
		UsageAggregation:  usageAggregationRunner,
		Ranking:           rankingRunner,
		LocalRanking:      localRankingRunner,
		Maintenance:       NewStorageCleanupRunner(syncService),
		MetadataSync:      metadataSyncRunner,
		QuotaService:      quotaService,
		QuotaAutoRefresh:  quotaService,
		BackupMaintenance: backupMaintenance,
		RecentUsageCache:  recentUsageCache,
		CostReadGate:      costReadGate,
		PricingCatalog:    pricingCatalog,
		PricingService:    pricingService,
		LogCloser:         logCloser,
		Router: api.NewRouter(
			webui.Static,
			backgroundPoller,
			usageService,
			pricingService,
			authConfig,
			authHandler,
			cfg.AppBasePath,
			api.OptionalProviders{
				UsageIdentity: usageIdentityService,
				CostReadGate:  costReadGate,
				ErrorEvents:   errorEventService,
				Quota:         quotaService,
				CPAAPIKeys:    cpaAPIKeyService,
				AuthFiles:     authFilesManagementService,
				// 认证文件与 AI 供应商共用一个 service，路由层按类型分发。
				CredentialStatus:   credentialStatusService,
				CredentialPriority: credentialPriorityService,
				RequestLogs:        requestLogService,
				Ranking:            rankingService,
				LocalRanking:       localRankingService,
				Status: api.StatusRouteConfig{
					CPAPublicURL:               cfg.CPAPublicURL,
					CPARequestLogAccessEnabled: cfg.CPARequestLogAccessEnabled,
				},
			},
		),
	}
	prepared = true
	return ready, nil
}

// appAuthConfig 让启动外壳和完整路由使用同一 BasePath、TLS 页面祖先及认证约束。
func appAuthConfig(cfg config.Config) api.AuthConfig {
	return api.AuthConfig{
		Enabled:                         cfg.AuthEnabled,
		LoginPassword:                   cfg.LoginPassword,
		SessionTTL:                      cfg.AuthSessionTTL,
		BasePath:                        cfg.AppBasePath,
		FrameAncestorOrigins:            frameAncestorOrigins(cfg),
		TrustedProxyCIDRs:               cfg.TrustedProxyCIDRs,
		APIKeyViewerLocalRankingEnabled: cfg.APIKeyViewerLocalRankingEnabled,
	}
}

func frameAncestorOrigins(cfg config.Config) []string {
	// 只信任显式浏览器公开地址；CPA_BASE_URL 可能是内网地址，不能进入 frame-ancestors。
	if origin, ok := publicOrigin(cfg.CPAPublicURL); ok {
		return []string{origin}
	}
	return nil
}

func publicOrigin(candidate string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + parsed.Host, true
}

func closeGormDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}

	a.stopBackgroundTasks()
	if a.PricingService != nil {
		a.PricingService.WaitPricingRecalculation()
	}
	if a.QuotaService != nil {
		// StopRefreshTasks 已同步等待刷新 worker，随后才可释放缓存和数据库。
		a.QuotaService.StopRefreshTasks()
	}
	if a.RecentUsageCache != nil {
		a.RecentUsageCache.Close()
		a.RecentUsageCache = nil
	}

	var closeErr error
	// 文件库先关闭独立 reader；内存库的 ReadDB 与 DB 相同，必须留给 writer 分支只关闭一次。
	if a.ReadDB != nil {
		// 只有独立池才需要单独关闭，避免内存库重复关闭同一个 database/sql。
		if a.ReadDB != a.DB {
			// 合并关闭错误，仍继续尝试关闭 writer 和日志资源。
			closeErr = errors.Join(closeErr, closeGormDB(a.ReadDB))
		}
		// 清空字段避免重复 Close 再次操作已经关闭的连接池。
		a.ReadDB = nil
	}
	// reader 释放后再关闭 writer，保持初始化顺序的逆序资源回收。
	if a.DB != nil {
		// writer 关闭失败也只并入结果，不能跳过后续日志资源清理。
		closeErr = errors.Join(closeErr, closeGormDB(a.DB))
		// 清空 writer 字段，使重复 Close 保持幂等。
		a.DB = nil
	}
	// 数据库连接池全部关闭后再释放日志输出资源。
	if a.LogCloser != nil {
		// 日志关闭错误与数据库关闭错误一起返回，保留完整清理结果。
		closeErr = errors.Join(closeErr, a.LogCloser.Close())
		// 清空日志字段，避免重复关闭同一个文件句柄。
		a.LogCloser = nil
	}
	return closeErr
}

func (a *App) Run() error {
	return a.RunContext(context.Background())
}

// startReadyBackgroundTasks 只在费用数据及完整 Router 都已准备后启动普通消费、聚合与维护。
func (a *App) startReadyBackgroundTasks(ctx context.Context) {
	if a.RedisIngest != nil && !a.ingestStarted {
		a.ingestStarted = true
		a.startBackgroundTask(func() {
			if err := a.RedisIngest.Run(ctx); err != nil {
				logrus.Errorf("redis ingest stopped: %v", err)
			}
		})
	}
	if a.RedisProcess != nil {
		a.startBackgroundTask(func() {
			if err := a.RedisProcess.Run(ctx); err != nil {
				logrus.Errorf("redis process stopped: %v", err)
			}
		})
	}
	if a.CPAErrors != nil {
		a.startBackgroundTask(func() {
			// Errors 是可选在线观测；runner 内部吞掉订阅/写入失败，这里只防御意外实现错误。
			if err := a.CPAErrors.Run(ctx); err != nil {
				logrus.Errorf("CPA errors ingest stopped: %v", err)
			}
		})
	}
	if a.UsageAggregation != nil {
		// 聚合 runner 使用独立 App 生命周期 goroutine，但内部始终串行执行一个 SQLite writer。
		a.startBackgroundTask(func() {
			// runner 错误只终止该后台任务，不影响 HTTP 或已提交 usage 数据。
			if err := a.UsageAggregation.Run(ctx); err != nil {
				logrus.Errorf("usage aggregation stopped: %v", err)
			}
		})
	}
	if a.Ranking != nil {
		a.startBackgroundTask(func() {
			// 排名中心故障只能终止本次可选同步任务，不能影响 Keeper HTTP 或 usage 采集。
			if err := a.Ranking.Run(ctx); err != nil {
				logrus.Errorf("ranking synchronization stopped: %v", err)
			}
		})
	}
	if a.Maintenance != nil {
		a.startBackgroundTask(func() {
			if err := a.Maintenance.Run(ctx); err != nil {
				logrus.Errorf("maintenance cleanup stopped: %v", err)
			}
		})
	}
	if a.MetadataSync != nil {
		a.startBackgroundTask(func() {
			if err := a.MetadataSync.Run(ctx); err != nil {
				logrus.Errorf("metadata sync stopped: %v", err)
			}
		})
	}
	if a.LocalRanking != nil {
		a.startBackgroundTask(func() {
			// Metadata 已先启动；Local runner 再等待首个五分钟周期，让 usage 与 Key 信息完成启动追赶。
			if err := a.LocalRanking.Run(ctx); err != nil {
				logrus.Errorf("local ranking aggregation stopped: %v", err)
			}
		})
	}
	if a.QuotaService != nil {
		a.QuotaService.SetRefreshContext(ctx)
	}
	if a.QuotaAutoRefresh != nil {
		a.startBackgroundTask(func() {
			// quota 自动刷新和手动刷新共用队列，但作为独立后台任务跟随 App 生命周期启动和停止。
			if err := a.QuotaAutoRefresh.StartAutoRefresh(ctx); err != nil {
				logrus.Errorf("quota auto refresh stopped: %v", err)
			}
		})
	}
	if a.BackupMaintenance != nil {
		a.startBackgroundTask(func() {
			if err := a.BackupMaintenance.Run(ctx); err != nil {
				logrus.Errorf("database backup stopped: %v", err)
			}
		})
	}

}

func (a *App) startBackgroundContext(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	a.backgroundCancel = cancel
	a.backgroundContext = ctx
	return ctx
}

func (a *App) startBackgroundTask(run func()) {
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		run()
	}()
}

func (a *App) stopBackgroundTasks() {
	if a.backgroundCancel != nil {
		a.backgroundCancel()
		a.backgroundCancel = nil
	}
	a.backgroundWG.Wait()
}
