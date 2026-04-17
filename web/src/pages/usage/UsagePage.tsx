import { useEffect, useMemo, useState } from 'react'
import { fetchPricing, fetchUsage, fetchUsedModels, updatePricing } from '../../lib/api'
import {
  buildApiSummary,
  buildCostTrendPoints,
  buildModelSummary,
  buildRecentEvents,
  buildSummaryCards,
  buildSeriesTrends,
  buildTokenBreakdown,
  collectUsageEvents,
  filterUsageSnapshot,
} from '../../lib/usage'
import type { PricingEntry, UsageSeriesDimension, UsageSnapshot, UsageTimeRange } from '../../lib/types'
import { ApiOverviewCard } from '../../components/usage/ApiOverviewCard'
import { ChartLineSelector } from '../../components/usage/ChartLineSelector'
import { CostTrendCard } from '../../components/usage/CostTrendCard'
import { LatencyCard } from '../../components/usage/LatencyCard'
import { ModelDetailsCard } from '../../components/usage/ModelDetailsCard'
import { PricingCard } from '../../components/usage/PricingCard'
import { RequestEventsCard } from '../../components/usage/RequestEventsCard'
import { ServiceHealthCard } from '../../components/usage/ServiceHealthCard'
import { SourceStatsCard } from '../../components/usage/SourceStatsCard'
import { SummaryCard } from '../../components/usage/SummaryCard'
import { TimeRangeSelector } from '../../components/usage/TimeRangeSelector'
import { TokenBreakdownCard } from '../../components/usage/TokenBreakdownCard'
import { TrendChartCard } from '../../components/usage/TrendChartCard'
import styles from './UsagePage.module.css'

interface UsagePageProps {
  theme: 'light' | 'dark'
  onThemeChange: (theme: 'light' | 'dark') => void
}

