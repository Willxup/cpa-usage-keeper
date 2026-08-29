export type AuthRole = 'admin' | 'api_key_viewer'

export interface AuthSessionAPIKeySummary {
  display_key: string
  alias?: string
}

export interface AuthSessionResponse {
  authenticated: boolean
  role?: AuthRole
  api_key?: AuthSessionAPIKeySummary
}

export type AuthManagedSessionKind = 'admin' | 'api_key'
export type AuthManagedSessionSource = 'standard' | 'embed'

export interface AuthManagedSessionItem {
  id: string
  kind: AuthManagedSessionKind
  role: AuthRole
  source?: AuthManagedSessionSource
  alias?: string
  current?: boolean
  loginAt?: string
  lastSeenAt?: string
  expiresAt?: string
  loginIp?: string
  lastSeenIp?: string
  userAgent?: string
  apiKeyId?: string
  label?: string
  displayKey?: string
}

export interface AuthManagedSessionsResponse {
  items: AuthManagedSessionItem[]
}

export interface StatusResponse {
  running: boolean
  sync_running: boolean
  timezone: string
  cpa_public_url?: string
  cpa_request_log_access_enabled?: boolean
  last_error?: string
  last_warning?: string
  last_status?: string
}

export type QuotaAutoRefreshScheduleUnit = 'minute' | 'hour' | 'day' | 'week'

export interface QuotaAutoRefreshSchedule {
  unit: QuotaAutoRefreshScheduleUnit
  value: number
}

export interface QuotaAutoRefreshSettings {
  enabled: boolean
  schedule: QuotaAutoRefreshSchedule | null
}

export interface VersionResponse {
  version: string
  updateCheckEnabled: boolean
}

export interface UpdateCheckResponse {
  currentVersion: string
  latestVersion: string
  updateAvailable: boolean
  canCompare: boolean
  message: string
}

export interface UsageOverviewUsageSnapshot {
  total_requests: number
  success_count: number
  failure_count: number
  total_tokens: number
}

export interface UsageOverviewSummary {
  rpm: number
  tpm: number
  total_cost: number
	cost_available: boolean
	input_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	reasoning_tokens: number
  daily_average_requests?: number
  daily_average_tokens?: number
  daily_average_cost?: number
  daily_average_range_days?: number
}

export interface UsageOverviewSeries {
  buckets: string[]
  requests: number[]
  tokens: number[]
  rpm: number[]
  tpm: number[]
  cost: number[]
	cache_read_rate: Array<number | null>
}

export type UsageActivityWindow = 'day' | 'week' | 'month' | 'year'

export interface UsageActivityBlock {
  start_time: string
  end_time: string
  success: number
  failure: number
  rate: number
	input_tokens: number
	output_tokens: number
	reasoning_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	total_tokens: number
}

export interface UsageActivityResponse {
  window: UsageActivityWindow
  grain: 'short' | 'medium' | 'long' | 'daily'
  timezone?: string
  total_success: number
  total_failure: number
  success_rate: number
	input_tokens: number
	output_tokens: number
	reasoning_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	total_tokens: number
  rows: number
  columns: number
  bucket_seconds: number
  window_start: string
  window_end: string
  blocks: UsageActivityBlock[]
}

export type OverviewRealtimeWindow = '15m' | '30m' | '60m'

export interface RealtimeTokenVelocityPoint {
  bucket: string
  tokens_per_minute: number
  tokens: number
  cost?: number
}

export interface RealtimeResponseLevelPoint {
  bucket: string
  ttft_p50_ms?: number
  ttft_p95_ms?: number
  latency_p50_ms?: number
  latency_p95_ms?: number
}

export interface RealtimeResponseAveragePoint {
  bucket: string
  avg_ms?: number | null
}

export interface RealtimeResponseParticle {
  bucket: string
  timestamp?: string
  ms: number
  count: number
}

export interface RealtimeResponseDistributionSeries {
  average_line: RealtimeResponseAveragePoint[]
  particles: RealtimeResponseParticle[]
  total_particles?: number
  sampled?: boolean
  max_particles?: number
}

export interface RealtimeResponseDistribution {
  ttft: RealtimeResponseDistributionSeries
  latency: RealtimeResponseDistributionSeries
}

export interface RealtimeUsageTopItem {
  key: string
  label: string
  tokens: number
  requests: number
  cost?: number
  share: number
}

