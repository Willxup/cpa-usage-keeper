package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const errorEventBodySummaryMaxRunes = 2048

type errorEventsResponse struct {
	// Events 是当前游标页；完整状态快照不输出，body 中已精确删除当前 Identity 的真实 API Key。
	Events []errorEventPayload `json:"events"`
	// NextCursor 由末行 timestamp/id 编码；没有下一页时省略。
	NextCursor string `json:"next_cursor,omitempty"`
	// HasMore 明确告诉前端是否继续触发游标加载。
	HasMore bool `json:"has_more"`
}

type errorEventPayload struct {
	// ID 是 Keeper 本地事件主键的字符串形式，避免 JavaScript 大整数精度损失。
	ID string `json:"id"`
	// Timestamp 是 CPA 生成 Error Event 的时间，按 Keeper storage time 格式输出。
	Timestamp string `json:"timestamp"`
	// Provider 是 CPA result.provider 展示值；它不是凭证关联键。
	Provider string `json:"provider,omitempty"`
	// Model 是 CPA result.model；上游未提供时省略。
	Model string `json:"model,omitempty"`
	// StatusCode 是 CPA 错误 HTTP 状态码。
	StatusCode int `json:"status_code"`
	// BodySummary 是原始 body 删除当前 Identity API Key、清理控制字符并限制长度后的展示摘要。
	BodySummary string `json:"body_summary"`
	// BodyTruncated 表示 BodySummary 是否因字符上限被截断。
	BodyTruncated bool `json:"body_truncated"`
	// Code 是 CPA 结构化错误码；上游未提供时省略。
	Code string `json:"code,omitempty"`
	// Retryable 是错误发生时 CPA 的重试判断。
	Retryable bool `json:"retryable"`
	// CredentialRetryAfter 是错误发生时 CPA 给出的凭证级重试时间；完整状态快照仅保存在数据库。
	CredentialRetryAfter *string `json:"credential_retry_after,omitempty"`
	// ModelRetryAfter 是错误发生时 CPA 给出的模型级重试时间；前端无需理解完整 ModelState。
	ModelRetryAfter *string `json:"model_retry_after,omitempty"`
}

// registerErrorEventRoutes 将 Errors 详情接口放在 admin protected group；API Key Viewer 无权读取凭证错误。
func registerErrorEventRoutes(router gin.IRoutes, provider service.ErrorEventProvider) {
	router.GET("/usage/identities/:id/errors", func(c *gin.Context) {
		identityID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil || identityID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage identity id"})
			return
		}
		// 与 Request Log 一致，诊断文本只允许即时读取，不交给浏览器或中间缓存长期保存。
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusOK, errorEventsResponse{Events: []errorEventPayload{}})
			return
		}

		request := service.ErrorEventListRequest{IdentityID: identityID, PageSize: positiveQueryInt(c, "page_size", 50)}
		if rawCursor := strings.TrimSpace(c.Query("cursor")); rawCursor != "" {
			cursorTimestamp, cursorID, err := decodeUsageEventsCursor(rawCursor)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor"})
				return
			}
			request.Cursor = &repository.ErrorEventCursor{Timestamp: cursorTimestamp, ID: cursorID}
		}

		result, err := provider.ListErrorEvents(c.Request.Context(), request)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidID):
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage identity id"})
			case errors.Is(err, gorm.ErrRecordNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "usage identity not found"})
			default:
				writeInternalError(c, "list CPA error events failed", err)
			}
			return
		}

		events := make([]errorEventPayload, 0, len(result.Events))
		for _, event := range result.Events {
			events = append(events, mapErrorEventPayload(event, result.APIKey))
		}
		nextCursor := ""
		if result.HasMore && len(result.Events) > 0 {
			last := result.Events[len(result.Events)-1]
			nextCursor = encodeUsageEventsCursor(last.Timestamp, last.ID)
		}
		c.JSON(http.StatusOK, errorEventsResponse{Events: events, NextCursor: nextCursor, HasMore: result.HasMore})
	})
}

func mapErrorEventPayload(event entities.ErrorEvent, apiKey string) errorEventPayload {
	bodySummary, bodyTruncated := summarizeCPAErrorBody(event.Body, apiKey, errorEventBodySummaryMaxRunes)
	return errorEventPayload{
		ID:                   strconv.FormatInt(event.ID, 10),
		Timestamp:            timeutil.FormatStorageTime(event.Timestamp),
		Provider:             event.Provider,
		Model:                event.Model,
		StatusCode:           event.StatusCode,
		BodySummary:          bodySummary,
		BodyTruncated:        bodyTruncated,
		Code:                 event.Code,
		Retryable:            event.Retryable,
		CredentialRetryAfter: formatCPAErrorOptionalTime(event.AuthNextRetryAfter),
		ModelRetryAfter:      formatCPAErrorOptionalTime(event.AuthModelNextRetryAfter),
	}
}

func formatCPAErrorOptionalTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := timeutil.FormatStorageTime(*value)
	return &formatted
}

// summarizeCPAErrorBody 构造前端摘要：数据库保留 CPA 原文，API 只精确删除 Keeper 已知的真实 API Key。
// CPA errors 的 body 来自 result.Error.Message，不包含 auth_status；不要按字段名猜测并删除其它诊断内容。
func summarizeCPAErrorBody(value, apiKey string, maxRunes int) (string, bool) {
	cleaned := strings.Map(func(r rune) rune {
		// 列表不保留控制字符，统一为空格，避免日志换行或终端字符破坏卡片布局。
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)

	if apiKey = strings.TrimSpace(apiKey); apiKey != "" {
		cleaned = strings.ReplaceAll(cleaned, apiKey, "[REDACTED]")
	}

	runes := []rune(strings.TrimSpace(cleaned))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes), false
	}
	return string(runes[:maxRunes]), true
}
