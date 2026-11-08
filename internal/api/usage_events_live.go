package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

const (
	liveBatchSize          = 100
	liveReplayLimit        = 10000
	liveWriteTimeout       = 5 * time.Second
	liveHeartbeatInterval  = 15 * time.Second
	liveRetryInterval      = 2 * time.Second
	liveReplayTimeout      = 30 * time.Second
	liveGlobalConnections  = 64
	liveSessionConnections = 3
)

var errLiveBusy = errors.New("costs busy")

type usageEventsLiveCursor struct {
	Version int       `json:"v"`
	ID      int64     `json:"id"`
	Anchor  time.Time `json:"anchor"`
	Scope   string    `json:"scope"`
}

type usageEventsLiveBounds struct {
	RangeStart   string `json:"range_start"`
	RangeEnd     string `json:"range_end"`
	EndExclusive bool   `json:"end_exclusive"`
}

type usageEventsLiveSlots struct {
	mu        sync.Mutex
	total     int
	bySession map[string]int
}

func (s *usageEventsLiveSlots) acquire(key string) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= liveGlobalConnections || s.bySession[key] >= liveSessionConnections {
		return nil, false
	}
	s.total++
	s.bySession[key]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.total--
		s.bySession[key]--
		if s.bySession[key] == 0 {
			delete(s.bySession, key)
		}
	}, true
}

func usageEventsLiveScope(values url.Values) string {
	canonical := url.Values{}
	for _, key := range []string{"range", "unit", "start", "end", "api_key_id", "model", "source", "auth_index", "auth_type", "result"} {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			canonical.Set(key, value)
		}
	}
	hash := sha256.Sum256([]byte(canonical.Encode()))
	return hex.EncodeToString(hash[:])
}