export interface RealtimeCurrentUsage {
  models: RealtimeUsageTopItem[]
  api_keys: RealtimeUsageTopItem[]
  auth_files: RealtimeUsageTopItem[]
  ai_providers: RealtimeUsageTopItem[]
}

export interface RealtimeRequestLevelPoint {
  bucket: string
  requests_per_minute: number
  requests: number
}

export interface RealtimeCacheLevelPoint {
	bucket: string
	cache_read_rate?: number | null
	cache_read_tokens: number
	cache_creation_tokens: number
	input_tokens: number
}

export interface OverviewRealtimeBlock {
  window: OverviewRealtimeWindow
  timezone?: string
  bucket_seconds: number
  window_start?: string
  window_end?: string
  token_velocity: RealtimeTokenVelocityPoint[]
  response_level: RealtimeResponseLevelPoint[]
  response_distribution: RealtimeResponseDistribution
  current_usage: RealtimeCurrentUsage
  request_level: RealtimeRequestLevelPoint[]
  cache_level: RealtimeCacheLevelPoint[]
}

export interface UsageOverviewResponse {
  usage: UsageOverviewUsageSnapshot
  summary?: UsageOverviewSummary
  series?: UsageOverviewSeries
  timezone?: string
}

export interface UsageEventTokens {
	input_tokens: number
	output_tokens: number
	reasoning_tokens: number
	cache_read_tokens: number
  cache_creation_tokens: number
  total_tokens: number
}

export interface UsageEvent {
  id?: string
  request_id?: string
  timestamp: string
  api_key?: string
  model: string
  model_alias?: string
  reasoning_effort?: string
  service_tier?: string
  response_service_tier?: string
  executor_type?: string
  endpoint?: string
  source: string
  source_raw?: string
  source_type?: string
  auth_index?: string
  isDelete?: boolean
  failed: boolean
  latency_ms: number
  ttft_ms?: number
  speed_tps?: number
  speed_total_tps?: number
  client_ip?: string | null
  x_forwarded_for?: string | null
  user_agent?: string | null
  tokens: UsageEventTokens
  cost_usd?: number
  cost_available?: boolean
  pricing_style?: PricingStyle
}

export interface UsageSourceFilterOption {
  value: string
  label: string
  displayName?: string
}

export interface UsageEventsResponse {
  events: UsageEvent[]
  total_count: number
  page: number
  page_size: number
  total_pages: number
  next_cursor?: string
  has_more?: boolean
}

export interface ErrorEvent {
  /** Keeper 本地事件主键，仅用于列表 key 与 cursor 稳定排序。 */
  id: string
  /** CPA 生成 Error Event 的时间，不是 Keeper 接收时间。 */
  timestamp: string
  /** CPA result.provider；缺失表示上游未提供。 */
  provider?: string
  /** CPA result.model；缺失表示上游未提供。 */
  model?: string
  /** CPA Error.HTTPStatus；上游无状态时当前契约为 500。 */
  status_code: number
  /** 原始 body 删除当前 Identity API Key、清理控制字符并限制长度后的展示摘要。 */
  body_summary: string
  /** 表示 body_summary 是否因 API 长度上限被截断。 */
  body_truncated: boolean
  /** CPA Error.Code；缺失表示上游没有结构化错误码。 */
  code?: string
  /** 错误发生时 CPA 是否认为该错误可重试。 */
  retryable: boolean
  /** 错误发生时 CPA 给出的凭证级下一次允许重试时间。 */
  credential_retry_after?: string
  /** 错误发生时 CPA 给出的模型级下一次允许重试时间。 */
  model_retry_after?: string
}

export interface ErrorEventsResponse {
  /** 当前 Identity 的 Error Event 游标页；body_summary 已删除真实 API Key。 */
  events: ErrorEvent[]
  /** 下一页 cursor；没有更多数据时缺失。 */
  next_cursor?: string
  /** 是否仍有下一页，前端不依赖总数查询。 */
  has_more: boolean
}

export interface UsageEventRequestLogSection {
  title: string
  content: string
}

export interface UsageEventRequestLogResponse {
  event_id: string
  request_id?: string
  filename?: string
  available: boolean
  previewable?: boolean
  too_large?: boolean
  downloadable?: boolean
  sections: UsageEventRequestLogSection[]
}

export interface UsageEventModelFilterOptionsResponse {
  models: string[]
}

export interface UsageEventSourceFilterOptionsResponse {
  sources: UsageSourceFilterOption[]
}

