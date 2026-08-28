package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const maxErrorEventPageSize = 100

// ErrorEventListRequest 是 Credential Errors API 的内部查询契约。
type ErrorEventListRequest struct {
	// IdentityID 是前端可见的 Keeper usage_identities 本地主键，不接受 CPA auth_index。
	IdentityID int64
	// Cursor 定位 timestamp/id 倒序列表的上一页末行；nil 表示从最新事件开始。
	Cursor *repository.ErrorEventCursor
	// PageSize 是期望返回条数；服务层会应用默认值并限制最大值。
	PageSize int
}

// ErrorEventListResponse 返回最终事件行和下一页判断；cursor 由 API 层编码，避免仓储依赖 HTTP 格式。
type ErrorEventListResponse struct {
	// Events 是目标 Identity 对应的原始数据库行；仅允许交给安全 API mapper。
	Events []entities.ErrorEvent
	// HasMore 表示仓储多取一行后确认仍有下一页。
	HasMore bool
	// APIKey 是 AI Provider Identity 的真实 lookup key，仅供 API 从 body 摘要中精确删除；Auth File 为空。
	APIKey string
}

// ErrorEventProvider 同时服务后台直接写入和管理员详情读取。
type ErrorEventProvider interface {
	StoreErrorEvent(context.Context, string, time.Time) error
	ListErrorEvents(context.Context, ErrorEventListRequest) (ErrorEventListResponse, error)
}

type errorEventService struct {
	// db 既用于订阅直接写入，也用于详情页按 Identity 查询；两条路径不经过 Usage Inbox。
	db *gorm.DB
}

// NewErrorEventService 构造 Errors 专用服务；构造阶段不联网，也不执行写入。
func NewErrorEventService(db *gorm.DB) ErrorEventProvider {
	return &errorEventService{db: db}
}

// StoreErrorEvent 将一条有效 CPA payload 完整映射到扁平列后直接写入最终表。
func (s *errorEventService) StoreErrorEvent(ctx context.Context, raw string, receivedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("CPA error event database is nil")
	}
	event, err := decodeCPAErrorEvent(raw, receivedAt)
	if err != nil {
		return err
	}
	return repository.InsertErrorEvent(ctx, s.db, &event)
}

// ListErrorEvents 先用 Keeper Identity ID 解析稳定 auth_index，再查询对应错误历史。
func (s *errorEventService) ListErrorEvents(ctx context.Context, request ErrorEventListRequest) (ErrorEventListResponse, error) {
	if s == nil || s.db == nil {
		return ErrorEventListResponse{}, fmt.Errorf("CPA error event database is nil")
	}
	if request.IdentityID <= 0 {
		return ErrorEventListResponse{}, ErrInvalidID
	}
	identity, err := repository.FindUsageIdentityByID(ctx, s.db, request.IdentityID)
	if err != nil {
		return ErrorEventListResponse{}, err
	}
	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = 50
	} else if pageSize > maxErrorEventPageSize {
		pageSize = maxErrorEventPageSize
	}
	page, err := repository.ListErrorEventsByAuthIndex(ctx, s.db, identity.Identity, request.Cursor, pageSize)
	if err != nil {
		return ErrorEventListResponse{}, err
	}
	return ErrorEventListResponse{
		Events:  page.Events,
		HasMore: page.HasMore,
		APIKey:  identity.LookupKey,
	}, nil
}

// cpaErrorEventPayload 镜像 CPA 当前 errors channel 顶层 JSON 契约；未知字段保持向前兼容并被忽略。
type cpaErrorEventPayload struct {
	// Timestamp 来自 CPA errorEvent.timestamp，是上游生成事件的时间，不是 Keeper 接收时间。
	Timestamp time.Time `json:"timestamp"`
	// Provider 来自 CPA result.provider；omitempty 只影响上游序列化，缺失时解码为空字符串。
	Provider string `json:"provider"`
	// Model 来自 CPA result.model；上游未识别模型时可能缺失。
	Model string `json:"model"`
	// AuthID 来自 CPA result.auth_id，是运行时内部标识，只落库排障，不对前端输出。
	AuthID string `json:"auth_id"`
	// AuthIndex 来自 CPA auth snapshot.index，是 Keeper 关联 usage_identities.identity 的稳定键。
	AuthIndex string `json:"auth_index"`
	// StatusCode 来自 CPA Error.HTTPStatus；上游无法取得 HTTP 状态时按当前契约写 500。
	StatusCode int `json:"status_code"`
	// Body 来自 CPA Error.Message 的最终回退值，是原始自由文本；API 输出时只精确删除 Identity lookup key。
	Body string `json:"body"`
	// Code 来自 CPA Error.Code；上游没有结构化错误码时为空字符串。
	Code string `json:"code"`
	// Retryable 来自 CPA Error.Retryable，描述错误发生时 CPA 的重试判断。
	Retryable bool `json:"retryable"`
	// AuthStatus 是 MarkResult 更新完成后的凭证状态快照，不代表 Keeper 查询时的当前状态。
	AuthStatus cpaErrorEventAuthSnapshot `json:"auth_status"`
}

// cpaErrorEventAuthSnapshot 镜像错误发生时的凭证状态；它不是当前 Credential Health。
type cpaErrorEventAuthSnapshot struct {
	// Status 是 CPA auth.Status 的字符串值，如 active/error/disabled。
	Status string `json:"status"`
	// StatusMessage 是 CPA 凭证状态说明自由文本，仅完整落库供排障，当前详情 API 不输出。
	StatusMessage string `json:"status_message"`
	// Disabled 表示错误发生时凭证是否被用户或配置显式禁用。
	Disabled bool `json:"disabled"`
	// Unavailable 表示错误发生时凭证是否正处于临时不可用或冷却状态。
	Unavailable bool `json:"unavailable"`
	// NextRetryAfter 是凭证级下一次允许重试时间；nil 表示上游未设置。
	NextRetryAfter *time.Time `json:"next_retry_after"`
	// Quota 是凭证级限额快照；nil 明确表示 CPA 当时没有可报告的限额状态。
	Quota *cpaErrorEventQuotaSnapshot `json:"quota"`
	// Model 是本次请求模型的独立状态快照；nil 表示 CPA 没有对应 ModelState。
	Model *cpaErrorEventModelSnapshot `json:"model"`
}