func encodeUsageEventsLiveCursor(cursor usageEventsLiveCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeUsageEventsLiveCursor(value, scope string, now time.Time) (usageEventsLiveCursor, bool) {
	var cursor usageEventsLiveCursor
	if len(value) > 1024 {
		return cursor, false
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.ID < 0 || cursor.Scope != scope || cursor.Anchor.IsZero() || cursor.Anchor.After(now.Add(time.Minute)) || cursor.Anchor.Before(now.Add(-90*24*time.Hour)) {
		return cursor, false
	}
	return cursor, true
}

func liveBounds(filter servicedto.UsageFilter) usageEventsLiveBounds {
	return usageEventsLiveBounds{RangeStart: timeutil.FormatStorageTime(*filter.StartTime), RangeEnd: timeutil.FormatStorageTime(*filter.EndTime), EndExclusive: filter.EndExclusive}
}

func liveFilter(c *gin.Context, anchor time.Time) (servicedto.UsageFilter, error) {
	filter, err := parseUsageFilterQuery(c.Request, anchor)
	if err != nil {
		return filter, err
	}
	if err = applyUsageEventsSourceFilter(&filter); err != nil {
		return filter, err
	}
	filter.CursorMode = true
	filter.CursorTimestamp = nil
	filter.CursorID = 0
	filter.Page = 1
	filter.Offset = 0
	return filter, nil
}

// liveSessionValid 不 Touch 会话，防止长连接自动延长或复活已撤销的管理员会话。
func liveSessionValid(c *gin.Context, handler *authHandler) bool {
	if handler == nil || !handler.config.Enabled {
		return true
	}
	value, ok := c.Get(authResolvedContextKey)
	resolved, valid := value.(resolvedSessionToken)
	if !ok || !valid || handler.sessions == nil {
		return false
	}
	session, valid := handler.sessions.Get(resolved.Token)
	return valid && session.Role == auth.RoleAdmin && sessionMatchesResolvedSource(session, resolved)
}

func registerUsageEventsLiveRoute(router gin.IRoutes, usage service.UsageProvider, identities service.UsageIdentityProvider, keys service.CPAAPIKeyProvider, notifier *service.UsageEventsNotifier, gate *service.CostReadGate, handler *authHandler) {
	slots := &usageEventsLiveSlots{bySession: make(map[string]int)}
	router.GET("/usage/events/live", func(c *gin.Context) {
		provider, ok := usage.(service.UsageEventsLiveProvider)
		if !ok || notifier == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "live_unavailable"})
			return
		}
		now := timeutil.NormalizeStorageTime(time.Now())
		filter, err := liveFilter(c, now)
		if err != nil {
			writeUsageFilterParseError(c, err)
			return
		}
		if filter.StartTime == nil || filter.EndTime == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "usage range is required"})
			return
		}
		sessionKey := c.GetString(authTokenContextKey)
		if sessionKey == "" {
			sessionKey = "anonymous:" + c.ClientIP()
		}
		releaseSlot, ok := slots.acquire(sessionKey)
		if !ok {
			c.Header("Retry-After", "5")
			c.JSON(http.StatusTooManyRequests, gin.H{"code": "live_capacity"})
			return
		}
		defer releaseSlot()
		// 先订阅再读取上界；连接内只传唤醒，数据库是唯一数据来源。
		wake, unsubscribe := notifier.Subscribe()
		defer unsubscribe()
		originalRequest := c.Request
		metadataCtx, cancelMetadata := context.WithTimeout(c.Request.Context(), liveWriteTimeout)
		c.Request = c.Request.WithContext(metadataCtx)
		resolution, err := loadUsageResolutionData(c, identities)
		if err != nil {
			cancelMetadata()
			c.Request = originalRequest
			writeInternalError(c, "load usage resolution data failed", err)
			return
		}
		resolver := newUsageIdentityResolver(resolution)
		keyInfos, err := loadCPAAPIKeyInfos(c, keys)
		cancelMetadata()
		c.Request = originalRequest
		if err != nil {
			return
		}
		stream := &usageEventsLiveStream{c: c, provider: provider, gate: gate, auth: handler, resolver: resolver, keyInfos: keyInfos}
		scope := usageEventsLiveScope(c.Request.URL.Query())
		cursor := usageEventsLiveCursor{Version: 1, Anchor: now, Scope: scope}
		after := strings.TrimSpace(c.Query("after"))
		if after != "" {
			var valid bool
			cursor, valid = decodeUsageEventsLiveCursor(after, scope, now)
			if !valid {
				stream.open()
				_ = stream.send("reset", "", gin.H{"code": "invalid_cursor"})
				return
			}
		} else {
			var page *servicedto.UsageEventsPage
			err = stream.batch(func(ctx context.Context) error {
				var high int64
				page, high, err = provider.LiveSnapshot(ctx, filter)
				if err != nil {
					return err
				}
				cursor.ID = high
				next := ""
				if page.HasMore && len(page.Events) > 0 {
					last := page.Events[len(page.Events)-1]
					next = encodeUsageEventsCursor(last.Timestamp, last.ID)
				}
				stream.open()
				return stream.send("snapshot", encodeUsageEventsLiveCursor(cursor), gin.H{
					"events": buildUsageEventsPayload(page.Events, resolver, keyInfos), "total_count": page.TotalCount,
					"page": page.Page, "page_size": page.PageSize, "total_pages": page.TotalPages,
					"next_cursor": next, "has_more": page.HasMore, "range_start": liveBounds(filter).RangeStart,
					"range_end": liveBounds(filter).RangeEnd, "end_exclusive": filter.EndExclusive, "snapshot_at": timeutil.FormatStorageTime(now),
				})
			})
			if err != nil {
				if errors.Is(err, errLiveBusy) {
					c.JSON(http.StatusServiceUnavailable, gin.H{"code": "costs_busy", "message": "Cost statistics are being updated. Please try again shortly."})
				} else if !c.Writer.Written() {
					writeInternalError(c, "load live usage snapshot failed", err)
				}
				return
			}
		}
		stream.open()
		stream.run(cursor, wake)
	})
}