export type UsageIdentityAuthType = 1 | 2

export interface UsageCredentialHealthBucket {
  start_time: string
  end_time: string
  success: number
  failure: number
  rate: number
}

export interface UsageCredentialHealth {
  window_seconds: number
  bucket_seconds: number
  window_start: string
  window_end: string
  total_success: number
  total_failure: number
  success_rate: number
  buckets: UsageCredentialHealthBucket[]
}

export interface UsageSubscriptionInfo {
  provider: string
  plan: string
  tierId?: string
  tierName?: string
}

export interface UsageIdentity {
  id: string
  name: string
  alias?: string | null
  displayName?: string
  auth_type: UsageIdentityAuthType
  auth_type_name: string
  identity: string
  type: string
  provider: string
  prefix: string
  file_name?: string
  file_path?: string
  priority?: number
  disabled: boolean
  note?: string
  subscription?: UsageSubscriptionInfo
  active_start?: string
  active_until?: string
  total_requests: number
  success_count: number
  failure_count: number
	input_tokens: number
	output_tokens: number
	reasoning_tokens: number
	cache_read_tokens: number
	total_tokens: number
  last_aggregated_usage_event_id: string
  first_used_at?: string
  last_used_at?: string
  stats_updated_at?: string
  credential_health?: UsageCredentialHealth
  is_deleted: boolean
  created_at: string
  updated_at: string
  deleted_at?: string
}

export interface UsageIdentitiesResponse {
  identities: UsageIdentity[]
}

export interface UsageIdentityTypeCount {
  type: string
  count: number
}

export interface UsageIdentitiesPageResponse {
  identities: UsageIdentity[]
  total_count: number
  page: number
  page_size: number
  total_pages: number
  type_counts?: UsageIdentityTypeCount[]
}

export interface UsageQuotaWindow {
  duration?: number
  unit?: string
  seconds?: number
}

export interface UsageQuotaRow {
  key: string
  label?: string
  scope?: string
  metric?: string
  groupKey?: string
  groupLabel?: string
  groupDescription?: string
  used?: number
  limit?: number
  remaining?: number
  usedPercent?: number
  remainingFraction?: number
  allowed?: boolean
  limitReached?: boolean
  window?: UsageQuotaWindow
  resetAt?: string
  resetAfterSeconds?: number
  window_usage_tokens?: number
  window_usage_cost?: number
}

export interface UsageQuotaCheckResponse {
  id: string
  quota: UsageQuotaRow[]
  subscription?: UsageSubscriptionInfo
  rateLimitResetCreditsAvailableCount?: number | null
}

export interface UsageQuotaResetResponse {
  authIndex: string
  code?: string
  windowsReset?: number
}

export interface UsageQuotaResetCredit {
  id: string
  status: string
  grantedAt?: string
  expiresAt: string
}

export interface UsageQuotaResetCreditsResponse {
  authIndex: string
  availableCount: number | null
  credits: UsageQuotaResetCredit[]
}

export interface UsageQuotaCacheItem {
  auth_index: string
  file_name?: string
  status: 'completed' | 'failed'
  quota?: UsageQuotaCheckResponse
  error?: string
  http_status_code?: number
  expires_at?: string
  refreshed_at?: string
}

export interface UsageQuotaCacheResponse {
  items: UsageQuotaCacheItem[]
}

// CodexQuotaHistoryWindow 以最近响应存在的 Primary/Secondary 角色为稳定键，并携带最新周期标题。
export interface CodexQuotaHistoryWindow {
  window_role: 'primary' | 'secondary'
  window_kind?: 'five_hour' | 'weekly' | 'monthly'
  window_seconds: number
  has_current_cycle: boolean
  last_observed_at: string
}

// CodexQuotaHistoryUsage 是周期摘要与百分比变化区间共同复用的动态用量。
export interface CodexQuotaHistoryUsage {
  requests: number
  successful_requests: number
  failed_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  total_tokens: number
  total_cost_usd: number
  cost_available: boolean
}

// CodexQuotaHistoryTransition 只表示真实观察到的相邻百分比变化；跨档不会补中间点。
export interface CodexQuotaHistoryTransition {
  from_remaining_percent: number
  to_remaining_percent: number
  percentage_points: number
  is_direct: boolean
  interval_started_at: string
  interval_ended_at: string
  usage: CodexQuotaHistoryUsage
  tokens_per_point: number
  cost_per_point: number
  cost_per_point_available: boolean
}

