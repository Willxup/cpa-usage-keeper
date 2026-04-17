export interface AuthSessionResponse {
  authenticated: boolean
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
  auth_index: string
  failed: boolean
  tokens: UsageTokenStats
}

export interface UsageModelSnapshot {
  total_requests: number
  success_count: number
  failure_count: number
  total_tokens: number
  details: UsageDetail[]
}

export interface UsageApiSnapshot {
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

export interface UsageResponse {
  usage: UsageSnapshot
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

export type UsageTimeRange = 'all' | '4h' | '8h' | '12h' | '24h' | '7d'

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
