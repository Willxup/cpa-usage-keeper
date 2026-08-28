import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import type { ChartData, ChartOptions } from 'chart.js'
import { Chart } from 'react-chartjs-2'
import '@/lib/chartjs'
import quotaCostIcon from '@/assets/icons/quota-cost.svg'
import quotaRequestIcon from '@/assets/icons/quota-request.svg'
import quotaTokenIcon from '@/assets/icons/quota-token.svg'
import quotaUnusedIcon from '@/assets/icons/quota-unused.svg'
import { ApiError, fetchCodexQuotaHistory, type FetchCodexQuotaHistoryOptions } from '@/lib/api'
import type { CodexQuotaHistoryCycle, CodexQuotaHistoryResponse, CodexQuotaHistoryTransition, CodexQuotaHistoryWindow } from '@/lib/types'
import { useThemeStore } from '@/stores'
import { formatCompactNumber, formatUsd } from '@/utils/usage'
import { buildUsageChartTooltipStyle, getUsageChartTheme, toUsageChartGradientFill, USAGE_CHART_REQUESTS_LINE_COLOR, type UsageChartGradientColor } from '@/utils/usage/chartConfig'
import styles from './CodexQuotaHistoryPanel.module.scss'

type QuotaEfficiencyChartType = 'bar' | 'line'
type QuotaEfficiencyChartData = ChartData<QuotaEfficiencyChartType, Array<number | null>, string>
type QuotaEfficiencyChartOptions = ChartOptions<QuotaEfficiencyChartType>

interface QuotaEfficiencyPoint {
  label: string
  transition: CodexQuotaHistoryTransition
}

interface QuotaSummaryMetrics {
  requests: number
  tokens: number
  cost: number
  costAvailable: boolean
  percentage?: number
}

interface QuotaCycleSummary {
  median: QuotaSummaryMetrics | null
  used: QuotaSummaryMetrics
  fullEstimate: QuotaSummaryMetrics | null
  estimatedUnused: QuotaSummaryMetrics | null
}

type QuotaSummaryKind = 'used' | 'full-estimate' | 'median' | 'estimated-unused'

const QUOTA_EFFICIENCY_BAR_COLORS = {
  direct: { base: '#2563eb', light: '#93c5fd' },
  averaged: { base: '#d97706', light: '#fde68a' },
} satisfies Record<'direct' | 'averaged', UsageChartGradientColor>

interface CodexQuotaHistoryPanelProps {
  authIndex: string
  onAuthRequired?: () => void
}