// CodexQuotaHistoryCycle 同时提供周期真实边界、Keeper 观察边界、周期总量和效率区间。
export interface CodexQuotaHistoryCycle {
  id: number
  status: 'current' | 'completed'
  window_seconds: number
  window_started_at: string
  reset_at: string
  effective_started_at: string
  effective_ended_at: string
  first_observed_at: string
  last_observed_at: string
  first_remaining_percent: number | null
  last_remaining_percent: number | null
  observation_count: number
  usage: CodexQuotaHistoryUsage
  transitions: CodexQuotaHistoryTransition[]
}

// CodexQuotaHistoryResponse 一次请求驱动当前周期图表和包含进行中周期的最近三十天完整列表。
export interface CodexQuotaHistoryResponse {
  generated_at: string
  range_start: string
  windows: CodexQuotaHistoryWindow[]
  selected_window: CodexQuotaHistoryWindow | null
  cycles: CodexQuotaHistoryCycle[]
}

export interface AuthFilesManagementResponse {
  names: string[]
  affected: number
}

export interface UsageQuotaRefreshTaskResponse {
  authIndex: string
  file_name?: string
  status: 'queued' | 'running' | 'completed' | 'failed'
  quota?: UsageQuotaCheckResponse
  error?: string
  http_status_code?: number
  refreshed_at?: string
  expiresAt?: string
}

export type UsageQuotaInspectionResultStatus = 'normal' | 'limit_reached' | 'unauthorized_401' | 'payment_required_402' | 'other_failed'

export interface UsageQuotaInspectionResult {
  auth_index: string
  name: string
  type: string
  file_name?: string
  status: UsageQuotaInspectionResultStatus
  error?: string
  http_status_code?: number
  refreshed_at?: string
}

export interface UsageQuotaInspectionStatusResponse {
  total: number
  cached: number
  running: boolean
  completed: boolean
  completed_at?: string
  normal: number
  limit_reached: number
  unauthorized_401: number
  payment_required_402: number
  unauthorized_401_402: number
  other_failed: number
  unknown: number
  results: UsageQuotaInspectionResult[]
}

export interface UsageQuotaRefreshTaskRef {
  authIndex: string
}

export interface UsageQuotaRefreshRejectedAuthIndex {
  authIndex: string
  error: 'not_found' | 'not_auth_file' | 'unsupported' | 'duplicate' | 'duplicate_request' | 'invalid'
}

export interface UsageQuotaRefreshResponse {
  tasks: UsageQuotaRefreshTaskRef[]
  rejected: UsageQuotaRefreshRejectedAuthIndex[]
  accepted: number
  skipped: number
  limit: number
}

export interface AnalysisTokenUsageBucket {
	bucket: string
	input_tokens: number
	output_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	reasoning_tokens: number
  total_tokens: number
  requests: number
  cost_usd: number
  cost_available: boolean
}

export interface AnalysisModelUsageSeries {
  model: string
  total_tokens: number[]
  requests: number[]
}

export interface AnalysisModelUsagePayload {
  buckets: string[]
  series: AnalysisModelUsageSeries[]
}

export interface AnalysisCompositionItem {
  key: string
  label: string
  total_tokens: number
  requests: number
  percent: number
	input_tokens: number
	output_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	reasoning_tokens: number
  cost_usd: number
  cost_available: boolean
}

export interface AnalysisHeatmapCell {
  api_key: string
  model: string
	input_tokens: number
	output_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	reasoning_tokens: number
  total_tokens: number
  requests: number
  cost_usd: number
  cost_available: boolean
  intensity: number
}

export interface AnalysisHeatmapPayload {
  api_keys: string[]
  api_key_labels: Record<string, string>
  models: string[]
  cells: AnalysisHeatmapCell[]
}

export interface AnalysisCostBreakdown {
	uncached_input_cost_usd: number
	cache_read_cost_usd: number
	cache_write_cost_usd: number
	output_cost_usd: number
	total_cost_usd: number
  cost_available: boolean
}

export interface AnalysisModelEfficiencyItem {
  model: string
  requests: number
	input_tokens: number
	output_tokens: number
	cache_read_tokens: number
	cache_creation_tokens: number
	reasoning_tokens: number
  total_tokens: number
  cost_usd: number
  cost_available: boolean
  cost_per_request_usd: number
  output_tokens_per_request: number
	cache_read_rate: number
}

export interface AnalysisLatencyPoint {
  ttft_ms: number
  latency_ms: number
}

