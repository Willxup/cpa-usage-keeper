import type {
  ApiSummaryItem,
  EventRow,
  ModelSummaryItem,
  PricingEntry,
  RateStats,
  SummaryCardValue,
  TokenBreakdown,
  TrendPoint,
  TrendSeries,
  UsageDetail,
  UsageSeriesDimension,
  UsageSnapshot,
  UsageTimeRange,
} from './types'

interface UsageEventWithNames extends UsageDetail {
  apiName: string
  modelName: string
}

const SERIES_COLORS = ['#2563eb', '#7c3aed', '#10b981', '#f97316', '#dc2626', '#0891b2', '#f59e0b']

export function formatNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value)
}

function getPriceMap(pricing: PricingEntry[]): Map<string, PricingEntry> {
  return new Map(pricing.map((entry) => [entry.model, entry]))
}

function formatLocalDayKey(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function calculateEventCost(event: UsageEventWithNames, priceMap: Map<string, PricingEntry>): number {
  const pricing = priceMap.get(event.modelName)
  if (!pricing) return 0
  return (
    (event.tokens.input_tokens / 1_000_000) * pricing.prompt_price_per_1m +
    (event.tokens.output_tokens / 1_000_000) * pricing.completion_price_per_1m +
    (event.tokens.cached_tokens / 1_000_000) * pricing.cache_price_per_1m
  )
}

export function collectUsageEvents(usage: UsageSnapshot): UsageEventWithNames[] {
  const events: UsageEventWithNames[] = []

  for (const [apiName, api] of Object.entries(usage.apis)) {
    for (const [modelName, model] of Object.entries(api.models)) {
      for (const detail of model.details ?? []) {
        events.push({
          ...detail,
          apiName,
          modelName,
        })
      }
    }
  }

  return events.sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp))
}

export function filterUsageSnapshot(usage: UsageSnapshot, range: UsageTimeRange): UsageSnapshot {
  if (range === 'all' || range === 'custom') {
    return usage
  }

  const events = collectUsageEvents(usage)
  if (events.length === 0) {
    return usage
  }

  const latestTimestamp = Math.max(...events.map((event) => Date.parse(event.timestamp)).filter((value) => Number.isFinite(value)))
  if (!Number.isFinite(latestTimestamp)) {
    return usage
  }

  const presetWindowMs: Partial<Record<UsageTimeRange, number>> = {
    '4h': 4 * 60 * 60 * 1000,
    '8h': 8 * 60 * 60 * 1000,
    '12h': 12 * 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
  }
  const nowMs = Date.now()
  const threshold = range === 'today'
    ? new Date(nowMs).setHours(0, 0, 0, 0)
    : latestTimestamp - (presetWindowMs[range] ?? 0)
  const upperThreshold = range === 'today' ? nowMs : Number.POSITIVE_INFINITY

  const filtered: UsageSnapshot = {
    total_requests: 0,
    success_count: 0,
    failure_count: 0,
    total_tokens: 0,
    requests_by_day: {},
    requests_by_hour: {},
    tokens_by_day: {},
    tokens_by_hour: {},
    apis: {},
  }

  for (const event of events) {
    const timestampMs = Date.parse(event.timestamp)
    if (!Number.isFinite(timestampMs) || timestampMs < threshold || timestampMs > upperThreshold) {
      continue
    }

    const api = filtered.apis[event.apiName] ?? {
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      total_tokens: 0,
      models: {},
    }
    const model = api.models[event.modelName] ?? {
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      total_tokens: 0,
      details: [],
    }

    model.total_requests += 1
    model.total_tokens += event.tokens.total_tokens
    const modelDetails = model.details ?? (model.details = [])
    modelDetails.push({
      timestamp: event.timestamp,
      latency_ms: event.latency_ms,
      source: event.source,
      auth_index: event.auth_index,
      failed: event.failed,
      tokens: event.tokens,
    })

    api.total_requests += 1
    api.total_tokens += event.tokens.total_tokens
    filtered.total_requests += 1
    filtered.total_tokens += event.tokens.total_tokens

    if (event.failed) {
      model.failure_count += 1
      api.failure_count += 1
      filtered.failure_count += 1
    } else {
      model.success_count += 1
      api.success_count += 1
      filtered.success_count += 1
    }

    const time = new Date(event.timestamp)
    const dayKey = formatLocalDayKey(time)
    const hourKey = `${time.toISOString().slice(0, 13)}:00:00Z`
    filtered.requests_by_day[dayKey] = (filtered.requests_by_day[dayKey] ?? 0) + 1
    filtered.requests_by_hour[hourKey] = (filtered.requests_by_hour[hourKey] ?? 0) + 1
    filtered.tokens_by_day[dayKey] = (filtered.tokens_by_day[dayKey] ?? 0) + event.tokens.total_tokens
    filtered.tokens_by_hour[hourKey] = (filtered.tokens_by_hour[hourKey] ?? 0) + event.tokens.total_tokens

    api.models[event.modelName] = model
    filtered.apis[event.apiName] = api
  }

  return filtered
}