export function UsagePage({ theme, onThemeChange }: UsagePageProps) {
  const [usage, setUsage] = useState<UsageSnapshot | null>(null)
  const [usedModels, setUsedModels] = useState<string[]>([])
  const [pricing, setPricing] = useState<PricingEntry[]>([])
  const [timeRange, setTimeRange] = useState<UsageTimeRange>('all')
  const [seriesDimension, setSeriesDimension] = useState<UsageSeriesDimension>('all')
  const [loading, setLoading] = useState(true)
  const [pricingSaving, setPricingSaving] = useState(false)
  const [error, setError] = useState('')
  const [pricingError, setPricingError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    let active = true

    async function load() {
      setLoading(true)
      setError('')
      try {
        const [usageResponse, usedModelsResponse, pricingResponse] = await Promise.all([
          fetchUsage(controller.signal),
          fetchUsedModels(controller.signal),
          fetchPricing(controller.signal),
        ])
        if (!active) return
        setUsage(usageResponse.usage)
        setUsedModels(usedModelsResponse.models)
        setPricing(pricingResponse.pricing)
      } catch (err) {
        if (!active) return
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load usage data')
      } finally {
        if (active) setLoading(false)
      }
    }

    void load()

    return () => {
      active = false
      controller.abort()
    }
  }, [refreshKey])

  const filteredUsage = useMemo(() => (usage ? filterUsageSnapshot(usage, timeRange) : null), [usage, timeRange])
  const filteredEvents = useMemo(() => (filteredUsage ? collectUsageEvents(filteredUsage) : []), [filteredUsage])

  const summaryCards = useMemo(() => (filteredUsage ? buildSummaryCards(filteredUsage, pricing) : []), [filteredUsage, pricing])
  const apiSummary = useMemo(() => (filteredUsage ? buildApiSummary(filteredUsage, pricing) : []), [filteredUsage, pricing])
  const modelSummary = useMemo(() => (filteredUsage ? buildModelSummary(filteredUsage, pricing) : []), [filteredUsage, pricing])
  const recentEvents = useMemo(() => (filteredUsage ? buildRecentEvents(filteredUsage, 250) : []), [filteredUsage])
  const requestTrend = useMemo(() => (filteredUsage ? buildSeriesTrends(filteredUsage, seriesDimension, 'requests') : []), [filteredUsage, seriesDimension])
  const tokenTrend = useMemo(() => (filteredUsage ? buildSeriesTrends(filteredUsage, seriesDimension, 'tokens') : []), [filteredUsage, seriesDimension])
  const costTrend = useMemo(() => (filteredUsage ? buildCostTrendPoints(filteredUsage, pricing) : []), [filteredUsage, pricing])
  const tokenBreakdown = useMemo(
    () => (filteredUsage ? buildTokenBreakdown(filteredUsage) : { inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cachedTokens: 0 }),
    [filteredUsage],
  )

  const latencyStats = useMemo(() => {
    if (filteredEvents.length === 0) {
      return { averageLatencyMs: 0, maxLatencyMs: 0, sampleCount: 0 }
    }
    const latencies = filteredEvents.map((event) => event.latency_ms)
    const total = latencies.reduce((sum, value) => sum + value, 0)
    return {
      averageLatencyMs: Math.round(total / latencies.length),
      maxLatencyMs: Math.max(...latencies),
      sampleCount: latencies.length,
    }
  }, [filteredEvents])

  const sourceStats = useMemo(() => {
    const stats = new Map<string, { source: string; requests: number; tokens: number }>()
    for (const event of filteredEvents) {
      const source = event.source || '-'
      const existing = stats.get(source) ?? { source, requests: 0, tokens: 0 }
      existing.requests += 1
      existing.tokens += event.tokens.total_tokens
      stats.set(source, existing)
    }
    return [...stats.values()].sort((left, right) => right.tokens - left.tokens).slice(0, 8)
  }, [filteredEvents])

  async function handleSavePricing(model: string, payload: Omit<PricingEntry, 'model'>) {
    setPricingSaving(true)
    setPricingError('')
    try {
      const updated = await updatePricing(model, payload)
      setPricing((current) => {
        const next = current.filter((entry) => entry.model !== updated.model)
        next.push(updated)
        return next.sort((left, right) => left.model.localeCompare(right.model))
      })
    } catch (err) {
      setPricingError(err instanceof Error ? err.message : 'Failed to save pricing')
    } finally {
      setPricingSaving(false)
    }
  }

  return (
    <main className={styles.container}>
      {loading && !usage ? (
        <div className={styles.loadingOverlay} aria-busy="true">
          <div className={styles.loadingOverlayContent}>
            <span className={styles.loadingOverlaySpinner} aria-hidden="true" />
            <span className={styles.loadingOverlayText}>Loading usage data…</span>
          </div>
        </div>
      ) : null}

      <section className={styles.header}>
        <h1 className={styles.pageTitle}>Usage</h1>
        <div className={styles.headerActions}>
          <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
          <button className={styles.secondaryButton} onClick={() => onThemeChange(theme === 'dark' ? 'light' : 'dark')}>
            {theme === 'dark' ? 'Light mode' : 'Dark mode'}
          </button>
          <button className={styles.secondaryButton} onClick={() => setRefreshKey((value) => value + 1)} disabled={loading}>
            {loading ? 'Loading...' : 'Refresh'}
          </button>
        </div>
      </section>

      {error ? <section className={styles.errorBox}>{error}</section> : null}

      {filteredUsage ? (
        <>
          <section className={styles.statsGrid}>
            {summaryCards.map((card) => (
              <SummaryCard key={card.key} card={card} />
            ))}
          </section>

          <ChartLineSelector value={seriesDimension} onChange={setSeriesDimension} />

          <ServiceHealthCard
            totalRequests={filteredUsage.total_requests}
            successCount={filteredUsage.success_count}
            failureCount={filteredUsage.failure_count}
          />

          <section className={styles.chartsGrid}>
            <TrendChartCard
              title="Requests trend"
              periodLabel="Request volume over time"
              series={requestTrend}
            />
            <TrendChartCard
              title="Tokens trend"
              periodLabel="Token volume over time"
              series={tokenTrend}
            />
          </section>

          <section className={styles.chartsGrid}>
            <TokenBreakdownCard
              inputTokens={tokenBreakdown.inputTokens}
              outputTokens={tokenBreakdown.outputTokens}
              reasoningTokens={tokenBreakdown.reasoningTokens}
              cachedTokens={tokenBreakdown.cachedTokens}
            />
            <CostTrendCard data={costTrend} />
          </section>

          <section className={styles.detailsGrid}>
            <ApiOverviewCard items={apiSummary} />
            <ModelDetailsCard items={modelSummary} />
          </section>

          <RequestEventsCard items={recentEvents} />

          <section className={styles.detailsGrid}>
            <LatencyCard
              averageLatencyMs={latencyStats.averageLatencyMs}
              maxLatencyMs={latencyStats.maxLatencyMs}
              sampleCount={latencyStats.sampleCount}
            />
            <SourceStatsCard items={sourceStats} />
          </section>

          <PricingCard
            usedModels={usedModels}
            pricing={pricing}
            saving={pricingSaving}
            error={pricingError}
            onSave={handleSavePricing}
          />
        </>
      ) : null}
    </main>
  )
}