export function CodexQuotaHistoryPanel({ authIndex, onAuthRequired }: CodexQuotaHistoryPanelProps) {
  const { t, i18n } = useTranslation()
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme)
  const [history, setHistory] = useState<CodexQuotaHistoryResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const controllerRef = useRef<AbortController | null>(null)
  const lastRequestOptionsRef = useRef<FetchCodexQuotaHistoryOptions>({})

  const loadHistory = useCallback(async (options: FetchCodexQuotaHistoryOptions = {}) => {
    const normalizedAuthIndex = authIndex.trim()
    if (!normalizedAuthIndex) return
    // 失败重试必须复用用户刚选择的上游角色，不能退回后端默认窗口。
    const requestOptions = { ...options }
    lastRequestOptionsRef.current = requestOptions
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    setLoading(true)
    setError('')
    try {
      // 窗口切换重新查询选中角色，但响应仍带回全部窗口选项；Token/Cost 指标切换不重新请求。
      const response = await fetchCodexQuotaHistory(normalizedAuthIndex, requestOptions, controller.signal)
      if (controllerRef.current !== controller) return
      setHistory(response)
    } catch (loadError) {
      if (controller.signal.aborted) return
      if (loadError instanceof ApiError && loadError.status === 401) {
        onAuthRequired?.()
        return
      }
      setError(loadError instanceof Error ? loadError.message : t('usage_stats.credentials_quota_history_load_failed'))
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null
        setLoading(false)
      }
    }
  }, [authIndex, onAuthRequired, t])

  useEffect(() => {
    lastRequestOptionsRef.current = {}
    setHistory(null)
    void loadHistory()
    return () => {
      controllerRef.current?.abort()
      controllerRef.current = null
    }
  }, [loadHistory])

  const locale = i18n?.resolvedLanguage || i18n?.language
  // 当前周期身份完全由后端 status 决定；前端不使用浏览器时间重算周期状态。
  const currentCycle = history?.cycles.find((cycle) => cycle.status === 'current') ?? null
  const cycleSummaries = useMemo(() => new Map(
    (history?.cycles ?? []).map((cycle) => [cycle.id, buildQuotaCycleSummary(cycle)]),
  ), [history])

  return (
    <div className={styles.panel} data-codex-quota-history-panel="true">
      {history && history.windows.length > 1 ? (
        <div className={styles.windowSwitcher} aria-label={t('usage_stats.credentials_quota_history_window_selector')}>
          {history.windows.map((window) => {
            const selected = sameQuotaWindow(window, history.selected_window)
            return (
              <button
                key={window.window_role}
                type="button"
                className={styles.segmentButton}
                aria-pressed={selected}
                disabled={loading && selected}
                onClick={() => void loadHistory({ windowRole: window.window_role })}
              >
                {formatWindowLabel(window, t)}
              </button>
            )
          })}
        </div>
      ) : null}

      {error ? (
        <div className={styles.errorState} role="status">
          <span>{error}</span>
          <button type="button" onClick={() => void loadHistory(lastRequestOptionsRef.current)}>{t('common.retry')}</button>
        </div>
      ) : null}

      {loading && !history ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : history ? (
        <>
          <CurrentCycleEfficiencyCard
            cycle={currentCycle}
            window={history.selected_window}
            isDark={resolvedTheme === 'dark'}
            locale={locale}
            summary={currentCycle ? cycleSummaries.get(currentCycle.id) ?? null : null}
          />
          <CyclesList cycles={history.cycles} locale={locale} summaries={cycleSummaries} />
        </>
      ) : !error ? (
        <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_empty')}</div>
      ) : null}
    </div>
  )
}

function CurrentCycleEfficiencyCard({
  cycle,
  window,
  isDark,
  locale,
  summary,
}: {
  cycle: CodexQuotaHistoryCycle | null
  window: CodexQuotaHistoryWindow | null
  isDark: boolean
  locale?: string
  summary: QuotaCycleSummary | null
}) {
  const { t } = useTranslation()
  const chart = useMemo(
    () => buildEfficiencyChart(cycle?.transitions ?? [], isDark, t, locale),
    [cycle?.transitions, isDark, locale, t],
  )

  return (
    <section className={styles.card} data-codex-quota-current-cycle="true">
      <header className={styles.cardHeader}>
        <div>
          <h3>
            {t('usage_stats.credentials_quota_history_current_title')}
            {window ? ` · ${formatWindowLabel(window, t)}` : ''}
          </h3>
          <p className={styles.currentCycleMeta}>
            {cycle
              ? <>
                <span className={styles.currentCycleRange} data-codex-quota-cycle-range="true">
                  {t('usage_stats.credentials_quota_history_cycle_range', {
                    start: formatDateTime(cycle.window_started_at, locale),
                    end: formatDateTime(cycle.reset_at, locale),
                  })}
                </span>
                <span className={styles.currentObservedRange} data-codex-quota-observed-range="true">
                  {t('usage_stats.credentials_quota_history_observed_range', {
                    start: formatDateTime(cycle.first_observed_at, locale),
                    end: formatDateTime(cycle.last_observed_at, locale),
                  })}
                </span>
              </>
              : t('usage_stats.credentials_quota_history_no_current')}
          </p>
        </div>
        {cycle && chart.hasUnavailableCost ? (
          <small className={styles.costHeaderHint} data-codex-quota-cost-warning="true">
            {t('usage_stats.credentials_quota_history_cost_unavailable')}
          </small>
        ) : null}
      </header>
      {!cycle ? (
        <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_no_current')}</div>
      ) : (
        <>
          {cycle.transitions.length === 0 ? (
            <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_no_transition')}</div>
          ) : <>
            <div className={styles.chartFrame} data-codex-quota-efficiency-chart="combined" aria-hidden="true">
              <Chart type="bar" data={chart.data} options={chart.options} />
            </div>
            <CurrentCycleAccessibleSummary transitions={cycle.transitions} locale={locale} />
            <div className={styles.chartLegend}>
              <span><i className={styles.directDot} />{t('usage_stats.credentials_quota_history_direct')}</span>
              <span><i className={styles.crossDot} />{t('usage_stats.credentials_quota_history_cross')}</span>
              <span>
                <i
                  className={styles.costLine}
                  data-codex-quota-cost-legend="true"
                  style={{ '--quota-cost-line-color': USAGE_CHART_REQUESTS_LINE_COLOR } as CSSProperties}
                />
                {t('usage_stats.credentials_quota_history_cost_per_point')}
              </span>
            </div>
          </>}
          <CurrentCycleQuotaSummary summary={summary} />
        </>
      )}
    </section>
  )
}