type usageEventsLiveStream struct {
	c        *gin.Context
	provider service.UsageEventsLiveProvider
	gate     *service.CostReadGate
	auth     *authHandler
	resolver usageIdentityResolver
	keyInfos map[string]analysisAPIKeyInfo
	opened   bool
}

func (s *usageEventsLiveStream) open() {
	if s.opened {
		return
	}
	s.c.Header("Content-Type", "text/event-stream; charset=utf-8")
	s.c.Header("Cache-Control", "no-cache, no-transform")
	s.c.Header("X-Accel-Buffering", "no")
	s.opened = true
}

func (s *usageEventsLiveStream) send(event, id string, payload any) error {
	if !liveSessionValid(s.c, s.auth) {
		return context.Canceled
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	controller := http.NewResponseController(s.c.Writer)
	if err := controller.SetWriteDeadline(time.Now().Add(liveWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	defer controller.SetWriteDeadline(time.Time{})
	if id != "" {
		if _, err = fmt.Fprintf(s.c.Writer, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err = fmt.Fprintf(s.c.Writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	return controller.Flush()
}

// batch 的许可覆盖查询、编码和限时写入，不能延续到心跳或通知等待。
func (s *usageEventsLiveStream) batch(fn func(context.Context) error) error {
	if !liveSessionValid(s.c, s.auth) {
		return context.Canceled
	}
	release, ok := s.gate.Acquire()
	if !ok {
		return errLiveBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(s.c.Request.Context(), liveWriteTimeout)
	defer cancel()
	return fn(ctx)
}

func (s *usageEventsLiveStream) run(cursor usageEventsLiveCursor, wake <-chan struct{}) {
	heartbeat := time.NewTicker(liveHeartbeatInterval)
	defer heartbeat.Stop()
	retry := time.NewTicker(liveRetryInterval)
	defer retry.Stop()
	busy := false
	for {
		// 每轮先排空唤醒后查询目标，覆盖快照期间和上次查询期间的并发提交。
		select {
		case <-wake:
		default:
		}
		err := s.catchUp(&cursor)
		if err != nil {
			if !errors.Is(err, errLiveBusy) {
				return
			}
			if !busy {
				if s.send("busy", "", gin.H{"code": "costs_busy", "retry_ms": 2000}) != nil {
					return
				}
				busy = true
			}
		} else {
			if busy {
				filter, filterErr := liveFilter(s.c, timeutil.NormalizeStorageTime(time.Now()))
				if filterErr != nil || s.send("heartbeat", "", liveBounds(filter)) != nil {
					return
				}
			}
			busy = false
		}
		var retryChannel <-chan time.Time
		var wakeChannel <-chan struct{} = wake
		if busy {
			retryChannel = retry.C
			wakeChannel = nil
		}
		select {
		case <-s.c.Request.Context().Done():
			return
		case <-wakeChannel:
		case <-retryChannel:
		case <-heartbeat.C:
			filter, err := liveFilter(s.c, timeutil.NormalizeStorageTime(time.Now()))
			if err != nil {
				_ = s.send("reset", "", gin.H{"code": "range_changed"})
				return
			}
			bounds := liveBounds(filter)
			payload := gin.H{"range_start": bounds.RangeStart, "range_end": bounds.RangeEnd, "end_exclusive": bounds.EndExclusive, "busy": busy}
			// 总数只在既有心跳节奏下查询，许可不跨越下一次通知等待。
			err = s.batch(func(ctx context.Context) error {
				total, countErr := s.provider.LiveCount(ctx, filter)
				if countErr != nil {
					return countErr
				}
				payload["total_count"] = total
				return s.send("heartbeat", "", payload)
			})
			if errors.Is(err, errLiveBusy) {
				payload["busy"] = true
				err = s.send("heartbeat", "", payload)
			}
			if err != nil {
				return
			}
		}
	}
}

func (s *usageEventsLiveStream) catchUp(cursor *usageEventsLiveCursor) error {
	started := time.Now()
	now := timeutil.NormalizeStorageTime(started)
	current, err := liveFilter(s.c, now)
	if err != nil {
		_ = s.send("reset", "", gin.H{"code": "range_changed"})
		return err
	}
	old, err := liveFilter(s.c, cursor.Anchor)
	if err != nil {
		_ = s.send("reset", "", gin.H{"code": "range_changed"})
		return err
	}
	if (current.Range == "today" || current.Range == "yesterday") && !current.StartTime.Equal(*old.StartTime) {
		_ = s.send("reset", "", gin.H{"code": "range_changed"})
		return context.Canceled
	}
	var high int64
	if err = s.batch(func(ctx context.Context) error { var e error; high, e = s.provider.LiveHighID(ctx); return e }); err != nil {
		return err
	}
	if cursor.ID > high {
		_ = s.send("reset", "", gin.H{"code": "invalid_cursor"})
		return context.Canceled
	}
	scanned := 0
	for cursor.ID < high {
		if scanned >= liveReplayLimit || time.Since(started) > liveReplayTimeout {
			_ = s.send("reset", "", gin.H{"code": "replay_limit"})
			return context.Canceled
		}
		err = s.batch(func(ctx context.Context) error {
			rows, next, count, e := s.provider.LiveIDBatch(ctx, old, cursor.ID, liveBatchSize)
			if e != nil {
				return e
			}
			if count == 0 {
				return fmt.Errorf("live high id changed")
			}
			scanned += count
			nextCursor := *cursor
			nextCursor.ID = next
			kind := "checkpoint"
			payload := gin.H{"range_start": liveBounds(old).RangeStart, "range_end": liveBounds(old).RangeEnd, "end_exclusive": old.EndExclusive}
			if len(rows) > 0 {
				kind = "append"
				payload["events"] = buildUsageEventsPayload(rows, s.resolver, s.keyInfos)
			}
			if e = s.send(kind, encodeUsageEventsLiveCursor(nextCursor), payload); e != nil {
				return e
			}
			*cursor = nextCursor
			return nil
		})
		if err != nil {
			return err
		}
	}
	// 滚动范围移动时补查已存在但此前位于未来的事件；直到整个窄窗完成才确认新锚点。
	if _, rolling := timeutil.ParseUsageRollingRange(current.Range); rolling && current.EndTime.After(*old.EndTime) {
		var lastTime *time.Time
		var lastID int64
		for {
			if scanned >= liveReplayLimit || time.Since(started) > liveReplayTimeout {
				_ = s.send("reset", "", gin.H{"code": "replay_limit"})
				return context.Canceled
			}
			var count int
			err = s.batch(func(ctx context.Context) error {
				rows, nextTime, nextID, readCount, e := s.provider.LiveTimeBatch(ctx, current, cursor.ID, *old.EndTime, *current.EndTime, lastTime, lastID, liveBatchSize)
				if e != nil {
					return e
				}
				count = readCount
				scanned += count
				if len(rows) > 0 {
					if e = s.send("append", encodeUsageEventsLiveCursor(*cursor), gin.H{"events": buildUsageEventsPayload(rows, s.resolver, s.keyInfos), "range_start": liveBounds(current).RangeStart, "range_end": liveBounds(current).RangeEnd, "end_exclusive": current.EndExclusive}); e != nil {
						return e
					}
				}
				lastTime, lastID = nextTime, nextID
				return nil
			})
			if err != nil {
				return err
			}
			if count < liveBatchSize {
				break
			}
		}
		next := *cursor
		next.Anchor = now
		err = s.batch(func(context.Context) error {
			return s.send("checkpoint", encodeUsageEventsLiveCursor(next), liveBounds(current))
		})
		if err != nil {
			return err
		}
		*cursor = next
	}
	return nil
}