// cpaErrorEventQuotaSnapshot 同时用于凭证级和模型级 quota，外层指针保留对象是否存在。
type cpaErrorEventQuotaSnapshot struct {
	// Exceeded 表示错误发生时此级别的 quota 是否已超限。
	Exceeded bool `json:"exceeded"`
	// Reason 是 CPA 给出的限额原因自由文本，仅完整落库供排障，当前详情 API 不输出 quota 快照。
	Reason string `json:"reason"`
	// NextRecoverAt 是 CPA 预计限额恢复时间；nil 表示未提供恢复时间。
	NextRecoverAt *time.Time `json:"next_recover_at"`
	// BackoffLevel 是 CPA 的退避级别；对象存在时零值也有意义并必须保存。
	BackoffLevel int `json:"backoff_level"`
}

// cpaErrorEventModelSnapshot 保存本次错误模型在 CPA auth_status 中的状态快照。
type cpaErrorEventModelSnapshot struct {
	// Name 是 CPA 用于查找 ModelState 的模型名，通常等于顶层 model。
	Name string `json:"name"`
	// Status 是错误发生时该模型的 CPA 状态字符串。
	Status string `json:"status"`
	// StatusMessage 是模型级状态说明自由文本，仅完整落库供排障，当前详情 API 不输出。
	StatusMessage string `json:"status_message"`
	// Unavailable 表示该模型在此凭证上是否暂时不可用。
	Unavailable bool `json:"unavailable"`
	// NextRetryAfter 是模型级下一次允许重试时间；nil 表示上游未设置。
	NextRetryAfter *time.Time `json:"next_retry_after"`
	// Quota 是模型级限额快照；nil 表示 CPA 当时没有可报告的模型限额状态。
	Quota *cpaErrorEventQuotaSnapshot `json:"quota"`
}

func decodeCPAErrorEvent(raw string, receivedAt time.Time) (entities.ErrorEvent, error) {
	var payload cpaErrorEventPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return entities.ErrorEvent{}, fmt.Errorf("decode CPA error event: %w", err)
	}
	if payload.Timestamp.IsZero() {
		return entities.ErrorEvent{}, fmt.Errorf("decode CPA error event: timestamp is required")
	}
	payload.AuthIndex = strings.TrimSpace(payload.AuthIndex)
	if payload.AuthIndex == "" {
		return entities.ErrorEvent{}, fmt.Errorf("decode CPA error event: auth_index is required")
	}

	// 顶层字段逐一映射，保证当前 CPA contract 的每个字段都有明确数据库列。
	event := entities.ErrorEvent{
		Timestamp:          timeutil.NormalizeStorageTime(payload.Timestamp),
		ReceivedAt:         timeutil.NormalizeStorageTime(receivedAt),
		Provider:           payload.Provider,
		Model:              payload.Model,
		AuthID:             payload.AuthID,
		AuthIndex:          payload.AuthIndex,
		StatusCode:         payload.StatusCode,
		Body:               payload.Body,
		Code:               payload.Code,
		Retryable:          payload.Retryable,
		AuthStatus:         payload.AuthStatus.Status,
		AuthStatusMessage:  payload.AuthStatus.StatusMessage,
		AuthDisabled:       payload.AuthStatus.Disabled,
		AuthUnavailable:    payload.AuthStatus.Unavailable,
		AuthNextRetryAfter: normalizeOptionalStorageTime(payload.AuthStatus.NextRetryAfter),
	}
	mapCPAErrorQuotaSnapshot(payload.AuthStatus.Quota, &event.AuthQuotaExceeded, &event.AuthQuotaReason, &event.AuthQuotaNextRecoverAt, &event.AuthQuotaBackoffLevel)
	if model := payload.AuthStatus.Model; model != nil {
		// 模型对象存在时全部列都写非 nil 指针，包括 false/0/空字符串，保留对象存在语义。
		event.AuthModelName = stringPointer(model.Name)
		event.AuthModelStatus = stringPointer(model.Status)
		event.AuthModelStatusMessage = stringPointer(model.StatusMessage)
		event.AuthModelUnavailable = boolPointer(model.Unavailable)
		event.AuthModelNextRetryAfter = normalizeOptionalStorageTime(model.NextRetryAfter)
		mapCPAErrorQuotaSnapshot(model.Quota, &event.AuthModelQuotaExceeded, &event.AuthModelQuotaReason, &event.AuthModelQuotaNextRecoverAt, &event.AuthModelQuotaBackoffLevel)
	}
	return event, nil
}

func mapCPAErrorQuotaSnapshot(snapshot *cpaErrorEventQuotaSnapshot, exceeded **bool, reason **string, nextRecoverAt **time.Time, backoffLevel **int) {
	if snapshot == nil {
		return
	}
	*exceeded = boolPointer(snapshot.Exceeded)
	*reason = stringPointer(snapshot.Reason)
	*nextRecoverAt = normalizeOptionalStorageTime(snapshot.NextRecoverAt)
	*backoffLevel = intPointer(snapshot.BackoffLevel)
}

func normalizeOptionalStorageTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := timeutil.NormalizeStorageTime(*value)
	return &normalized
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func intPointer(value int) *int          { return &value }