function CurrentCycleQuotaSummary({ summary }: { summary: QuotaCycleSummary | null }) {
  const { t } = useTranslation()

  return (
    <dl className={styles.chartSummary} data-codex-quota-chart-summary="true">
      <QuotaSummaryRow kind="median" label={t('usage_stats.credentials_quota_history_median_per_point')} metrics={summary?.median ?? null} />
      <QuotaSummaryRow kind="used" label={t('usage_stats.credentials_quota_history_used')} metrics={summary?.used ?? null} />
      <QuotaSummaryRow kind="full-estimate" label={t('usage_stats.credentials_quota_history_full_estimate')} metrics={summary?.fullEstimate ?? null} />
    </dl>
  )
}

function QuotaSummaryRow({
  kind,
  label,
  metrics,
  leadingMetric = 'requests',
}: {
  kind: QuotaSummaryKind
  label: string
  metrics: QuotaSummaryMetrics | null
  leadingMetric?: 'requests' | 'percentage'
}) {
  const { t } = useTranslation()
  const values = metrics ? {
    requests: formatQuotaSummaryRequests(metrics.requests),
    percentage: metrics.percentage == null ? '—' : formatQuotaSummaryPercentage(metrics.percentage),
    tokens: formatCompactNumber(metrics.tokens),
    cost: metrics.costAvailable ? formatUsd(metrics.cost) : t('usage_stats.credentials_quota_history_cost_missing'),
  } : null
  return (
    <div className={styles.chartSummaryRow} data-codex-quota-summary={kind}>
      <dt>{label}</dt>
      <dd>{values ? <>
        {leadingMetric === 'requests' ? <QuotaSummaryMetric
          kind="requests"
          icon={quotaRequestIcon}
          label={t('usage_stats.total_requests')}
          value={values.requests}
        /> : <QuotaSummaryMetric
          kind="percentage"
          icon={quotaUnusedIcon}
          label={t('usage_stats.credentials_quota_history_unused_percentage')}
          value={values.percentage}
        />}
        <QuotaSummaryMetric
          kind="tokens"
          icon={quotaTokenIcon}
          label={t('usage_stats.total_tokens')}
          value={values.tokens}
        />
        <QuotaSummaryMetric
          kind="cost"
          icon={quotaCostIcon}
          label={t('usage_stats.total_cost')}
          value={values.cost}
        />
      </> : '—'}</dd>
    </div>
  )
}

function QuotaSummaryMetric({
  kind,
  icon,
  label,
  value,
}: {
  kind: 'requests' | 'percentage' | 'tokens' | 'cost'
  icon: string
  label: string
  value: string
}) {
  return (
    <span
      className={styles.chartSummaryMetric}
      data-codex-quota-summary-metric={kind}
      role="img"
      aria-label={`${label}: ${value}`}
    >
      <img src={icon} alt="" aria-hidden="true" />
      <span>{value}</span>
    </span>
  )
}

