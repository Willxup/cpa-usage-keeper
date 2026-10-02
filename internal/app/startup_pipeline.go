package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
)

// Initialize 沿生产启动的同一 M0–M8 路径准备数据库和业务对象；独立调用时不自行监听或拉取 CPA。
// RunContext 已监听外壳时，本方法在引导 inbox 建好后立即启动唯一接收 runner，再执行旧库保护和回填。
func (a *App) Initialize(ctx context.Context) error {
	if a == nil || a.Config == nil || a.StartupShell == nil {
		return fmt.Errorf("startup shell is not initialized")
	}
	if a.DB != nil || a.Router != nil {
		return fmt.Errorf("application initialization already started")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if a.backgroundContext == nil {
		a.startBackgroundContext(ctx)
	}
	db, reader, err := repository.OpenUnmigratedDatabasePools(*a.Config)
	if err != nil {
		return fmt.Errorf("open unmigrated database: %w", err)
	}
	a.DB, a.ReadDB = db, reader
	state, err := repository.BootstrapPricingInitialization(ctx, db)
	if err != nil {
		return fmt.Errorf("fix pricing initialization identity: %w", err)
	}
	if err := repository.EnsurePricingBootstrapInbox(ctx, db); err != nil {
		return fmt.Errorf("prepare durable usage inbox: %w", err)
	}
	ingest, bridge := newPricingBootstrapIngestRunner(*a.Config, db)
	a.ingestBridge = bridge
	a.RedisIngest = ingest
	if a.serveStartup {
		a.ingestStarted = true
		a.startBackgroundTask(func() {
			if runErr := ingest.Run(a.backgroundContext); runErr != nil {
				logrus.WithError(runErr).Error("pricing bootstrap ingest stopped")
			}
		})
	}
	if err := a.StartupShell.Publish(api.StartupPhaseMigrating, nil); err != nil {
		return err
	}
	switch state.InitKind {
	case repository.PricingInitKindFresh:
		if err := repository.InitializeFreshPricingDatabase(ctx, db); err != nil {
			return fmt.Errorf("initialize fresh pricing database: %w", err)
		}
	case repository.PricingInitKindLegacy:
		if !state.DataComplete {
			baseline, err := repository.MigrateLegacyPricingEvents(ctx, db, reader, a.Config.BackupDir, time.Now())
			if err != nil {
				return fmt.Errorf("migrate legacy pricing events: %w", err)
			}
			if err := repository.CompleteLegacyPricingData(ctx, db, reader, baseline); err != nil {
				return fmt.Errorf("complete legacy pricing data: %w", err)
			}
		}
	default:
		return fmt.Errorf("unknown pricing initialization identity")
	}
	if err := repository.VerifyPricingStartupDataComplete(ctx, db); err != nil {
		return err
	}
	// 价格数据已经受保护并完成后，再补齐后来注册的普通版本；重启不跳过合法新增 migration。
	if err := repository.RunDatabaseMigrationsWithBackup(ctx, db, reader, a.Config.BackupDir); err != nil {
		return fmt.Errorf("run remaining database migrations: %w", err)
	}
	ready, err := buildReadyApp(*a.Config, db, reader, a.LogCloser, a.backgroundContext, ingest)
	if err != nil {
		return fmt.Errorf("prepare business services: %w", err)
	}
	a.installReadyApp(ready)
	if err := bridge.ActivateNormalWriter(poller.NewRedisInboxWriter(db), a.MetadataSync); err != nil {
		return fmt.Errorf("activate normal usage ingest: %w", err)
	}
	return nil
}

// installReadyApp 只移交已构造的业务字段；HTTP 外壳、生命周期和唯一接收实例仍属原 App。
func (a *App) installReadyApp(ready *App) {
	a.Poller, a.RedisIngest, a.RedisProcess = ready.Poller, ready.RedisIngest, ready.RedisProcess
	a.CPAErrors, a.UsageAggregation = ready.CPAErrors, ready.UsageAggregation
	a.Ranking, a.LocalRanking, a.Maintenance = ready.Ranking, ready.LocalRanking, ready.Maintenance
	a.MetadataSync, a.QuotaService, a.QuotaAutoRefresh = ready.MetadataSync, ready.QuotaService, ready.QuotaAutoRefresh
	a.BackupMaintenance, a.RecentUsageCache = ready.BackupMaintenance, ready.RecentUsageCache
	a.CostReadGate, a.PricingCatalog, a.PricingService = ready.CostReadGate, ready.PricingCatalog, ready.PricingService
	a.Router = ready.Router
}

// RunContext 先监听独立 HTTP 外壳，再运行同一 Initialize；失败仍提供公开状态与静态页面直到进程停止。
func (a *App) RunContext(ctx context.Context) error {
	if a == nil || a.Config == nil || a.StartupShell == nil {
		return fmt.Errorf("application startup shell is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listener, err := net.Listen("tcp", a.Config.ListenAddress())
	if err != nil {
		return fmt.Errorf("listen for startup shell: %w", err)
	}
	server := NewHTTPServer(*a.Config, a.StartupShell)
	if a.Config.TLSEnabled {
		certificate, err := tls.LoadX509KeyPair(a.Config.TLSCertFile, a.Config.TLSKeyFile)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("load startup TLS certificate: %w", err)
		}
		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certificate}}
	}
	acceptReady := &startupAcceptReadyListener{Listener: listener, ready: make(chan struct{})}
	serverDone := make(chan error, 1)
	go func() {
		if a.Config.TLSEnabled {
			serverDone <- server.ServeTLS(acceptReady, "", "")
		} else {
			serverDone <- server.Serve(acceptReady)
		}
	}()
	select {
	case <-acceptReady.ready:
	case err := <-serverDone:
		return fmt.Errorf("serve startup shell: %w", err)
	}
	defer a.stopBackgroundTasks()
	a.serveStartup = true
	if err := a.Initialize(ctx); err != nil {
		logrus.WithError(err).Error("pricing startup failed")
		if publishErr := a.StartupShell.Publish(api.StartupPhaseFailed, nil); publishErr != nil {
			logrus.WithError(publishErr).Error("publish pricing startup failure")
		}
	} else {
		a.startReadyBackgroundTasks(a.backgroundContext)
		if err := a.StartupShell.ActivateReady(a.Router); err != nil {
			logrus.WithError(err).Error("activate business router")
			_ = a.StartupShell.Publish(api.StartupPhaseFailed, nil)
		}
	}
	select {
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown startup HTTP shell: %w", err)
		}
		return nil
	}
}

// startupAcceptReadyListener 只确认 HTTP Serve 已进入接收循环，避免监听或 TLS 失败后仍改动旧库。
type startupAcceptReadyListener struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *startupAcceptReadyListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.ready) })
	return l.Listener.Accept()
}