export function buildRateStats(usage: UsageSnapshot): RateStats {
  const events = collectUsageEvents(usage)
  if (events.length === 0) {
    return { rpm: 0, tpm: 0, requestCount: 0, tokenCount: 0, windowMinutes: 30 }
  }

  const latestTimestamp = Math.max(...events.map((event) => Date.parse(event.timestamp)).filter((value) => Number.isFinite(value)))
  const windowMinutes = 30
  const threshold = latestTimestamp - windowMinutes * 60 * 1000
  const windowEvents = events.filter((event) => Date.parse(event.timestamp) >= threshold)
  const requestCount = windowEvents.length
  const tokenCount = windowEvents.reduce((sum, event) => sum + event.tokens.total_tokens, 0)

  return {
    rpm: requestCount / windowMinutes,
    tpm: tokenCount / windowMinutes,
    requestCount,
    tokenCount,
    windowMinutes,
  }
}

export function buildSummaryCards(usage: UsageSnapshot, pricing: PricingEntry[]): SummaryCardValue[] {
  const rateStats = buildRateStats(usage)
  const tokenBreakdown = buildTokenBreakdown(usage)
  const totalCost = buildCostSummary(usage, pricing)
  const hasPricing = pricing.length > 0

  return [
    {
      key: 'requests',
      label: 'Total requests',
      value: formatNumber(usage.total_requests),
      hint: `${formatNumber(usage.success_count)} success / ${formatNumber(usage.failure_count)} failed`,
      accent: '#2563eb',
    },
    {
      key: 'tokens',
      label: 'Total tokens',
      value: formatNumber(usage.total_tokens),
      hint: `${formatNumber(tokenBreakdown.cachedTokens)} cached / ${formatNumber(tokenBreakdown.reasoningTokens)} reasoning`,
      accent: '#7c3aed',
    },
    {
      key: 'rpm',
      label: 'RPM (30m)',
      value: formatNumber(Number(rateStats.rpm.toFixed(2))),
      hint: `${formatNumber(rateStats.requestCount)} requests in last ${rateStats.windowMinutes}m`,
      accent: '#10b981',
    },
    {
      key: 'tpm',
      label: 'TPM (30m)',
      value: formatNumber(Number(rateStats.tpm.toFixed(2))),
      hint: `${formatNumber(rateStats.tokenCount)} tokens in last ${rateStats.windowMinutes}m`,
      accent: '#f97316',
    },
    {
      key: 'cost',
      label: 'Total cost',
      value: hasPricing ? `$${formatNumber(totalCost)}` : '--',
      hint: hasPricing ? 'Calculated from saved backend pricing' : 'Save model pricing to unlock cost analytics',
      accent: '#f59e0b',
    },
  ]
}

export function buildCostSummary(usage: UsageSnapshot, pricing: PricingEntry[]): number {
  const priceMap = getPriceMap(pricing)
  return collectUsageEvents(usage).reduce((sum, event) => sum + calculateEventCost(event, priceMap), 0)
}

export function buildApiSummary(usage: UsageSnapshot, pricing: PricingEntry[]): ApiSummaryItem[] {
  const priceMap = getPriceMap(pricing)

  return Object.entries(usage.apis)
    .map(([apiName, api]) => {
      const models = Object.entries(api.models)
        .map(([modelName, model]) => {
          const detailEvents = (model.details ?? []).map((detail) => ({ ...detail, apiName, modelName }))
          const totalCost = detailEvents.reduce((sum, event) => sum + calculateEventCost(event, priceMap), 0)
          return {
            modelName,
            totalRequests: model.total_requests,
            successCount: model.success_count,
            failureCount: model.failure_count,
            totalTokens: model.total_tokens,
            totalCost,
          }
        })
        .sort((a, b) => b.totalTokens - a.totalTokens)

      return {
        apiName,
        totalRequests: api.total_requests,
        successCount: api.success_count,
        failureCount: api.failure_count,
        totalTokens: api.total_tokens,
        modelCount: models.length,
        totalCost: models.reduce((sum, model) => sum + model.totalCost, 0),
        models,
      }
    })
    .sort((a, b) => b.totalTokens - a.totalTokens)
}

