package repository

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const (
	// 默认保留 70 分钟，覆盖 realtime 最大 60 分钟窗口和 Overview 当前右边界补偿余量。
	usageRecentEventCacheDefaultWindow = 70 * time.Minute
	// 队列按写入批次计数，100 个槽位可承接短时突发，又避免无界缓存批次拖垮内存。
	usageRecentEventCacheDefaultQueueSize = 100
)

// RecentUsageIdentityKind 是最近事件缓存里的身份 fallback 类型。
type RecentUsageIdentityKind uint8

const (
	// RecentUsageIdentityUnknown 表示缓存事件没有可用于 current usage 的身份 fallback。
	RecentUsageIdentityUnknown RecentUsageIdentityKind = iota
	// RecentUsageIdentityAuthFile 表示 auth_type=oauth，fallback label 使用 source。
	RecentUsageIdentityAuthFile
	// RecentUsageIdentityAIProvider 表示 auth_type=apikey，fallback label 使用 provider。
	RecentUsageIdentityAIProvider
)

// RecentUsageEvent 是 Overview 边界补偿和 realtime 共用的最近事件最小投影。
type RecentUsageEvent struct {
	// Timestamp 是事件时间，所有入缓存路径都会先归一化到项目配置时区。
	Timestamp time.Time
	// APIGroupKey 保留 Overview / KeyOverview 的 API Key 作用域过滤条件。
	APIGroupKey string
	// Model 用于 realtime 当前模型占比和 cost 价格表匹配。
	Model string
	// ModelAlias 用于当前价格配置按 alias 优先匹配。
	ModelAlias string
	// AuthIndex 用于关联 usage_identities，找不到身份时才使用 fallback。
	AuthIndex string
	// IdentityFallbackKind 记录 fallback 应落到 Auth File 还是 AI Provider。
	IdentityFallbackKind RecentUsageIdentityKind
	// IdentityFallbackLabel 保存 source/provider 展示名，避免 realtime 再读 usage_events。
	IdentityFallbackLabel string
	// Failed 保留请求成功状态，realtime 请求水平统计需要成功和失败总量。
	Failed bool
	// LatencyMS 保留响应耗时样本，用于 Response Level 滑动聚合。
	LatencyMS int64
	// TTFTMS 保留可空首 token 延迟样本，用指针区分缺失和 0。
	TTFTMS *int64
	// InputTokens 以下 token 字段覆盖 Overview 边界补偿和所有 realtime token 指标。
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

// UsageRecentEventCacheOptions 控制最近事件缓存的时钟、窗口和投递队列大小。
type UsageRecentEventCacheOptions struct {
	// Now 允许测试固定时钟；生产默认 time.Now 并统一走 timeutil.NormalizeStorageTime。
	Now func() time.Time
	// Window 控制缓存保留时长；为空时使用 70 分钟默认值。
	Window time.Duration
	// QueueSize 控制异步投递缓冲；为空时使用小队列默认值。
	QueueSize int
}

// UsageRecentEventCache 保存最近窗口内 usage_events 的最小投影。
type UsageRecentEventCache struct {
	// mu 保护 events 和字符串池；读路径可并发，追加/剪枝独占。
	mu sync.RWMutex
	// events 按追加顺序保存最近 usage event 的瘦身投影。
	events []RecentUsageEvent
	// pool 复用重复的 model/api/auth 字符串，降低 10w 级事件缓存内存。
	pool recentUsageStringPool
	// credentialHealth 保存 Auth Files / AI Provider 最近 5h 的成功/失败 10 分钟桶。
	credentialHealth credentialHealthCache
	// window 是缓存保留时长，只在创建时确定。
	window time.Duration
	// now 只用于启动加载和剪枝，不参与 Overview 边界是否 fallback 的判断。
	now func() time.Time

	// appendCh 接收事务提交后的非阻塞增量事件投递。
	appendCh chan []entities.UsageEvent
	// appendSlots 在复制事件前预留队列槽位，队列满时直接丢弃，避免满队列还复制整批事件。
	appendSlots chan struct{}
	// stopCh 通知 worker 退出。
	stopCh chan struct{}
	// doneCh 在 worker 完全退出后关闭，Close 用它等待资源释放。
	doneCh chan struct{}
	// closeOnce 保证 stopCh 只关闭一次，避免并发 Close 重复 close channel。
	closeOnce sync.Once
}

type recentUsageEventLoadRow struct {
	// 这个结构只列出缓存真正需要的列，避免启动加载把 usage_events 大字段读进内存。
	APIGroupKey         string
	Provider            string
	AuthType            string
	Model               string
	ModelAlias          string `gorm:"column:model_alias"`
	Timestamp           time.Time
	Source              string
	AuthIndex           string
	Failed              bool
	LatencyMS           int64
	TTFTMS              *int64 `gorm:"column:ttft_ms"`
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

type recentUsageStringPool struct {
	// values 以字符串内容为 key，保存当前缓存窗口内仍被引用的唯一字符串。
	values map[string]*recentUsageInternedString
}

type recentUsageInternedString struct {
	// value 是 strings.Clone 后由池持有的稳定字符串。
	value string
	// refs 记录当前缓存事件里还有多少字段引用该字符串。
	refs int
}

// NewUsageRecentEventCache 从数据库加载最近窗口投影，并启动异步增量写入 worker。
func NewUsageRecentEventCache(db *gorm.DB, opts UsageRecentEventCacheOptions) (*UsageRecentEventCache, error) {
	// 先创建空缓存和 worker；如果后续初始化失败，会 Close 掉避免 goroutine 泄漏。
	cache := newEmptyUsageRecentEventCache(opts)
	// DB 不存在属于缓存创建失败，上层可以选择不注入缓存并走 DB 旧路径。
	if db == nil {
		cache.Close()
		return nil, fmt.Errorf("database is nil")
	}
	// 启动加载基于缓存自己的时钟，仅用于确定“最近 70 分钟”初始窗口。
	now := timeutil.NormalizeStorageTime(cache.now())
	// start 是初始化查询左边界，窗口之外的历史事件不进入纯内存缓存。
	start := now.Add(-cache.window)
	// 只读取 Overview/realtime 需要的列，避免 event_key/request_id 等无关字段占内存。
	rows, err := loadUsageRecentEventCacheRows(db, start)
	if err != nil {
		// 初始化失败表示缓存对象不可用，必须关闭 worker 并把错误交给调用方。
		cache.Close()
		return nil, err
	}
	// 初始化写入 events 和字符串池需要独占锁，避免 worker 同时追加。
	cache.mu.Lock()
	// 将 DB row 转成缓存投影，同时做字符串池复用和身份 fallback 预计算。
	cache.appendRecentEventsLocked(rows)
	// 再剪一次窗口，防止初始化查询和当前时间之间有极小漂移。
	cache.pruneLocked(now)
	// 初始化完成后释放锁，后续读写正常并发。
	cache.mu.Unlock()
	// 健康图保留 5h，可能覆盖高流量账号的百万级请求；按批流式加载，避免启动峰值内存跟行数线性增长。
	if err := loadCredentialHealthCacheRowsBatched(db, now.Add(-credentialHealthWindow), credentialHealthStartupBatchSize, func(rows []credentialHealthLoadRow) error {
		cache.mu.Lock()
		cache.appendCredentialHealthRowsLocked(rows)
		cache.mu.Unlock()
		return nil
	}); err != nil {
		cache.Close()
		return nil, err
	}
	cache.mu.Lock()
	cache.pruneCredentialHealthLocked(now, nil)
	cache.mu.Unlock()
	return cache, nil
}

// newEmptyUsageRecentEventCache 创建空缓存，供启动加载和增量追加共用。
func newEmptyUsageRecentEventCache(opts UsageRecentEventCacheOptions) *UsageRecentEventCache {
	// 未传 Now 时使用真实时钟，生产路径再由 timeutil 统一到项目时区。
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	// 未配置或配置非法时回到 70 分钟默认窗口。
	window := opts.Window
	if window <= 0 {
		window = usageRecentEventCacheDefaultWindow
	}
	// 未配置或配置非法时回到默认批次队列宽度，保持写入链路轻量且能吸收短时突发。
	queueSize := opts.QueueSize
	if queueSize <= 0 {
		queueSize = usageRecentEventCacheDefaultQueueSize
	}
	// 初始化空事件切片和字符串池，避免首次 append/read 遇到 nil map。
	cache := &UsageRecentEventCache{
		events: []RecentUsageEvent{},
		pool:   recentUsageStringPool{values: map[string]*recentUsageInternedString{}},
		credentialHealth: credentialHealthCache{
			bucketsByCredential: map[credentialHealthKey]map[int64]credentialHealthBucketCounts{},
		},
		window:      window,
		now:         now,
		appendCh:    make(chan []entities.UsageEvent, queueSize),
		appendSlots: make(chan struct{}, queueSize),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	for index := 0; index < queueSize; index++ {
		// 每个 token 代表一个可用队列槽位，TryAppend 必须先拿 token 再复制事件。
		cache.appendSlots <- struct{}{}
	}
	// worker 专门处理事务提交后的异步追加，调用方不等待缓存更新完成。
	go cache.run()
	return cache
}

// Close 停止最近事件缓存 worker。
func (c *UsageRecentEventCache) Close() {
	// nil cache 允许上层 defer 调用，简化错误路径。
	if c == nil {
		return
	}
	// closeOnce 保证并发 Close 只有一个调用者关闭 stopCh。
	c.closeOnce.Do(func() {
		close(c.stopCh)
	})
	// 所有 Close 调用都等待 worker 退出，避免调用方看到半关闭状态。
	<-c.doneCh
}

func (c *UsageRecentEventCache) run() {
	// worker 退出时关闭 doneCh，通知 Close 可以返回。
	defer close(c.doneCh)
	for {
		select {
		case events := <-c.appendCh:
			// worker 已经取出一个批次，立即释放队列槽位，让后续写入可继续投递。
			c.releaseAppendSlot()
			// 复用同步追加路径，保证测试追加和异步追加的剪枝/池化语义一致。
			c.appendEvents(events)
		case <-c.stopCh:
			// 收到停止信号后直接退出；队列里未处理的事件不再阻塞关闭。
			return
		}
	}
}

// TryAppend 非阻塞投递新增事件。队列满时只丢弃本次投递信号，不阻塞写入链路。
func (c *UsageRecentEventCache) TryAppend(events []entities.UsageEvent) bool {
	// 没有缓存或没有事件时视为成功，调用方无需为 nil cache 写特殊分支。
	if c == nil || len(events) == 0 {
		return true
	}
	if !c.acquireAppendSlot() {
		// 队列满只说明这批增量没有进入缓存，不能把整个缓存标记为坏。
		return false
	}
	clonedEvents := cloneUsageEventsForRecentCache(events)
	select {
	case c.appendCh <- clonedEvents:
		// 投递成功即可返回，真正写入缓存由 worker 异步完成。
		return true
	case <-c.stopCh:
		// 关闭过程中放弃投递，并归还刚才预留的槽位。
		c.releaseAppendSlot()
		return false
	default:
		// 理论上拿到槽位后 appendCh 应可写；保留防御分支，避免异常状态阻塞写入链路。
		c.releaseAppendSlot()
		return false
	}
}

func (c *UsageRecentEventCache) acquireAppendSlot() bool {
	// 先检查停止信号，避免关闭后的投递继续占用槽位。
	select {
	case <-c.stopCh:
		return false
	default:
	}
	select {
	case <-c.appendSlots:
		return true
	default:
		return false
	}
}

func (c *UsageRecentEventCache) releaseAppendSlot() {
	select {
	case c.appendSlots <- struct{}{}:
	default:
		// 槽位已满说明释放重复发生，忽略即可保持 Close/测试路径幂等。
	}
}

// appendEvents 同步写入缓存事件，启动加载和异步 worker 都复用这条瘦身/池化路径。
func (c *UsageRecentEventCache) appendEvents(events []entities.UsageEvent) {
	// nil cache 或空事件直接返回，保持测试和异步 worker 路径都可安全调用。
	if c == nil || len(events) == 0 {
		return
	}
	// 先把实体转成和 DB 启动加载一致的 row，后续只维护一套投影转换逻辑。
	rows := make([]recentUsageEventLoadRow, 0, len(events))
	for _, event := range events {
		// 这里刻意不带 event_key/request_id，它们不参与 Overview/realtime 计算。
		rows = append(rows, recentUsageEventLoadRow{
			APIGroupKey: event.APIGroupKey,
			Provider:    event.Provider,
			AuthType:    event.AuthType,
			Model:       event.Model,
			ModelAlias: func() string {
				if event.ModelAlias == nil {
					return ""
				}
				return *event.ModelAlias
			}(),
			Timestamp:           event.Timestamp,
			Source:              event.Source,
			AuthIndex:           event.AuthIndex,
			Failed:              event.Failed,
			LatencyMS:           event.LatencyMS,
			TTFTMS:              cloneInt64Ptr(event.TTFTMS),
			InputTokens:         event.InputTokens,
			OutputTokens:        event.OutputTokens,
			ReasoningTokens:     event.ReasoningTokens,
			CachedTokens:        event.CachedTokens,
			CacheReadTokens:     event.CacheReadTokens,
			CacheCreationTokens: event.CacheCreationTokens,
			TotalTokens:         event.TotalTokens,
		})
	}
	// 剪枝时间来自缓存时钟，只决定保留窗口，不影响 Overview 当前边界选择。
	now := timeutil.NormalizeStorageTime(c.now())
	// 追加、剪枝、字符串池引用计数必须在同一把锁下完成。
	c.mu.Lock()
	c.appendRecentEventsLocked(rows)
	touchedHealthKeys := c.appendCredentialHealthRowsLocked(credentialHealthRowsFromUsageEvents(events))
	c.pruneLocked(now)
	c.pruneCredentialHealthLocked(now, touchedHealthKeys)
	c.mu.Unlock()
}

// Events 返回缓存中落在指定窗口内的事件；覆盖判断由调用方按 queryNow 统一调度。
func (c *UsageRecentEventCache) Events(start, end time.Time, includeEnd bool, apiGroupKey string) ([]RecentUsageEvent, bool) {
	// nil cache 表示缓存对象不可用，调用方可以按自己的策略 fallback。
	if c == nil {
		return nil, false
	}
	// 查询边界先归一化到项目存储时区，避免 time.Location 差异影响比较。
	start = timeutil.NormalizeStorageTime(start)
	end = timeutil.NormalizeStorageTime(end)
	// 空半开窗口没有事件，但 cache 本身仍然可用。
	if end.Before(start) || (!includeEnd && end.Equal(start)) {
		return nil, true
	}
	// 读锁覆盖 events 遍历，允许多个 Overview/realtime 请求并发读取。
	c.mu.RLock()
	defer c.mu.RUnlock()
	// API Group 过滤在缓存内完成，KeyOverview 和 Overview 共用同一份缓存。
	apiGroupKey = strings.TrimSpace(apiGroupKey)
	result := make([]RecentUsageEvent, 0)
	for _, event := range c.events {
		// 每条事件再归一化一次，防止测试直接构造的时间没有走入库规范化。
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		// 左边界始终是闭区间。
		if timestamp.Before(start) {
			continue
		}
		// DB 历史边界需要尊重 includeEnd；当前右边界会走 EventsSince。
		if includeEnd {
			if timestamp.After(end) {
				continue
			}
		} else if !timestamp.Before(end) {
			continue
		}
		// 有 API Key 限定时，只返回对应 API Group 的事件。
		if apiGroupKey != "" && event.APIGroupKey != apiGroupKey {
			continue
		}
		// 返回副本，避免调用方修改 TTFT 指针影响缓存内部状态。
		result = append(result, cloneRecentUsageEvent(event))
	}
	return result, true
}

// EventsSince 返回从 start 起的缓存事件，供 Overview 当前右边界规避 now/end 轻微漂移。
func (c *UsageRecentEventCache) EventsSince(start time.Time, apiGroupKey string) ([]RecentUsageEvent, bool) {
	// nil cache 表示缓存对象不可用，Overview 当前右边界可回到 DB 旧路径。
	if c == nil {
		return nil, false
	}
	// start 是当前右边界补偿起点，必须使用项目存储时区比较。
	start = timeutil.NormalizeStorageTime(start)
	// Open-ended 读取仍然只遍历内存缓存，不访问数据库。
	c.mu.RLock()
	defer c.mu.RUnlock()
	// API Group 过滤在缓存层完成。
	apiGroupKey = strings.TrimSpace(apiGroupKey)
	result := make([]RecentUsageEvent, 0)
	for _, event := range c.events {
		// 当前右边界只要求 timestamp >= start，不添加 end 上限。
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		if timestamp.Before(start) {
			continue
		}
		// KeyOverview 只读取当前 API Key 对应的事件。
		if apiGroupKey != "" && event.APIGroupKey != apiGroupKey {
			continue
		}
		// 返回副本，避免外部修改指针字段污染缓存。
		result = append(result, cloneRecentUsageEvent(event))
	}
	return result, true
}

// Window 返回缓存保留时长，供调用方用自己的 queryNow 判断覆盖范围。
func (c *UsageRecentEventCache) Window() time.Duration {
	// nil cache 没有覆盖窗口，调用方会自然走 fallback。
	if c == nil {
		return 0
	}
	// window 创建后不再变化，不需要加锁读取。
	return c.window
}

func loadUsageRecentEventCacheRows(db *gorm.DB, start time.Time) ([]recentUsageEventLoadRow, error) {
	var rows []recentUsageEventLoadRow
	// 只 select 最近缓存和 realtime 必需字段，避免大字段进入 70 分钟内存窗口。
	if err := db.Model(&entities.UsageEvent{}).
		Select("api_group_key, provider, auth_type, model, model_alias, timestamp, source, auth_index, failed, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
		// 启动加载只取 retention 左边界之后的数据。
		Where("timestamp >= ?", timeutil.FormatStorageTime(start)).
		// 按时间排序让后续剪枝和调试输出更直观。
		Order("timestamp asc").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage recent event cache rows: %w", err)
	}
	return rows, nil
}

func (c *UsageRecentEventCache) appendRecentEventsLocked(rows []recentUsageEventLoadRow) {
	for _, row := range rows {
		// 单条 row 先做投影瘦身、字符串池化和身份 fallback 预计算。
		event := c.recentEventFromRowLocked(row)
		// 缓存只追加最近投影，不回写数据库，也不影响 usage_events 入库事务。
		c.events = append(c.events, event)
	}
}

func (c *UsageRecentEventCache) recentEventFromRowLocked(row recentUsageEventLoadRow) RecentUsageEvent {
	// auth_type 决定 fallback label 的来源：auth file 用 source，ai provider 用 provider。
	identityKind, fallbackLabel := usageRecentIdentityFallback(row.AuthType, row.Source, row.Provider)
	return RecentUsageEvent{
		// timestamp 进入缓存前统一到项目存储时区。
		Timestamp: timeutil.NormalizeStorageTime(row.Timestamp),
		// 高频重复字符串通过池化复用，降低缓存内存占用。
		APIGroupKey:           c.pool.intern(strings.TrimSpace(row.APIGroupKey)),
		Model:                 c.pool.intern(strings.TrimSpace(row.Model)),
		ModelAlias:            c.pool.intern(strings.TrimSpace(row.ModelAlias)),
		AuthIndex:             c.pool.intern(strings.TrimSpace(row.AuthIndex)),
		IdentityFallbackKind:  identityKind,
		IdentityFallbackLabel: c.pool.intern(fallbackLabel),
		Failed:                row.Failed,
		LatencyMS:             row.LatencyMS,
		TTFTMS:                cloneInt64Ptr(row.TTFTMS),
		InputTokens:           row.InputTokens,
		OutputTokens:          row.OutputTokens,
		ReasoningTokens:       row.ReasoningTokens,
		CachedTokens:          row.CachedTokens,
		CacheReadTokens:       row.CacheReadTokens,
		CacheCreationTokens:   row.CacheCreationTokens,
		TotalTokens:           row.TotalTokens,
	}
}

func (c *UsageRecentEventCache) pruneLocked(now time.Time) {
	// cutoff 是当前缓存窗口左边界，早于它的事件会被移出内存。
	cutoff := now.Add(-c.window)
	// 复用原切片底层数组，避免每次剪枝产生额外分配。
	kept := c.events[:0]
	for _, event := range c.events {
		// 过期事件释放字符串引用后跳过，不再进入 kept。
		if timeutil.NormalizeStorageTime(event.Timestamp).Before(cutoff) {
			c.releaseEventStringsLocked(event)
			continue
		}
		// 未过期事件保留原投影对象。
		kept = append(kept, event)
	}
	// 复用底层数组时，len 之外的旧槽位仍会持有字符串/指针引用；必须清零才能让 GC 回收。
	for index := len(kept); index < len(c.events); index++ {
		c.events[index] = RecentUsageEvent{}
	}
	// 重新指向保留后的窗口数据。
	c.events = kept
}

func (c *UsageRecentEventCache) releaseEventStringsLocked(event RecentUsageEvent) {
	// 每个池化字段都按引用计数释放，计数归零后才删除底层字符串。
	c.pool.release(event.APIGroupKey)
	c.pool.release(event.Model)
	c.pool.release(event.ModelAlias)
	c.pool.release(event.AuthIndex)
	c.pool.release(event.IdentityFallbackLabel)
}

func usageRecentIdentityFallback(authType, source, provider string) (RecentUsageIdentityKind, string) {
	// 先把历史兼容的 auth_type 写法归一，再决定 fallback 维度。
	switch normalizeRecentUsageAuthType(authType) {
	case "oauth":
		// Auth File 没有 identity 命中时，展示 source 作为兜底名称。
		return RecentUsageIdentityAuthFile, strings.TrimSpace(source)
	case "apikey":
		// AI Provider 没有 identity 命中时，展示 provider 作为兜底名称。
		return RecentUsageIdentityAIProvider, strings.TrimSpace(provider)
	default:
		// 未知类型不做 fallback，避免把无意义字符串展示成身份。
		return RecentUsageIdentityUnknown, ""
	}
}

func normalizeRecentUsageAuthType(value string) string {
	// auth_type 来自不同历史路径，先小写并去空白。
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "oauth":
		return "oauth"
	case "apikey", "api_key":
		return "apikey"
	default:
		return ""
	}
}

func (p *recentUsageStringPool) intern(value string) string {
	// 空字符串不进池，减少 map key 和引用计数噪音。
	if value == "" {
		return ""
	}
	// 已存在的字符串只增加引用计数，返回同一份 value。
	if item, ok := p.values[value]; ok {
		item.refs++
		return item.value
	}
	// 新字符串 clone 一份由池持有，避免引用到较大的原始字符串底层数组。
	cloned := strings.Clone(value)
	// 初次进入池时引用计数为 1。
	p.values[cloned] = &recentUsageInternedString{value: cloned, refs: 1}
	return cloned
}

func (p *recentUsageStringPool) release(value string) {
	// 空字符串没有进入池，无需释放。
	if value == "" {
		return
	}
	// 找不到说明不是池化值或已释放，直接忽略保持剪枝幂等。
	item, ok := p.values[value]
	if !ok {
		return
	}
	// 每移出一个事件，就释放它持有的一次引用。
	item.refs--
	if item.refs <= 0 {
		// 引用归零后删除池项，让字符串可被 GC 回收。
		delete(p.values, value)
	}
}

func cloneUsageEventsForRecentCache(events []entities.UsageEvent) []entities.UsageEvent {
	// 异步投递必须复制切片，避免调用方复用或修改原始 batch。
	result := make([]entities.UsageEvent, len(events))
	for index := range events {
		// 结构体浅拷贝覆盖大多数字段。
		result[index] = events[index]
		// 指针字段需要深拷贝避免跨 goroutine 共享。
		result[index].ModelAlias = cloneStringPtr(events[index].ModelAlias)
		result[index].TTFTMS = cloneInt64Ptr(events[index].TTFTMS)
	}
	return result
}

func cloneRecentUsageEvent(event RecentUsageEvent) RecentUsageEvent {
	// RecentUsageEvent 也包含 TTFT 指针，返回给调用方前需要复制。
	event.TTFTMS = cloneInt64Ptr(event.TTFTMS)
	return event
}

func recentUsageEventToEntity(event RecentUsageEvent) entities.UsageEvent {
	// Overview 聚合已有实体处理函数，这里把缓存投影还原成最小 UsageEvent。
	result := entities.UsageEvent{
		APIGroupKey:         event.APIGroupKey,
		Model:               event.Model,
		Timestamp:           event.Timestamp,
		AuthIndex:           event.AuthIndex,
		Failed:              event.Failed,
		LatencyMS:           event.LatencyMS,
		TTFTMS:              cloneInt64Ptr(event.TTFTMS),
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
	if modelAlias := strings.TrimSpace(event.ModelAlias); modelAlias != "" {
		result.ModelAlias = &modelAlias
	}
	return result
}

func cloneInt64Ptr(value *int64) *int64 {
	// nil 表示原始事件没有该样本，必须原样保留。
	if value == nil {
		return nil
	}
	// 非 nil 时复制数值，避免调用方通过指针修改缓存内容。
	cloned := *value
	return &cloned
}

func cloneStringPtr(value *string) *string {
	// nil 表示原始事件没有该可选字段，必须原样保留。
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
