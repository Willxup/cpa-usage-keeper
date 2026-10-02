package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

type StartupPhase string

const (
	StartupPhaseOpening   StartupPhase = "opening"
	StartupPhaseMigrating StartupPhase = "migrating"
	StartupPhaseFailed    StartupPhase = "failed"
	StartupPhaseReady     StartupPhase = "ready"
)

// StartupProgress 只表达可靠的已处理量；未知总量保留 JSON null，不生成百分比。
type StartupProgress struct {
	ProcessedCount int64  `json:"processed_count"`
	TotalCount     *int64 `json:"total_count"`
}

// StartupStatus 是公开且不依赖业务数据库的启动状态合同。
type StartupStatus struct {
	Phase    StartupPhase     `json:"phase"`
	Message  string           `json:"message"`
	Progress *StartupProgress `json:"progress"`
}

type startupTarget struct {
	status  StartupStatus
	handler http.Handler
}

// StartupShell 始终只替换完整 http.Handler 指针，从不并发修改 Gin 路由树。
// App 在业务库尚未就绪时先发布启动外壳，完成数据与运行初始化后原子切换到完整 Router。
type StartupShell struct {
	target atomic.Pointer[startupTarget]
}

// NewStartupShell 在业务库尚未就绪时装配公开状态、存活检查和静态页面。
// 普通 API 在切换前返回迁移中；调用方完成数据与运行环境检查后再调用 ActivateReady。
func NewStartupShell(staticFS fs.FS, authConfig AuthConfig, basePath string) *StartupShell {
	shell := &StartupShell{}
	router := newRouterEngine(authConfig)
	appGroup := router.Group(basePath)
	registerHealthRoutes(appGroup)
	apiV1 := appGroup.Group("/api/v1")
	registerStartupStatusRoute(apiV1, shell.Status)
	registerStaticRoutes(router, appGroup, staticFS, authConfig.FrameAncestorOrigins, basePath, func(c *gin.Context) {
		setNoStoreHeaders(c)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "migration_in_progress", "message": "Service is starting. Please try again shortly.",
		})
	})
	shell.target.Store(&startupTarget{status: startupStatusForPhase(StartupPhaseOpening, nil), handler: router})
	return shell
}

// Publish 只接受粗粒度阶段与数量；固定公共文案使 SQL、路径和原始消息不能进入状态响应。
func (s *StartupShell) Publish(phase StartupPhase, progress *StartupProgress) error {
	if s == nil {
		return fmt.Errorf("startup shell is nil")
	}
	if phase != StartupPhaseOpening && phase != StartupPhaseMigrating && phase != StartupPhaseFailed {
		return fmt.Errorf("startup phase %q cannot be published before a ready handler exists", phase)
	}
	if progress != nil {
		if progress.ProcessedCount < 0 || progress.TotalCount != nil && (*progress.TotalCount < 0 || progress.ProcessedCount > *progress.TotalCount) {
			return fmt.Errorf("startup progress is invalid")
		}
	}
	status := startupStatusForPhase(phase, progress)
	for {
		current := s.target.Load()
		if current == nil || current.status.Phase == StartupPhaseReady {
			return fmt.Errorf("startup shell is already ready")
		}
		if s.target.CompareAndSwap(current, &startupTarget{status: status, handler: current.handler}) {
			return nil
		}
	}
}

// ActivateReady 在完整 Router 已构建并完成业务就绪检查后一次性对外切换。
func (s *StartupShell) ActivateReady(fullHandler http.Handler) error {
	if s == nil || fullHandler == nil || fullHandler == s {
		return fmt.Errorf("startup ready handler is missing")
	}
	for {
		current := s.target.Load()
		if current == nil || current.status.Phase == StartupPhaseReady {
			return fmt.Errorf("startup shell is already ready")
		}
		if s.target.CompareAndSwap(current, &startupTarget{status: readyStartupStatus(), handler: fullHandler}) {
			return nil
		}
	}
}

func (s *StartupShell) Status() StartupStatus {
	if s == nil {
		return startupStatusForPhase(StartupPhaseFailed, nil)
	}
	target := s.target.Load()
	if target == nil {
		return startupStatusForPhase(StartupPhaseFailed, nil)
	}
	current := target.status
	return startupStatusForPhase(current.Phase, current.Progress)
}

// ServeHTTP 为每个请求读取一次外层 handler 指针，保证路由和就绪状态同时切换。
func (s *StartupShell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	target := s.target.Load()
	if target == nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	target.handler.ServeHTTP(w, r)
}

// registerStartupStatusRoute 在认证中间件前发布状态；响应只用内存快照并禁止缓存。
func registerStartupStatusRoute(router gin.IRoutes, status func() StartupStatus) {
	router.GET("/startup/status", func(c *gin.Context) {
		setNoStoreHeaders(c)
		c.JSON(http.StatusOK, status())
	})
}

func readyStartupStatus() StartupStatus { return startupStatusForPhase(StartupPhaseReady, nil) }

// startupStatusForPhase 固定公开文案并复制进度，避免内部错误或可变指针进入响应。
func startupStatusForPhase(phase StartupPhase, progress *StartupProgress) StartupStatus {
	message := "Starting service"
	switch phase {
	case StartupPhaseMigrating:
		message = "Upgrading stored data"
	case StartupPhaseFailed:
		message = "Startup failed. Check server logs and retry."
	case StartupPhaseReady:
		message = "Ready"
	}
	result := StartupStatus{Phase: phase, Message: message}
	if progress != nil {
		value := StartupProgress{ProcessedCount: progress.ProcessedCount}
		if progress.TotalCount != nil {
			total := *progress.TotalCount
			value.TotalCount = &total
		}
		result.Progress = &value
	}
	return result
}