export function buildModelSummary(usage: UsageSnapshot, pricing: PricingEntry[]): ModelSummaryItem[] {
  const priceMap = getPriceMap(pricing)

  return Object.entries(usage.apis)
    .flatMap(([apiName, api]) =>
      Object.entries(api.models).map(([modelName, model]) => {
        const details = model.details ?? []
        const latencyValues = details.map((detail) => detail.latency_ms).filter((value) => Number.isFinite(value))
        const totalLatencyMs = latencyValues.reduce((sum, value) => sum + value, 0)
        const totalCost = details
          .map((detail) => ({ ...detail, apiName, modelName }))
          .reduce((sum, event) => sum + calculateEventCost(event, priceMap), 0)

        return {
          apiName,
          modelName,
          totalRequests: model.total_requests,
          successCount: model.success_count,
          failureCount: model.failure_count,
          totalTokens: model.total_tokens,
          averageLatencyMs: latencyValues.length > 0 ? Math.round(totalLatencyMs / latencyValues.length) : 0,
          totalLatencyMs,
          successRate: model.total_requests > 0 ? (model.success_count / model.total_requests) * 100 : 100,
          totalCost,
        }
      }),
    )
    .sort((a, b) => b.totalTokens - a.totalTokens)
}

export function buildRecentEvents(usage: UsageSnapshot, limit = 12): EventRow[] {
  return collectUsageEvents(usage)
    .map((event) => ({
      timestamp: event.timestamp,
      apiName: event.apiName,
      modelName: event.modelName,
      source: event.source || '-',
      authIndex: event.auth_index || '-',
      failed: event.failed,
      latencyMs: event.latency_ms,
      inputTokens: event.tokens.input_tokens,
      outputTokens: event.tokens.output_tokens,
      reasoningTokens: event.tokens.reasoning_tokens,
      cachedTokens: event.tokens.cached_tokens,
      totalTokens: event.tokens.total_tokens,
    }))
    .slice(0, limit)
}

export function buildTrendPoints(series: Record<string, number>): TrendPoint[] {
  return Object.entries(series)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([label, value]) => ({ label, value }))
}

export function buildSeriesTrends(usage: UsageSnapshot, dimension: UsageSeriesDimension, metric: 'requests' | 'tokens'): TrendSeries[] {
  if (dimension === 'all') {
    return [
      {
        key: metric,
        label: metric === 'requests' ? 'All requests' : 'All tokens',
        color: SERIES_COLORS[0],
        data: buildTrendPoints(metric === 'requests' ? usage.requests_by_hour : usage.tokens_by_hour),
      },
    ]
  }

  const grouped = new Map<string, Record<string, number>>()
  for (const event of collectUsageEvents(usage)) {
    const key = dimension === 'api' ? event.apiName : event.modelName
    const hourKey = `${new Date(event.timestamp).toISOString().slice(0, 13)}:00:00Z`
    const bucket = grouped.get(key) ?? {}
    bucket[hourKey] = (bucket[hourKey] ?? 0) + (metric === 'requests' ? 1 : event.tokens.total_tokens)
    grouped.set(key, bucket)
  }

  return [...grouped.entries()]
    .map(([key, values], index) => ({
      key,
      label: key,
      color: SERIES_COLORS[index % SERIES_COLORS.length],
      data: buildTrendPoints(values),
    }))
    .sort((left, right) => {
      const leftTotal = left.data.reduce((sum, point) => sum + point.value, 0)
      const rightTotal = right.data.reduce((sum, point) => sum + point.value, 0)
      return rightTotal - leftTotal
    })
    .slice(0, 5)
}

export function buildCostTrendPoints(usage: UsageSnapshot, pricing: PricingEntry[]): TrendPoint[] {
  const priceMap = getPriceMap(pricing)
  const buckets: Record<string, number> = {}
  for (const event of collectUsageEvents(usage)) {
    const hourKey = `${new Date(event.timestamp).toISOString().slice(0, 13)}:00:00Z`
    buckets[hourKey] = (buckets[hourKey] ?? 0) + calculateEventCost(event, priceMap)
  }
  return buildTrendPoints(buckets)
}

export function buildTokenBreakdown(usage: UsageSnapshot): TokenBreakdown {
  return Object.values(usage.apis).reduce(
    (totals, api) => {
      for (const model of Object.values(api.models)) {
        for (const detail of model.details ?? []) {
          totals.inputTokens += detail.tokens.input_tokens
          totals.outputTokens += detail.tokens.output_tokens
          totals.reasoningTokens += detail.tokens.reasoning_tokens
          totals.cachedTokens += detail.tokens.cached_tokens
        }
      }
      return totals
    },
    {
      inputTokens: 0,
      outputTokens: 0,
      reasoningTokens: 0,
      cachedTokens: 0,
    },
  )
}