export interface AnalysisLatencyDensityCell {
  ttft_min_ms: number
  ttft_max_ms: number
  latency_min_ms: number
  latency_max_ms: number
  count: number
  intensity: number
}

export interface AnalysisLatencyDiagnostics {
  supported?: boolean
  unsupported_reason?: 'range_outside_recent_30_days'
  points: AnalysisLatencyPoint[]
  density: AnalysisLatencyDensityCell[]
  total_points: number
  sampled: boolean
  p95_ttft_ms: number
  p95_latency_ms: number
  max_ttft_ms: number
  max_latency_ms: number
}

export interface AnalysisResponse {
  granularity: 'hourly' | 'daily'
  timezone: string
  range_start?: string
  range_end?: string
  token_usage: AnalysisTokenUsageBucket[]
  model_usage?: AnalysisModelUsagePayload
  api_key_composition: AnalysisCompositionItem[]
  model_composition: AnalysisCompositionItem[]
  auth_files_composition: AnalysisCompositionItem[]
  ai_provider_composition: AnalysisCompositionItem[]
  heatmap: AnalysisHeatmapPayload
  cost_breakdown: AnalysisCostBreakdown
  model_efficiency: AnalysisModelEfficiencyItem[]
}

export interface CpaApiKeyDisplayItem {
  id: string
  keyAlias: string
  displayKey: string
  label: string
  lastSyncedAt: string | null
}

export interface CpaApiKeySettingsItem extends CpaApiKeyDisplayItem {
  apiKey: string
}

export interface CpaApiKeyOption {
  id: string
  label: string
}

export interface CpaApiKeysResponse {
  items: CpaApiKeyDisplayItem[]
}

export interface CpaApiKeySettingsResponse {
  items: CpaApiKeySettingsItem[]
}

export interface CpaApiKeyOptionsResponse {
  options: CpaApiKeyOption[]
}

export type PricingStyle = 'openai' | 'claude'

export interface ModelPrice {
	style: PricingStyle
	prompt: number
	completion: number
	cacheRead: number
	cacheWrite: number
	multiplier: number
}

export interface PricingSaveFailure {
  model: string
  message: string
  error?: unknown
}

export interface PricingSaveResult {
  successModels: string[]
  failures: PricingSaveFailure[]
}

export interface PricingEntry {
  model: string
	pricing_style: PricingStyle
	prompt_price_per_1m: number
	completion_price_per_1m: number
	cache_read_price_per_1m: number
	cache_write_price_per_1m: number
	price_multiplier: number
}

export interface UsedModelsResponse {
  models: string[]
}

export interface PricingResponse {
  pricing: PricingEntry[]
}

export interface PricingRule {
  key: string
  value: string
  multiplier: number
}

export interface ReplacePricingRuleInput {
  key: string
  value: string
  multiplier?: number
}

export interface PricingRulesResponse {
  model: string
  rules: PricingRule[]
}

export interface ReplacePricingRulesRequest {
  model: string
  rules: ReplacePricingRuleInput[]
}

export interface PricingSyncMatch {
  model: string
  matched_model: string
  match_type: string
  source_provider_id: string
  source_provider_name: string
	pricing_style: PricingStyle
	prompt_price_per_1m: number
	completion_price_per_1m: number
	cache_read_price_per_1m: number
	cache_write_price_per_1m: number
}

export interface PricingSyncPreviewResponse {
  source: string
  source_url: string
  metadata_models: number
  matches: PricingSyncMatch[]
  unmatched_models: string[]
}

export type UsageRollingHourTimeRange = `${number}h`

export type UsageRollingDayTimeRange = `${number}d`

export type KeyOverviewTimeRange = UsageRollingHourTimeRange | UsageRollingDayTimeRange | 'today' | 'yesterday'

export type UsageTimeRange = KeyOverviewTimeRange | 'custom'

export type UsageCustomRangeUnit = 'hour' | 'day'

export interface UsageCustomRange {
	unit: UsageCustomRangeUnit
	start: string
	end: string
}

export interface UsageRangeRequest {
	range: UsageTimeRange
	unit?: UsageCustomRangeUnit
	start?: string
	end?: string
}

export type UsageActivityRequest = UsageRangeRequest | {
	window: UsageActivityWindow | 'today' | 'yesterday'
}

export interface UsageFilterWindow {
  startMs?: number
  endMs?: number
  windowMinutes?: number
}
