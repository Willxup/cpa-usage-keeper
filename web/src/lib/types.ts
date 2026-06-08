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

export interface StatusResponse {
  running: boolean
  sync_running: boolean
  timezone: string
  version?: string
  updateCheckEnabled?: boolean
  quotaAutoRefreshEnabled?: boolean
  cpa_public_url?: string
  last_run_at?: string
  last_error?: string
  last_warning?: string
  last_status?: string
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
  requests_by_day: Record<string, number>
  requests_by_hour: Record<string, number>
  tokens_by_day: Record<string, number>
  tokens_by_hour: Record<string, number>
}

export interface UsageOverviewSummary {
  request_count: number
  token_count: number
  window_minutes: number
  rpm: number
  tpm: number
  total_cost: number
  cost_available: boolean
  cached_tokens: number
  reasoning_tokens: number
}

export interface UsageOverviewSeries {
  requests: Record<string, number>
  tokens: Record<string, number>
  rpm: Record<string, number>
  tpm: Record<string, number>
  cost: Record<string, number>
  input_tokens: Record<string, number>
  output_tokens: Record<string, number>
  cached_tokens: Record<string, number>
  reasoning_tokens: Record<string, number>
  models?: Record<string, UsageOverviewSeries>
}

export interface UsageOverviewServiceHealthBlock {
  start_time: string
  end_time: string
  success: number
  failure: number
  rate: number
}

export interface UsageOverviewServiceHealth {
  total_success: number
  total_failure: number
  success_rate: number
  rows?: number
  columns?: number
  bucket_seconds?: number
  window_start?: string
  window_end?: string
  block_details: UsageOverviewServiceHealthBlock[]
}

export interface UsageOverviewResponse {
  usage: UsageOverviewUsageSnapshot
  summary?: UsageOverviewSummary
  series?: UsageOverviewSeries
  hourly_series?: UsageOverviewSeries
  daily_series?: UsageOverviewSeries
  service_health?: UsageOverviewServiceHealth
  timezone?: string
  range_start?: string
  range_end?: string
}

export interface UsageEventTokens {
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  total_tokens: number
}

export interface UsageEvent {
  id?: string
  timestamp: string
  api_key?: string
  model: string
  reasoning_effort?: string
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
}

export interface UsageEventModelFilterOptionsResponse {
  models: string[]
}

export interface UsageEventSourceFilterOptionsResponse {
  sources: UsageSourceFilterOption[]
}

export type UsageIdentityAuthType = 1 | 2

export interface UsageIdentity {
  id: string
  name: string
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
  plan_type?: string
  active_start?: string
  active_until?: string
  total_requests: number
  success_count: number
  failure_count: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  total_tokens: number
  last_aggregated_usage_event_id: string
  first_used_at?: string
  last_used_at?: string
  stats_updated_at?: string
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
  planType?: string
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
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  requests: number
  cost_usd: number
  cost_available: boolean
}

export interface AnalysisCompositionItem {
  key: string
  label: string
  total_tokens: number
  requests: number
  percent: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  cost_usd: number
  cost_available: boolean
}

export interface AnalysisHeatmapCell {
  api_key: string
  model: string
  input_tokens: number
  output_tokens: number
  cached_tokens: number
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
  input_cost_usd: number
  output_cost_usd: number
  cached_cost_usd: number
  total_cost_usd: number
  cost_available: boolean
}

export interface AnalysisModelEfficiencyItem {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  cost_usd: number
  cost_available: boolean
  cost_per_request_usd: number
  output_tokens_per_request: number
  cache_rate: number
}

export interface AnalysisResponse {
  granularity: 'hourly' | 'daily'
  timezone: string
  range_start?: string
  range_end?: string
  token_usage: AnalysisTokenUsageBucket[]
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
  cache: number
  cacheCreation: number
}

export interface PricingEntry {
  model: string
  pricing_style: PricingStyle
  prompt_price_per_1m: number
  completion_price_per_1m: number
  cache_price_per_1m: number
  cache_creation_price_per_1m: number
}

export interface UsedModelsResponse {
  models: string[]
}

export interface PricingResponse {
  pricing: PricingEntry[]
}

export type KeyOverviewTimeRange = '4h' | '8h' | '12h' | '24h' | 'today' | 'yesterday' | '7d' | '30d'

export type UsageTimeRange = KeyOverviewTimeRange | 'custom'

export interface UsageFilterWindow {
  startMs?: number
  endMs?: number
  windowMinutes?: number
}