function calculateFullQuotaEstimate(cycle: CodexQuotaHistoryCycle): QuotaSummaryMetrics | null {
  const remainingPercent = cycle.last_remaining_percent
  if (remainingPercent == null || !Number.isFinite(remainingPercent)) return null
  const usedPercent = 100 - Math.min(100, Math.max(0, remainingPercent))
  if (usedPercent <= 0 || cycle.usage.requests <= 0 || cycle.usage.total_tokens <= 0) return null
  // 与认证文件列表 Estimated 口径一致：当前用量除以已用比例，外推到 100% 额度。
  const ratio = usedPercent / 100
  return {
    requests: cycle.usage.requests / ratio,
    tokens: cycle.usage.total_tokens / ratio,
    cost: cycle.usage.total_cost_usd / ratio,
    costAvailable: cycle.usage.cost_available,
  }
}

function buildQuotaCycleSummary(cycle: CodexQuotaHistoryCycle): QuotaCycleSummary {
  const used: QuotaSummaryMetrics = {
    requests: cycle.usage.requests,
    tokens: cycle.usage.total_tokens,
    cost: cycle.usage.total_cost_usd,
    costAvailable: cycle.usage.cost_available,
  }
  const fullEstimate = calculateFullQuotaEstimate(cycle)
  const median = calculateQuotaMedianMetrics(cycle.transitions)
  const estimatedUnused = cycle.status === 'completed' ? subtractQuotaMetrics(fullEstimate, used) : null
  return { median, used, fullEstimate, estimatedUnused }
}

function calculateQuotaMedianMetrics(transitions: CodexQuotaHistoryTransition[]): QuotaSummaryMetrics | null {
  const points = expandEfficiencyPoints(transitions)
  if (points.length === 0) return null
  const requestMedian = calculateMedian(points.map(({ transition }) => transition.usage.requests / transition.percentage_points))
  const tokenMedian = calculateMedian(points.map(({ transition }) => transition.tokens_per_point))
  const hasUnavailableCost = transitions.some((transition) => !transition.cost_per_point_available)
  const costMedian = hasUnavailableCost
    ? null
    : calculateMedian(points.map(({ transition }) => transition.cost_per_point))
  if (requestMedian == null || tokenMedian == null) return null
  return {
    requests: requestMedian,
    tokens: tokenMedian,
    cost: costMedian ?? 0,
    costAvailable: costMedian != null,
  }
}

function subtractQuotaMetrics(
  fullEstimate: QuotaSummaryMetrics | null,
  usage: QuotaSummaryMetrics,
): QuotaSummaryMetrics | null {
  if (!fullEstimate) return null
  const unusedTokens = Math.max(fullEstimate.tokens - usage.tokens, 0)
  return {
    requests: Math.max(fullEstimate.requests - usage.requests, 0),
    tokens: unusedTokens,
    cost: Math.max(fullEstimate.cost - usage.cost, 0),
    costAvailable: fullEstimate.costAvailable && usage.costAvailable,
    percentage: fullEstimate.tokens > 0 ? (unusedTokens / fullEstimate.tokens) * 100 : 0,
  }
}

function formatQuotaSummaryRequests(value: number): string {
  if (Math.abs(value) >= 1_000) return formatCompactNumber(value)
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: Math.abs(value) >= 100 ? 0 : Math.abs(value) >= 10 ? 1 : 2,
  }).format(value)
}

