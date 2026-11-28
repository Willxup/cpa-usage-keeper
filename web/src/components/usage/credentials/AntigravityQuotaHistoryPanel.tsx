import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ChartData, ChartOptions } from 'chart.js'
import { Chart } from '../charts/UsageChart'
import '@/lib/chartjs'
import { ApiError, fetchCodexQuotaHistory } from '@/lib/api'
import { Button } from '@/components/ui/Button'
import type { CodexQuotaHistoryCycle, CodexQuotaHistoryResponse } from '@/lib/types'
import { useThemeStore } from '@/stores'
import { buildUsageChartTooltipStyle, getUsageChartTheme } from '@/utils/usage/chartConfig'
import styles from './CodexQuotaHistoryPanel.module.scss'

interface AntigravityQuotaHistoryPanelProps {
  authIndex: string
  onAuthRequired?: () => void
}

interface SeriesPoint {
  at: number
  remaining: number
}

const SERIES_COLORS = ['#2563eb', '#d97706', '#059669', '#dc2626', '#7c3aed', '#0891b2', '#be185d', '#4f46e5']

export function AntigravityQuotaHistoryPanel({ authIndex, onAuthRequired }: AntigravityQuotaHistoryPanelProps) {
  return <HistoryPanel key={authIndex.trim()} authIndex={authIndex} onAuthRequired={onAuthRequired} />
}

function HistoryPanel({ authIndex, onAuthRequired }: AntigravityQuotaHistoryPanelProps) {
  const { t } = useTranslation()
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme)
  const [windows, setWindows] = useState<CodexQuotaHistoryResponse['windows']>([])
  const [cyclesByRole, setCyclesByRole] = useState<Record<string, CodexQuotaHistoryCycle[]>>({})
  const [hidden, setHidden] = useState<Record<string, boolean>>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const controllerRef = useRef<AbortController | null>(null)

  const loadHistory = useCallback(async () => {
    const normalizedAuthIndex = authIndex.trim()
    if (!normalizedAuthIndex) return
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    setLoading(true)
    setError('')
    try {
      const overview = await fetchCodexQuotaHistory(normalizedAuthIndex, {}, controller.signal)
      const roles = overview.windows.map((window) => window.window_role)
      const responses = await Promise.all(roles.map(async (role) => {
        if (overview.selected_window?.window_role === role) return [role, overview.cycles] as const
        const response = await fetchCodexQuotaHistory(normalizedAuthIndex, { windowRole: role }, controller.signal)
        return [role, response.cycles] as const
      }))
      if (controller.signal.aborted) return
      setWindows(overview.windows)
      setCyclesByRole(Object.fromEntries(responses))
    } catch (loadError) {
      if (controller.signal.aborted) return
      if (loadError instanceof ApiError && loadError.status === 401) onAuthRequired?.()
      setError(loadError instanceof Error ? loadError.message : t('usage_stats.credentials_quota_history_load_failed'))
    } finally {
      if (controllerRef.current === controller) setLoading(false)
    }
  }, [authIndex, onAuthRequired, t])

  useEffect(() => {
    void loadHistory()
    return () => controllerRef.current?.abort()
  }, [loadHistory])

  const series = useMemo(() => windows.map((window, index) => ({
    role: window.window_role,
    label: formatRole(window.window_role),
    color: SERIES_COLORS[index % SERIES_COLORS.length],
    points: pointsFor(cyclesByRole[window.window_role] ?? []),
  })).filter((item) => item.points.length > 0), [cyclesByRole, windows])

  const labels = useMemo(() => {
    const times = new Set<number>()
    series.forEach((item) => item.points.forEach((point) => times.add(point.at)))
    return [...times].sort((left, right) => left - right)
  }, [series])

  const data = useMemo<ChartData<'line', Array<number | null>, string>>(() => ({
    labels: labels.map((time) => new Date(time).toLocaleString()),
    datasets: series.filter((item) => !hidden[item.role]).map((item) => {
      const byTime = new Map(item.points.map((point) => [point.at, point.remaining]))
      return {
        label: item.label,
        data: labels.map((time) => byTime.get(time) ?? null),
        borderColor: item.color,
        backgroundColor: item.color,
        spanGaps: true,
        tension: 0.25,
        pointRadius: 2,
      }
    }),
  }), [hidden, labels, series])

  const options = useMemo<ChartOptions<'line'>>(() => {
    const theme = getUsageChartTheme(resolvedTheme === 'dark')
    return {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...buildUsageChartTooltipStyle(theme),
          callbacks: {
            label: (item) => `${item.dataset.label}: ${item.parsed.y}%`,
          },
        },
      },
      scales: {
        x: { ticks: { color: theme.textSecondary, maxRotation: 0, autoSkip: true, maxTicksLimit: 6 }, grid: { color: theme.grid } },
        y: { min: 0, max: 100, ticks: { color: theme.textSecondary, callback: (value) => `${value}%` }, grid: { color: theme.grid } },
      },
    }
  }, [resolvedTheme])

  return (
    <div className={styles.panel}>
      {loading ? <p>...</p> : null}
      {error ? <Button type="button" onClick={() => void loadHistory()}>{error}</Button> : null}
      {!loading && !error && series.length === 0 ? <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_empty')}</div> : null}
      {series.length > 0 ? (
        <>
          <div className={styles.windowSwitcher}>
            {series.map((item) => (
              <button key={item.role} type="button" className={styles.segmentButton} aria-pressed={!hidden[item.role]} onClick={() => setHidden((current) => ({ ...current, [item.role]: !current[item.role] }))}>
                <span style={{ color: item.color }}>{item.label}</span>
              </button>
            ))}
          </div>
          <div className={styles.chartFrame}>
            <Chart type="line" data={data} options={options} />
          </div>
        </>
      ) : null}
    </div>
  )
}

function pointsFor(cycles: CodexQuotaHistoryCycle[]): SeriesPoint[] {
  const points: SeriesPoint[] = []
  cycles.forEach((cycle) => {
    if (cycle.first_remaining_percent != null) points.push({ at: Date.parse(cycle.first_observed_at), remaining: cycle.first_remaining_percent })
    cycle.transitions.forEach((transition) => points.push({ at: Date.parse(transition.interval_ended_at), remaining: transition.to_remaining_percent }))
    if (cycle.last_remaining_percent != null) points.push({ at: Date.parse(cycle.last_observed_at), remaining: cycle.last_remaining_percent })
  })
  return points.filter((point) => Number.isFinite(point.at)).sort((left, right) => left.at - right.at)
}

function formatRole(role: string) {
  return role.replaceAll('antigravity-', '').replaceAll('-', ' ').replaceAll('/', ' / ')
}
