export interface AuthSessionResponse {
  authenticated: boolean
}

export interface StatusResponse {
  running: boolean
  sync_running: boolean
  last_run_at?: string
  last_error?: string
  last_warning?: string
  last_status?: string
}

export interface UsageTokenStats {
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  total_tokens: number
}

export interface UsageDetail {
  timestamp: string
  latency_ms: number
  source: string
  source_raw?: string
  source_display?: string
  source_type?: string
  source_key?: string
  auth_index: string
  failed: boolean
  tokens: UsageTokenStats
}

export interface UsageModelSnapshot {
  total_requests: number
  success_count: number
  failure_count: number
  total_tokens: number
  details?: UsageDetail[]
}

export interface UsageApiSnapshot {
  display_name?: string
  total_requests: number
  success_count: number
  failure_count: number
  total_tokens: number
  models: Record<string, UsageModelSnapshot>
}

export interface UsageSnapshot {
  total_requests: number
  success_count: number
  failure_count: number
  total_tokens: number
  requests_by_day: Record<string, number>
  requests_by_hour: Record<string, number>
  tokens_by_day: Record<string, number>
  tokens_by_hour: Record<string, number>
  apis: Record<string, UsageApiSnapshot>
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
  usage: UsageSnapshot
  summary?: UsageOverviewSummary
  series?: UsageOverviewSeries
  hourly_series?: UsageOverviewSeries
  daily_series?: UsageOverviewSeries
  service_health?: UsageOverviewServiceHealth
}

export interface UsageEventTokens {
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  total_tokens: number
}

export interface UsageEvent {
  id?: number
  timestamp: string
  model: string
  source: string
  source_raw?: string
  source_type?: string
  source_key?: string
  auth_index?: string
  failed: boolean
  latency_ms: number
  tokens: UsageEventTokens
}

export interface UsageSourceFilterOption {
  value: string
  label: string
}

export interface UsageEventsResponse {
  events: UsageEvent[]
  models: string[]
  sources: UsageSourceFilterOption[]
  total_count: number
  page: number
  page_size: number
  total_pages: number
}

export interface UsageEventFilterOptionsResponse {
  models: string[]
  sources: UsageSourceFilterOption[]
}

export interface UsageCredential {
  source: string
  source_type?: string
  source_key?: string
  success_count: number
  failure_count: number
  total_count: number
}

export interface UsageCredentialsResponse {
  credentials: UsageCredential[]
}

export interface UsageAnalysisModel {
  model: string
  total_requests: number
  success_count: number
  failure_count: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  total_tokens: number
  total_latency_ms: number
  latency_sample_count: number
}

export interface UsageAnalysisApi {
  api_key: string
  display_name: string
  total_requests: number
  success_count: number
  failure_count: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  total_tokens: number
  models: UsageAnalysisModel[]
}

export interface UsageAnalysisResponse {
  apis: UsageAnalysisApi[]
  models: UsageAnalysisModel[]
}

export interface PricingEntry {
  model: string
  prompt_price_per_1m: number
  completion_price_per_1m: number
  cache_price_per_1m: number
}

export interface UsedModelsResponse {
  models: string[]
}

export interface PricingResponse {
  pricing: PricingEntry[]
}

export type UsageTimeRange = 'all' | '4h' | '8h' | '12h' | '24h' | 'today' | '7d' | 'custom'

export interface UsageFilterWindow {
  startMs?: number
  endMs?: number
  windowMinutes?: number
}

export type UsageSeriesDimension = 'all' | 'api' | 'model'

export interface SummaryCardValue {
  key: string
  label: string
  value: string
  hint?: string
  accent: string
}

export interface ApiSummaryItem {
  apiName: string
  totalRequests: number
  successCount: number
  failureCount: number
  totalTokens: number
  modelCount: number
  totalCost: number
  models: Array<{
    modelName: string
    totalRequests: number
    successCount: number
    failureCount: number
    totalTokens: number
    totalCost: number
  }>
}

export interface ModelSummaryItem {
  apiName: string
  modelName: string
  totalRequests: number
  successCount: number
  failureCount: number
  totalTokens: number
  averageLatencyMs: number
  totalLatencyMs: number
  successRate: number
  totalCost: number
}

export interface EventRow {
  timestamp: string
  apiName: string
  modelName: string
  source: string
  authIndex: string
  failed: boolean
  latencyMs: number
  inputTokens: number
  outputTokens: number
  reasoningTokens: number
  cachedTokens: number
  totalTokens: number
}

export interface TrendPoint {
  label: string
  value: number
}

export interface TrendSeries {
  key: string
  label: string
  color: string
  data: TrendPoint[]
}

export interface TokenBreakdown {
  inputTokens: number
  outputTokens: number
  reasoningTokens: number
  cachedTokens: number
}

export interface RateStats {
  rpm: number
  tpm: number
  requestCount: number
  tokenCount: number
  windowMinutes: number
}