function formatQuotaSummaryPercentage(value: number): string {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)}%`
}

function CurrentCycleAccessibleSummary({
  transitions,
  locale,
}: {
  transitions: CodexQuotaHistoryTransition[]
  locale?: string
}) {
  const { t } = useTranslation()
  return (
    <ul
      className={styles.screenReaderOnly}
      data-codex-quota-current-cycle-summary="true"
      aria-label={t('usage_stats.credentials_quota_history_current_title')}
    >
      {transitions.map((transition, index) => (
        <li key={`${transition.interval_started_at}:${transition.to_remaining_percent}:${index}`}>
          <span>{transition.from_remaining_percent}% → {transition.to_remaining_percent}%.</span>{' '}
          <span>
            {t('usage_stats.credentials_quota_history_interval')}: {formatDateTime(transition.interval_started_at, locale)} → {formatDateTime(transition.interval_ended_at, locale)}.
          </span>{' '}
          <span>
            {transition.is_direct
              ? t('usage_stats.credentials_quota_history_direct')
              : t('usage_stats.credentials_quota_history_cross_points', { count: transition.percentage_points })}.
          </span>{' '}
          <span>{t('usage_stats.credentials_quota_history_tokens_per_point')}: {formatCompactNumber(transition.tokens_per_point)} Token/1%.</span>{' '}
          <span>
            {t('usage_stats.credentials_quota_history_cost_per_point')}: {transition.cost_per_point_available
              ? `${formatUsd(transition.cost_per_point)}/1%`
              : t('usage_stats.credentials_quota_history_cost_missing')}.
          </span>{' '}
          {!transition.is_direct ? (
            <span>
              {t('usage_stats.total_tokens')}: {formatCompactNumber(transition.usage.total_tokens)} Token.{' '}
              {t('usage_stats.total_cost')}: {transition.usage.cost_available
                ? formatUsd(transition.usage.total_cost_usd)
                : t('usage_stats.credentials_quota_history_cost_missing')}.
            </span>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

function CyclesList({
  cycles,
  locale,
  summaries,
}: {
  cycles: CodexQuotaHistoryCycle[]
  locale?: string
  summaries: Map<number, QuotaCycleSummary>
}) {
  const { t } = useTranslation()
  return (
    <section className={styles.historySection} data-codex-quota-cycles="true">
      <div className={styles.sectionHeading}>
        <div>
          <h3>{t('usage_stats.credentials_quota_history_records_title')}</h3>
          <p>{t('usage_stats.credentials_quota_history_records_subtitle')}</p>
        </div>
        <span>{t('usage_stats.credentials_quota_history_cycle_count', { count: cycles.length })}</span>
      </div>
      {cycles.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_no_records')}</div>
      ) : (
        <div className={styles.cycleList}>
          {cycles.map((cycle) => (
            <CycleCard key={cycle.id} cycle={cycle} locale={locale} summary={summaries.get(cycle.id) ?? null} />
          ))}
        </div>
      )}
    </section>
  )
}

function CycleCard({
  cycle,
  locale,
  summary,
}: {
  cycle: CodexQuotaHistoryCycle
  locale?: string
  summary: QuotaCycleSummary | null
}) {
  const { t } = useTranslation()
  const statusLabel = cycle.status === 'current'
    ? t('usage_stats.credentials_quota_history_status_current')
    : t('usage_stats.credentials_quota_history_status_completed')
  return (
    <article
      className={styles.cycleCard}
      data-codex-quota-cycle-id={cycle.id}
      data-codex-quota-cycle-status={cycle.status}
    >
      <div className={`${styles.boundaryRow} ${styles.startBoundary}`.trim()}>
        <span>{t('usage_stats.credentials_quota_history_cycle_start')}</span>
        <strong>
          {formatDateTime(cycle.effective_started_at, locale)}
          <i className={cycle.status === 'current' ? styles.currentStatus : styles.completedStatus}>{statusLabel}</i>
        </strong>
        <small>
          {formatCycleWindowLabel(cycle.window_seconds, t)}
          {' · '}
          {t('usage_stats.credentials_quota_history_first_observed', { value: formatDateTime(cycle.first_observed_at, locale) })}
          {' · '}
          {t('usage_stats.credentials_quota_history_percent_summary', {
            percent: cycle.last_remaining_percent ?? '—',
            count: cycle.observation_count,
          })}
        </small>
      </div>
      <div className={styles.transitionHeader} aria-hidden="true">
        <span>{t('usage_stats.credentials_quota_history_change')}</span>
        <span>{t('usage_stats.credentials_quota_history_interval')}</span>
        <span>{t('usage_stats.credentials_quota_history_usage')}</span>
        <span>{t('usage_stats.credentials_quota_history_efficiency')}</span>
      </div>
      {cycle.transitions.length === 0 ? (
        <div className={styles.noCycleTransitions}>{t('usage_stats.credentials_quota_history_no_transition')}</div>
      ) : cycle.transitions.map((transition, index) => (
        <TransitionRow key={`${transition.interval_started_at}:${transition.to_remaining_percent}:${index}`} transition={transition} locale={locale} />
      ))}
      <div className={`${styles.boundaryRow} ${styles.endBoundary}`.trim()}>
        <span>{t(cycle.status === 'current'
          ? 'usage_stats.credentials_quota_history_cycle_expected_reset'
          : 'usage_stats.credentials_quota_history_cycle_end')}</span>
        <strong>{formatDateTime(cycle.status === 'current' ? cycle.reset_at : cycle.effective_ended_at, locale)}</strong>
        <CycleQuotaSummary status={cycle.status} summary={summary} />
      </div>
    </article>
  )
}

function CycleQuotaSummary({
  status,
  summary,
}: {
  status: CodexQuotaHistoryCycle['status']
  summary: QuotaCycleSummary | null
}) {
  const { t } = useTranslation()
  const isCurrent = status === 'current'
  return (
    <dl
      className={`${styles.cycleSummary} ${isCurrent ? styles.currentCycleSummary : styles.completedCycleSummary}`.trim()}
      data-codex-quota-cycle-summary="true"
    >
      <QuotaSummaryRow kind="median" label={t('usage_stats.credentials_quota_history_median_per_point')} metrics={summary?.median ?? null} />
      {isCurrent ? <>
        <QuotaSummaryRow
          kind="used"
          label={t('usage_stats.credentials_quota_history_used')}
          metrics={summary?.used ?? null}
        />
        <QuotaSummaryRow kind="full-estimate" label={t('usage_stats.credentials_quota_history_full_estimate')} metrics={summary?.fullEstimate ?? null} />
      </> : <>
        <QuotaSummaryRow kind="full-estimate" label={t('usage_stats.credentials_quota_history_full_estimate')} metrics={summary?.fullEstimate ?? null} />
        <QuotaSummaryRow
          kind="used"
          label={t('usage_stats.credentials_quota_history_used')}
          metrics={summary?.used ?? null}
        />
        <QuotaSummaryRow
          kind="estimated-unused"
          label={t('usage_stats.credentials_quota_history_estimated_unused')}
          metrics={summary?.estimatedUnused ?? null}
          leadingMetric="percentage"
        />
      </>}
    </dl>
  )
}

function TransitionRow({ transition, locale }: { transition: CodexQuotaHistoryTransition; locale?: string }) {
  const { t } = useTranslation()
  return (
    <div className={styles.transitionRow} data-codex-quota-transition={transition.is_direct ? 'direct' : 'cross'}>
      <div className={styles.changeCell}>
        <strong>{transition.from_remaining_percent}% → {transition.to_remaining_percent}%</strong>
        <span className={transition.is_direct ? styles.directBadge : styles.crossBadge}>
          {transition.is_direct
            ? t('usage_stats.credentials_quota_history_direct')
            : t('usage_stats.credentials_quota_history_cross_points', { count: transition.percentage_points })}
        </span>
      </div>
      <div className={styles.intervalCell}>
        <span>{formatDateTime(transition.interval_started_at, locale)}</span>
        <span>→ {formatDateTime(transition.interval_ended_at, locale)}</span>
      </div>
      <div className={styles.valueCell}>
        <strong>{formatCompactNumber(transition.usage.total_tokens)} Token</strong>
        <span>{transition.usage.cost_available ? formatUsd(transition.usage.total_cost_usd) : t('usage_stats.credentials_quota_history_cost_missing')}</span>
      </div>
      <div className={styles.valueCell}>
        <strong>{formatCompactNumber(transition.tokens_per_point)} Token/1%</strong>
        <span>{transition.cost_per_point_available ? `${formatUsd(transition.cost_per_point)}/1%` : t('usage_stats.credentials_quota_history_cost_missing')}</span>
      </div>
    </div>
  )
}

function buildEfficiencyChart(
  transitions: CodexQuotaHistoryTransition[],
  isDark: boolean,
  t: (key: string) => string,
  locale?: string,
): {
  data: QuotaEfficiencyChartData
  options: QuotaEfficiencyChartOptions
  hasUnavailableCost: boolean
} {
  const points = expandEfficiencyPoints(transitions)
  const tokenValues = points.map(({ transition }) => transition.tokens_per_point)
  const costValues = points.map(({ transition }) => transition.cost_per_point_available ? transition.cost_per_point : null)
  const hasUnavailableCost = transitions.some((transition) => !transition.cost_per_point_available)
  const chartTheme = getUsageChartTheme(isDark)
  const text = chartTheme.textPrimary
  const muted = chartTheme.textSecondary
  const grid = chartTheme.grid
  return {
    data: {
      labels: points.map((point) => point.label),
      datasets: [
        {
          type: 'bar',
          label: t('usage_stats.credentials_quota_history_tokens_per_point'),
          data: tokenValues,
          yAxisID: 'tokens',
          backgroundColor: (context) => {
            const transition = points[context.dataIndex]?.transition
            const color = transition?.is_direct ? QUOTA_EFFICIENCY_BAR_COLORS.direct : QUOTA_EFFICIENCY_BAR_COLORS.averaged
            return toUsageChartGradientFill(context, color)
          },
          borderColor: points.map(({ transition }) => transition.is_direct
            ? QUOTA_EFFICIENCY_BAR_COLORS.direct.base
            : QUOTA_EFFICIENCY_BAR_COLORS.averaged.base),
          order: 2,
        },
        {
          type: 'line',
          label: t('usage_stats.credentials_quota_history_cost_per_point'),
          data: costValues,
          yAxisID: 'cost',
          borderColor: USAGE_CHART_REQUESTS_LINE_COLOR,
          backgroundColor: USAGE_CHART_REQUESTS_LINE_COLOR,
          pointBackgroundColor: USAGE_CHART_REQUESTS_LINE_COLOR,
          pointBorderColor: isDark ? '#111827' : '#ffffff',
          pointBorderWidth: 2,
          // 连续折线不画圆点；只有无法形成线段的单点才保留可见标记。
          pointRadius: (context) => isIsolatedCostPoint(costValues, context.dataIndex) ? 3 : 0,
          pointHoverRadius: 5,
          borderWidth: 2,
          borderDash: [6, 4],
          tension: 0.24,
          spanGaps: false,
          order: 1,
        },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...buildUsageChartTooltipStyle(chartTheme),
          displayColors: false,
          callbacks: {
            label: (context) => {
              if (context.datasetIndex !== 0) return []
              const point = points[context.dataIndex]
              if (!point) return []
              return [
                `${t('usage_stats.credentials_quota_history_tokens_per_point')}: ${formatCompactNumber(point.transition.tokens_per_point)}`,
                `${t('usage_stats.credentials_quota_history_cost_per_point')}: ${point.transition.cost_per_point_available
                  ? formatUsd(point.transition.cost_per_point)
                  : t('usage_stats.credentials_quota_history_cost_missing')}`,
              ]
            },
            afterBody: (items) => {
              const point = points[items[0]?.dataIndex]
              if (!point) return []
              const interval = `${t('usage_stats.credentials_quota_history_interval')}: ${formatDateTime(point.transition.interval_started_at, locale)} → ${formatDateTime(point.transition.interval_ended_at, locale)}`
              // Direct 与跨百分点样本统一显示真实观察时间；跨百分点再补充整段变化和总用量。
              if (point.transition.is_direct) return [interval]
              return [
                `${t('usage_stats.credentials_quota_history_change')}: ${point.transition.from_remaining_percent}% → ${point.transition.to_remaining_percent}%`,
                interval,
                `${t('usage_stats.total_tokens')}: ${formatCompactNumber(point.transition.usage.total_tokens)} Token`,
                `${t('usage_stats.total_cost')}: ${point.transition.usage.cost_available
                  ? formatUsd(point.transition.usage.total_cost_usd)
                  : t('usage_stats.credentials_quota_history_cost_missing')}`,
              ]
            },
          },
        },
      },
      scales: {
        x: {
          ticks: { color: muted, font: { size: 10 }, autoSkip: true, maxTicksLimit: 8, maxRotation: 0, minRotation: 0 },
          grid: { display: false },
          border: { color: grid },
        },
        tokens: {
          position: 'left',
          beginAtZero: true,
          ticks: { color: muted, font: { size: 10 }, maxTicksLimit: 5, callback: (value) => formatCompactNumber(Number(value)) },
          grid: { color: grid },
          border: { display: false },
          title: { display: true, color: text, text: 'Token/1%' },
        },
        cost: {
          position: 'right',
          beginAtZero: true,
          ticks: { color: muted, font: { size: 10 }, maxTicksLimit: 5, callback: (value) => formatUsd(Number(value)) },
          grid: { drawOnChartArea: false },
          border: { display: false },
          title: { display: true, color: text, text: 'USD/1%' },
        },
      },
    },
    hasUnavailableCost,
  }
}

function isIsolatedCostPoint(values: Array<number | null>, index: number): boolean {
  if (values[index] == null) return false
  const hasPrevious = index > 0 && values[index - 1] != null
  const hasNext = index < values.length - 1 && values[index + 1] != null
  return !hasPrevious && !hasNext
}

function expandEfficiencyPoints(transitions: CodexQuotaHistoryTransition[]): QuotaEfficiencyPoint[] {
  // 跨百分点样本只有整个观察区间的用量，因此拆分后的每个 1% 共用同一份区间平均值。
  return transitions.flatMap((transition) => Array.from({ length: transition.percentage_points }, (_, offset) => {
    const from = transition.from_remaining_percent - offset
    return {
      label: `${from}% → ${from - 1}%`,
      transition,
    }
  }))
}

function calculateMedian(values: number[]): number | null {
  if (values.length === 0) return null
  const sorted = [...values].sort((left, right) => left - right)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 0 ? (sorted[middle - 1] + sorted[middle]) / 2 : sorted[middle]
}

function sameQuotaWindow(left: CodexQuotaHistoryWindow, right: CodexQuotaHistoryWindow | null): boolean {
  return right != null && left.window_role === right.window_role
}

function formatWindowLabel(window: CodexQuotaHistoryWindow, t: (key: string) => string): string {
  return window.window_kind ? t(`usage_stats.credentials_quota_history_window_${window.window_kind}`) : formatWindowDuration(window.window_seconds)
}

function formatCycleWindowLabel(seconds: number, t: (key: string) => string): string {
  switch (seconds) {
    case 5 * 60 * 60:
      return t('usage_stats.credentials_quota_history_window_five_hour')
    case 7 * 24 * 60 * 60:
      return t('usage_stats.credentials_quota_history_window_weekly')
    case 30 * 24 * 60 * 60:
    case 365 * 24 * 60 * 60 / 12:
      return t('usage_stats.credentials_quota_history_window_monthly')
    default:
      return formatWindowDuration(seconds)
  }
}

function formatWindowDuration(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400}d`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  return `${seconds}s`
}

function formatDateTime(value: string, locale?: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value || '—'
  // API 时间已经使用项目 TZ；把原墙上时间映射到 UTC 仅供 Intl 本地化排版，避免浏览器再次换区。
  const offsetTime = value.trim().match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i)
  const displayDate = offsetTime
    ? new Date(Date.UTC(
      Number(offsetTime[1]),
      Number(offsetTime[2]) - 1,
      Number(offsetTime[3]),
      Number(offsetTime[4]),
      Number(offsetTime[5]),
      Number(offsetTime[6]),
    ))
    : date
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    ...(offsetTime ? { timeZone: 'UTC' } : {}),
  }).format(displayDate)
}
