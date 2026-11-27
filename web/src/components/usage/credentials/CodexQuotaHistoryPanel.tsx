import { ChartLifecycleProvider, useChartRefresh } from '../charts/chartLifecycle'
import { createContext, memo, useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import type { CategoryScaleOptions, ChartData, ChartOptions, LinearScaleOptions } from 'chart.js'
import { Chart } from '../charts/UsageChart'
import '@/lib/chartjs'
import quotaCostIcon from '@/assets/icons/quota-cost.svg'
import quotaRequestIcon from '@/assets/icons/quota-request.svg'
import quotaTokenIcon from '@/assets/icons/quota-token.svg'
import quotaUnusedIcon from '@/assets/icons/quota-unused.svg'
import { ApiError, deleteCodexQuotaHistoryCycle, fetchCodexQuotaHistory, isCostsBusy, type FetchCodexQuotaHistoryOptions } from '@/lib/api'
import { Button } from '@/components/ui/Button'
import { ChartSurface } from '@/components/ui/ChartSurface'
import { Modal } from '@/components/ui/Modal'
import { PillSwitcher } from '@/components/ui/PillSwitcher'
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import { IconCheck, IconChevronDown, IconTrash2 } from '@/components/ui/icons'
import type { CodexQuotaHistoryCycle, CodexQuotaHistoryResponse, CodexQuotaHistoryTransition, CodexQuotaHistoryWindow } from '@/lib/types'
import { useThemeStore } from '@/stores'
import { formatCompactNumber, formatUsd } from '@/utils/usage'
import { buildUsageChartTooltipStyle, getUsageChartTheme, toUsageChartGradientFill, USAGE_CHART_TOKEN_COLORS, type UsageChartGradientColor } from '@/utils/usage/chartConfig'
import { quotaRemainingStatus } from './credentialViewModels'
import { createQuotaHistoryDateTimeFormatter, formatQuotaSummaryPercentage, formatQuotaSummaryRequests } from './quotaHistoryFormatters'
import { createQuotaHistoryTimeFormatter, type QuotaHistoryTimeFormatter } from './quotaHistoryTimeLabels'
import styles from './CodexQuotaHistoryPanel.module.scss'

type QuotaEfficiencyChartType = 'bar' | 'line'
type QuotaEfficiencyChartData = ChartData<QuotaEfficiencyChartType, Array<number | null>, string>
type QuotaEfficiencyChartOptions = ChartOptions<QuotaEfficiencyChartType>

interface QuotaEfficiencyPoint {
  label: string
  remainingPercent: number
  transition: CodexQuotaHistoryTransition
}

interface QuotaSummaryMetrics {
  requests: number
  tokens: number
  cost: number
  costAvailable: boolean
  costStale?: boolean
  percentage?: number
}

interface QuotaCycleSummary {
  median: QuotaSummaryMetrics | null
  used: QuotaSummaryMetrics | null
  fullEstimate: QuotaSummaryMetrics | null
  estimatedUnused: QuotaSummaryMetrics | null
}

type QuotaSummaryKind = 'used' | 'full-estimate' | 'median' | 'estimated-unused'

const QUOTA_EFFICIENCY_BAR_COLORS = {
  // 对齐全站 #727 调色板：1% 直接样本用主蓝渐变，跨档均值用青绿渐变，彻底避开费用线暖色系。
  direct: USAGE_CHART_TOKEN_COLORS.input,
  averaged: USAGE_CHART_TOKEN_COLORS.cacheRead,
} satisfies Record<'direct' | 'averaged', UsageChartGradientColor>

const QUOTA_COST_LINE_COLORS = {
  // 费用折线对齐琥珀暖色系，亮暗主题分别保证足够对比度。
  light: '#d97706',
  dark: '#fbbf24',
} as const

const QUOTA_REMAINING_LINE_COLORS = {
  // 低饱和冷灰承担剩余百分比趋势衬底，配合极淡渐变面积稳住视觉重心。
  light: 'rgba(100, 116, 139, 0.72)',
  dark: 'rgba(148, 163, 184, 0.72)',
} as const

const QUOTA_REMAINING_AREA_STOPS = {
  light: 'rgba(100, 116, 139, 0.11)',
  dark: 'rgba(148, 163, 184, 0.13)',
} as const

interface CodexQuotaHistoryPanelProps {
  authIndex: string
  onAuthRequired?: () => void
}

type QuotaHistoryTooltipController = {
  tooltipId: string
  activeTargetId: string | null
  isOpen: boolean
  showOnMouseEnter: (lines: string[], anchor: HTMLElement, targetId?: string) => void
  hideOnMouseLeave: (anchor: HTMLElement) => void
  showOnFocus: (lines: string[], anchor: HTMLElement, targetId?: string) => void
  hideOnBlur: (anchor: HTMLElement) => void
  dismiss: () => boolean
}

const QuotaHistoryTooltipContext = createContext<QuotaHistoryTooltipController | null>(null)

export function CodexQuotaHistoryPanel({ authIndex, onAuthRequired }: CodexQuotaHistoryPanelProps) {
  // 账号切换立即隔离选择、展开和请求状态，不让旧周期在新账号下短暂出现。
  return <QuotaHistoryPanel key={authIndex.trim()} authIndex={authIndex} onAuthRequired={onAuthRequired} />
}

function QuotaHistoryPanel({ authIndex, onAuthRequired }: CodexQuotaHistoryPanelProps) {
  const { t, i18n } = useTranslation()
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme)
  const [history, setHistory] = useState<CodexQuotaHistoryResponse | null>(null)
  const [selection, setSelection] = useState<{ windowRole: string | null; cycleId: number | null }>({ windowRole: null, cycleId: null })
  const [loading, setLoading] = useState(false)
  const [chartRefresh, markChartLoaded] = useChartRefresh()
  const [error, setError] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<CodexQuotaHistoryCycle | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const deleteControllerRef = useRef<AbortController | null>(null)
  const controllerRef = useRef<AbortController | null>(null)
  const lastRequestOptionsRef = useRef<FetchCodexQuotaHistoryOptions>({})
  const chartRef = useRef<HTMLElement | null>(null)
  const activeAnchorRef = useRef<HTMLElement | null>(null)
  const [activeTargetId, setActiveTargetId] = useState<string | null>(null)
  const tooltipId = `quota-history-tooltip-${useId()}`
  const {
    tooltip,
    showOnMouseEnter: rawShowOnMouseEnter,
    hideOnMouseLeave,
    showOnFocus: rawShowOnFocus,
    hideOnBlur,
    dismiss: rawDismissTooltip,
  } = usePortalTooltip({ trackAnchor: true })
  const showOnMouseEnter = useCallback((lines: string[], anchor: HTMLElement, targetId?: string) => {
    activeAnchorRef.current = anchor
    setActiveTargetId(targetId ?? null)
    rawShowOnMouseEnter(lines, anchor)
  }, [rawShowOnMouseEnter])
  const showOnFocus = useCallback((lines: string[], anchor: HTMLElement, targetId?: string) => {
    activeAnchorRef.current = anchor
    setActiveTargetId(targetId ?? null)
    rawShowOnFocus(lines, anchor)
  }, [rawShowOnFocus])
  const dismissTooltip = useCallback(() => {
    activeAnchorRef.current = null
    setActiveTargetId(null)
    return rawDismissTooltip()
  }, [rawDismissTooltip])
  const tooltipOpen = tooltip !== null
  useEffect(() => {
    if (!tooltipOpen) return
    // 在 document 捕获阶段处理 Escape 与外部点击，确保焦点停留在抽屉标签栏时按 Escape 只关浮层而不误关抽屉。
    const handlePointerDown = (event: PointerEvent) => {
      if (!activeAnchorRef.current?.contains(event.target as Node)) {
        dismissTooltip()
      }
    }
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismissTooltip()) {
        event.preventDefault()
        event.stopPropagation()
      }
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    document.addEventListener('keydown', handleEscape, true)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true)
      document.removeEventListener('keydown', handleEscape, true)
    }
  }, [tooltipOpen, dismissTooltip])
  const tooltipController = useMemo<QuotaHistoryTooltipController>(() => ({
    tooltipId,
    activeTargetId,
    isOpen: tooltipOpen,
    showOnMouseEnter,
    hideOnMouseLeave,
    showOnFocus,
    hideOnBlur,
    dismiss: dismissTooltip,
  }), [tooltipId, activeTargetId, tooltipOpen, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismissTooltip])

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
      let response = await fetchCodexQuotaHistory(normalizedAuthIndex, requestOptions, controller.signal)
      if (controllerRef.current !== controller) return
      // 删除某角色的最后一个周期后，原选择可能消失；回到剩余窗口的默认选择。
      if (requestOptions.windowRole && !response.selected_window && response.windows.length > 0) {
        lastRequestOptionsRef.current = {}
        response = await fetchCodexQuotaHistory(normalizedAuthIndex, {}, controller.signal)
      }
      if (controllerRef.current !== controller) return
      // 同一角色刷新保留有效周期；角色变化或选中周期被删除时，回到当前周期或最新历史周期。
      const windowRole = response.selected_window?.window_role ?? null
      const cycles = response.cycles
      setSelection((previous) => ({
        windowRole,
        cycleId: previous.windowRole === windowRole && cycles.some((cycle) => cycle.id === previous.cycleId)
          ? previous.cycleId
          : (cycles.find((cycle) => cycle.status === 'current') ?? cycles[0])?.id ?? null,
      }))
      setHistory(response)
      markChartLoaded(response)
    } catch (loadError) {
      if (controller.signal.aborted) return
      if (loadError instanceof ApiError && loadError.status === 401) {
        onAuthRequired?.()
        return
      }
      // 同一账号费用暂缓时继续显示上次成功的周期，保留原刷新入口重试。
      setError(isCostsBusy(loadError) ? t('usage_stats.costs_busy')
        : loadError instanceof Error ? loadError.message : t('usage_stats.credentials_quota_history_load_failed'))
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null
        setLoading(false)
      }
    }
  }, [authIndex, onAuthRequired, t, markChartLoaded])

  useEffect(() => {
    lastRequestOptionsRef.current = {}
    setHistory(null)
    setDeleteTarget(null)
    setDeleteError('')
    setDeleting(false)
    void loadHistory()
    return () => {
      controllerRef.current?.abort()
      controllerRef.current = null
      deleteControllerRef.current?.abort()
      deleteControllerRef.current = null
    }
  }, [loadHistory])

  const confirmDelete = async () => {
    if (!deleteTarget || deleteControllerRef.current) return
    const windowRole = history?.selected_window?.window_role
    const controller = new AbortController()
    deleteControllerRef.current = controller
    setDeleting(true)
    setDeleteError('')
    try {
      await deleteCodexQuotaHistoryCycle(authIndex.trim(), deleteTarget.id, controller.signal)
      // 账号切换或抽屉关闭后的旧响应不能刷新另一个账号。
      if (deleteControllerRef.current !== controller) return
      setDeleteTarget(null)
      setHistory(null)
      await loadHistory(windowRole ? { windowRole } : lastRequestOptionsRef.current)
    } catch (deleteFailure) {
      if (controller.signal.aborted) return
      if (deleteFailure instanceof ApiError && deleteFailure.status === 401) {
        onAuthRequired?.()
        return
      }
      setDeleteError(deleteFailure instanceof Error ? deleteFailure.message : t('usage_stats.credentials_quota_history_delete_failed'))
    } finally {
      if (deleteControllerRef.current === controller) {
        deleteControllerRef.current = null
        setDeleting(false)
      }
    }
  }

  const closeDelete = () => {
    if (deleteControllerRef.current) return
    setDeleteTarget(null)
    setDeleteError('')
  }

  const selectedWindowRole = history?.selected_window?.window_role ?? null
  const selectCycle = useCallback((cycleId: number) => {
    setSelection((previous) => previous.windowRole === selectedWindowRole && previous.cycleId === cycleId
      ? previous : { windowRole: selectedWindowRole, cycleId })
  }, [selectedWindowRole])
  const openDelete = useCallback((cycle: CodexQuotaHistoryCycle) => {
    setDeleteError('')
    setDeleteTarget(cycle)
  }, [])
  const returnToChart = useCallback(() => {
    const chart = chartRef.current
    if (!chart) return
    const behavior = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth'
    const body = chart.closest<HTMLElement>('.modal-body')
    const tabs = body?.querySelector<HTMLElement>('[data-credential-detail-tab-bar]')
    // 按真实抽屉标签栏的实际高度定位，只调整本次返回，不影响其他面板的滚动。
    if (body && tabs) {
      const top = body.scrollTop + chart.getBoundingClientRect().top - body.getBoundingClientRect().top
        - body.clientTop - tabs.getBoundingClientRect().height - 12
      body.scrollTo({ top: Math.max(0, top), behavior })
    } else {
      chart.scrollIntoView({ block: 'start', behavior })
    }
    chart.focus({ preventScroll: true })
  }, [])

  const locale = i18n?.resolvedLanguage || i18n?.language
  const formatTime = useMemo(
    () => createQuotaHistoryTimeFormatter(history?.cycles ?? [], createQuotaHistoryDateTimeFormatter(locale)),
    [history, locale],
  )
  // 周期状态和新旧顺序沿用后端；图表只读取所选角色中稳定 id 对应的周期。
  const selectedCycle = history?.cycles.find((cycle) => cycle.id === selection.cycleId) ?? null
  const cycleSummaries = useMemo(() => new Map(
    (history?.cycles ?? []).map((cycle) => [cycle.id, buildQuotaCycleSummary(cycle)]),
  ), [history])

  return (
    <QuotaHistoryTooltipContext.Provider value={tooltipController}>
      <div className={styles.panel} data-codex-quota-history-panel="true">
      {history && history.windows.length > 1 ? (
        <PillSwitcher
          variant="toolbar"
          className={styles.windowSwitcher}
          ariaLabel={t('usage_stats.credentials_quota_history_window_selector')}
          value={history.selected_window?.window_role ?? null}
          onChange={(windowRole) => void loadHistory({ windowRole })}
          options={history.windows.map((window) => {
            const selected = sameQuotaWindow(window, history.selected_window)
            const baseLabel = formatWindowLabel(window, t)
            const duplicate = history.windows.some((other) => other.window_role !== window.window_role && formatWindowLabel(other, t) === baseLabel)
            return {
              value: window.window_role,
              label: `${baseLabel}${duplicate ? ` · ${window.window_role === 'primary' ? 'Primary' : 'Secondary'}` : ''}`,
              disabled: deleting || (loading && selected),
            }
          })}
        />
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
          <ChartLifecycleProvider queryKey={JSON.stringify([authIndex, history.selected_window?.window_role, selectedCycle?.id])}
            source={selectedCycle} refreshSignal={chartRefresh?.source === history && selectedCycle ? { source: selectedCycle, revision: chartRefresh.revision } : undefined}>
          <CycleEfficiencyChart
            cycle={selectedCycle}
            window={history.selected_window}
            isDark={resolvedTheme === 'dark'}
            formatTime={formatTime}
            chartRef={chartRef}
          />
          </ChartLifecycleProvider>
          <CyclesList key={history.selected_window?.window_role ?? 'empty'}
            cycles={history.cycles} formatTime={formatTime} summaries={cycleSummaries}
            selectedCycleId={selectedCycle?.id ?? null}
            onSelect={selectCycle}
            onReturnToChart={returnToChart}
            deleteDisabled={loading || deleting}
            onDelete={openDelete} />
        </>
      ) : !error ? (
        <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_empty')}</div>
      ) : null}
      <Modal
        open={deleteTarget !== null}
        title={t('usage_stats.credentials_quota_history_delete_title')}
        onClose={closeDelete}
        closeDisabled={deleting}
        width={460}
        footer={<div className={styles.deleteActions}>
          <Button variant="secondary" appearance="action" onClick={closeDelete} disabled={deleting}>{t('common.cancel')}</Button>
          <Button variant="danger" appearance="action" onClick={() => void confirmDelete()} loading={deleting}>{t('common.delete')}</Button>
        </div>}
      >
        {deleteTarget ? <>
          <p>{t('usage_stats.credentials_quota_history_delete_description')}</p>
          <p>{t('usage_stats.credentials_quota_history_cycle_range', {
            start: formatTime(deleteTarget.window_started_at),
            end: formatTime(deleteTarget.reset_at),
          })}</p>
          <p>{t('usage_stats.credentials_quota_history_percent_summary', {
            percent: deleteTarget.last_remaining_percent ?? '—', count: deleteTarget.observation_count,
          })}</p>
        </> : null}
        {deleteError ? <p role="alert">{deleteError}</p> : null}
      </Modal>
      <PortalTooltip id={tooltipId} tooltip={tooltip} />
      </div>
    </QuotaHistoryTooltipContext.Provider>
  )
}

function CycleEfficiencyChart({
  cycle,
  window,
  isDark,
  formatTime,
  chartRef,
}: {
  cycle: CodexQuotaHistoryCycle | null
  window: CodexQuotaHistoryWindow | null
  isDark: boolean
  formatTime: QuotaHistoryTimeFormatter
  chartRef: RefObject<HTMLElement | null>
}) {
  const { t } = useTranslation()
  const titleId = useId()
  const statusLabel = t(cycle?.status === 'current'
    ? 'usage_stats.credentials_quota_history_status_current'
    : 'usage_stats.credentials_quota_history_status_completed')
  // 未完成首次结算的下降段不参与图表渲染与无障碍摘要，避免把未计算空窗当作 0。
  const readyTransitions = useMemo(
    () => (cycle?.transitions ?? []).filter(isQuotaTransitionReady),
    [cycle?.transitions],
  )
  const costStale = cycle?.cost_stale === true
  const chart = useMemo(
    () => buildEfficiencyChart(readyTransitions, isDark, t, formatTime, costStale),
    [readyTransitions, isDark, t, formatTime, costStale],
  )
  const hasIncompleteCost = Boolean(cycle && (costStale || (cycle.usage_ready !== false && !cycle.usage.cost_available) || chart.hasUnavailableCost))

  return (
    <section ref={chartRef} tabIndex={-1} aria-labelledby={titleId}
      className={styles.card} data-codex-quota-chart-cycle-id={cycle?.id}>
      <header className={styles.cardHeader}>
        <div className={styles.chartHeading}>
          <h3 id={titleId}>
            {t('usage_stats.credentials_quota_history_efficiency_title')}
            {cycle ? ` · ${formatCycleWindowLabel(cycle.window_seconds, t)}` : window ? ` · ${formatWindowLabel(window, t)}` : ''}
            {cycle ? <span className={cycle.status === 'current' ? styles.currentStatus : styles.completedStatus}>{statusLabel}</span> : null}
          </h3>
          <p className={styles.chartMeta}>
            {cycle
              ? <>
                <span className={styles.chartCycleRange} data-codex-quota-cycle-range="true">
                  {t('usage_stats.credentials_quota_history_cycle_range', {
                    start: formatTime(cycle.effective_started_at),
                    end: formatTime(cycle.status === 'current' ? cycle.reset_at : cycle.effective_ended_at),
                  })}
                </span>
                <span className={styles.chartObservedRange} data-codex-quota-observed-range="true">
                  {t('usage_stats.credentials_quota_history_observed_range', {
                    start: formatTime(cycle.first_observed_at),
                    end: formatTime(cycle.last_observed_at),
                  })}
                </span>
              </>
              : t('usage_stats.credentials_quota_history_empty')}
          </p>
        </div>
        {cycle ? (
          <div className={styles.chartStatus}>
            <dl className={styles.chartRemaining} data-status={quotaRemainingStatus(cycle.last_remaining_percent)}>
              <dt>{t(cycle.status === 'current'
                ? 'usage_stats.credentials_quota_history_current_remaining'
                : 'usage_stats.credentials_quota_history_last_remaining')}</dt>
              <dd>
                {cycle.last_remaining_percent ?? '—'}
                {cycle.last_remaining_percent !== null ? <span>%</span> : null}
              </dd>
            </dl>
            {hasIncompleteCost ? (
              <small className={styles.costHeaderHint} data-codex-quota-cost-warning="true">
                {t('usage_stats.credentials_quota_history_cost_unavailable')}
              </small>
            ) : null}
          </div>
        ) : null}
      </header>
      {!cycle ? (
        <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_empty')}</div>
      ) : (
        <>
          {readyTransitions.length === 0 ? (
            <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_no_transition')}</div>
          ) : (
            <ChartSurface className={styles.chartSurface} data-codex-quota-chart-surface="true">
              <div className={styles.chartFrame} data-codex-quota-efficiency-chart="combined" aria-hidden="true">
                <Chart type="bar" data={chart.data} options={chart.options} />
              </div>
              <CycleAccessibleSummary transitions={readyTransitions} formatTime={formatTime} titleId={titleId} costStale={costStale} />
              <div className={styles.chartLegend}>
                <span><i className={styles.directDot} />{t('usage_stats.credentials_quota_history_direct')}</span>
                <span><i className={styles.crossDot} />{t('usage_stats.credentials_quota_history_cross')}</span>
                <span>
                  <i
                    className={styles.remainingLine}
                    data-codex-quota-remaining-legend="true"
                    style={{ '--quota-remaining-line-color': isDark ? QUOTA_REMAINING_LINE_COLORS.dark : QUOTA_REMAINING_LINE_COLORS.light } as CSSProperties}
                  />
                  {t('usage_stats.credentials_quota_history_remaining_percentage')}
                </span>
                <span>
                  <i
                    className={styles.costLine}
                    data-codex-quota-cost-legend="true"
                    style={{ '--quota-cost-line-color': isDark ? QUOTA_COST_LINE_COLORS.dark : QUOTA_COST_LINE_COLORS.light } as CSSProperties}
                  />
                  {t('usage_stats.credentials_quota_history_cost_per_point')}
                </span>
              </div>
            </ChartSurface>
          )}
        </>
      )}
    </section>
  )
}

function QuotaSummaryRow({
  kind,
  label,
  metrics,
  leadingMetric = 'requests',
  emphasis = 'secondary',
}: {
  kind: QuotaSummaryKind
  label: string
  metrics: QuotaSummaryMetrics | null
  leadingMetric?: 'requests' | 'percentage'
  emphasis?: 'primary' | 'secondary'
}) {
  const { t } = useTranslation()
  const formattedCost = metrics ? formatUsd(metrics.cost) : ''
  const incompleteNote = t('usage_stats.credentials_quota_history_cost_incomplete')
  const costLabel = t('usage_stats.total_cost')
  const costInteractive = Boolean(metrics && !metrics.costStale && !metrics.costAvailable)
  const values = metrics ? {
    requests: formatQuotaSummaryRequests(metrics.requests),
    percentage: metrics.percentage == null ? '—' : formatQuotaSummaryPercentage(metrics.percentage),
    tokens: formatCompactNumber(metrics.tokens),
    cost: (
      <QuotaCostValue
        text={formattedCost}
        available={metrics.costAvailable}
        stale={metrics.costStale}
        labelPrefix={costLabel}
      />
    ),
    costAria: metrics.costStale
      ? t('usage_stats.credentials_quota_history_cost_missing')
      : metrics.costAvailable ? formattedCost : `${formattedCost} (${incompleteNote})`,
  } : null
  return (
    <div
      className={`${styles.chartSummaryRow} ${emphasis === 'primary' ? styles.primarySummaryRow : styles.secondarySummaryRow}`.trim()}
      data-codex-quota-summary={kind}
      data-emphasis={emphasis}
    >
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
          label={costLabel}
          value={values.cost}
          ariaValue={values.costAria}
          interactive={costInteractive}
        />
      </> : <span className={styles.summaryUnavailable}>—</span>}</dd>
    </div>
  )
}

function QuotaCostValue({
  text,
  available,
  stale = false,
  labelPrefix,
}: {
  text: string
  available: boolean
  stale?: boolean
  labelPrefix?: string
}) {
  const { t } = useTranslation()
  const tooltipCtx = useContext(QuotaHistoryTooltipContext)
  const targetId = useId()
  const pointerTypeRef = useRef<string | null>(null)
  if (stale) return <>{t('usage_stats.credentials_quota_history_cost_missing')}</>
  if (available) return <>{text}</>
  const incompleteHint = t('usage_stats.credentials_quota_history_cost_incomplete')
  const ariaLabel = labelPrefix ? `${labelPrefix}: ${text} (${incompleteHint})` : `${text} (${incompleteHint})`
  const isTargetOpen = Boolean(tooltipCtx?.isOpen && tooltipCtx.activeTargetId === targetId)
  return (
    <span
      className={styles.incompleteCostTarget}
      data-codex-quota-cost-incomplete="true"
      tabIndex={0}
      aria-label={ariaLabel}
      aria-describedby={isTargetOpen ? tooltipCtx?.tooltipId : undefined}
      onPointerDown={(event) => { pointerTypeRef.current = event.pointerType }}
      onMouseEnter={(event) => {
        if (pointerTypeRef.current !== 'touch') tooltipCtx?.showOnMouseEnter([incompleteHint], event.currentTarget, targetId)
      }}
      onMouseLeave={(event) => {
        if (pointerTypeRef.current !== 'touch') tooltipCtx?.hideOnMouseLeave(event.currentTarget)
      }}
      onFocus={(event) => {
        if (!pointerTypeRef.current) tooltipCtx?.showOnFocus([incompleteHint], event.currentTarget, targetId)
      }}
      onBlur={(event) => {
        pointerTypeRef.current = null
        tooltipCtx?.hideOnBlur(event.currentTarget)
      }}
      onClick={(event) => {
        if (pointerTypeRef.current === 'touch' || pointerTypeRef.current === 'pen') {
          event.stopPropagation()
          if (isTargetOpen) {
            tooltipCtx?.dismiss()
          } else {
            tooltipCtx?.showOnFocus([incompleteHint], event.currentTarget, targetId)
          }
        }
      }}
    >
      {text}
      <span className={styles.costIncomplete} aria-hidden="true">*</span>
    </span>
  )
}

function QuotaSummaryMetric({
  kind,
  icon,
  label,
  value,
  ariaValue,
  interactive = false,
}: {
  kind: 'requests' | 'percentage' | 'tokens' | 'cost'
  icon: string
  label: string
  value: ReactNode
  ariaValue?: string
  interactive?: boolean
}) {
  return (
    <span
      className={styles.chartSummaryMetric}
      data-codex-quota-summary-metric={kind}
      role={interactive ? undefined : 'img'}
      aria-label={interactive ? undefined : `${label}: ${ariaValue ?? String(value)}`}
    >
      <img src={icon} alt="" aria-hidden="true" />
      <span>{value}</span>
    </span>
  )
}

function isQuotaTransitionReady(transition: CodexQuotaHistoryTransition): boolean {
  return transition.usage_ready !== false
}

function calculateFullQuotaEstimate(transitions: CodexQuotaHistoryTransition[], costStale = false): QuotaSummaryMetrics | null {
  // 只使用已完成持久化结算且被相邻百分比观察夹住的事件；周期总量还包含首段前和尾段后的未结算用量。
  const settled = transitions.reduce((total, transition) => ({
    percentagePoints: total.percentagePoints + transition.percentage_points,
    requests: total.requests + transition.usage.requests,
    tokens: total.tokens + transition.usage.total_tokens,
    cost: total.cost + transition.usage.total_cost_usd,
    costAvailable: total.costAvailable && transition.cost_per_point_available,
  }), { percentagePoints: 0, requests: 0, tokens: 0, cost: 0, costAvailable: true })
  if (settled.percentagePoints <= 0 || settled.requests <= 0 || settled.tokens <= 0) return null
  const ratio = 100 / settled.percentagePoints
  return {
    requests: settled.requests * ratio,
    tokens: settled.tokens * ratio,
    cost: settled.cost * ratio,
    costAvailable: settled.costAvailable && !costStale,
    costStale,
  }
}

function buildQuotaCycleSummary(cycle: CodexQuotaHistoryCycle): QuotaCycleSummary {
  const readyTransitions = cycle.transitions.filter(isQuotaTransitionReady)
  const costStale = cycle.cost_stale === true
  const used: QuotaSummaryMetrics | null = cycle.usage_ready === false ? null : {
    requests: cycle.usage.requests,
    tokens: cycle.usage.total_tokens,
    cost: cycle.usage.total_cost_usd,
    costAvailable: cycle.usage.cost_available && !costStale,
    costStale,
  }
  const fullEstimate = calculateFullQuotaEstimate(readyTransitions, costStale)
  const median = calculateQuotaMedianMetrics(readyTransitions, costStale)
  const estimatedUnused = cycle.status === 'completed' && used ? subtractQuotaMetrics(fullEstimate, used) : null
  return { median, used, fullEstimate, estimatedUnused }
}

function calculateQuotaMedianMetrics(transitions: CodexQuotaHistoryTransition[], costStale = false): QuotaSummaryMetrics | null {
  const points = expandEfficiencyPoints(transitions)
  if (points.length === 0) return null
  const requestMedian = calculateMedian(points.map(({ transition }) => transition.usage.requests / transition.percentage_points))
  const tokenMedian = calculateMedian(points.map(({ transition }) => transition.tokens_per_point))
  const hasUnavailableCost = transitions.some((transition) => !transition.cost_per_point_available)
  // 普通缺价继续计算已存费用的中位数并标 *，仅在显式改价中断（costStale）时隐藏未对齐金额。
  const costMedian = costStale
    ? null
    : calculateMedian(points.map(({ transition }) => transition.cost_per_point))
  if (requestMedian == null || tokenMedian == null) return null
  return {
    requests: requestMedian,
    tokens: tokenMedian,
    cost: costMedian ?? 0,
    costAvailable: !hasUnavailableCost && costMedian != null,
    costStale,
  }
}

function subtractQuotaMetrics(
  fullEstimate: QuotaSummaryMetrics | null,
  usage: QuotaSummaryMetrics,
): QuotaSummaryMetrics | null {
  if (!fullEstimate) return null
  const unusedTokens = Math.max(fullEstimate.tokens - usage.tokens, 0)
  const costStale = Boolean(fullEstimate.costStale || usage.costStale)
  return {
    requests: Math.max(fullEstimate.requests - usage.requests, 0),
    tokens: unusedTokens,
    cost: Math.max(fullEstimate.cost - usage.cost, 0),
    costAvailable: fullEstimate.costAvailable && usage.costAvailable && !costStale,
    costStale,
    percentage: fullEstimate.tokens > 0 ? (unusedTokens / fullEstimate.tokens) * 100 : 0,
  }
}

function CycleAccessibleSummary({
  transitions,
  formatTime,
  titleId,
  costStale = false,
}: {
  transitions: CodexQuotaHistoryTransition[]
  formatTime: QuotaHistoryTimeFormatter
  titleId: string
  costStale?: boolean
}) {
  const { t } = useTranslation()
  return (
    <ul
      className={styles.screenReaderOnly}
      data-codex-quota-accessible-summary="true"
      aria-labelledby={titleId}
    >
      {transitions.map((transition, index) => (
        <li key={`${transition.interval_started_at}:${transition.to_remaining_percent}:${index}`}>
          <span>{transition.from_remaining_percent}% → {transition.to_remaining_percent}%.</span>{' '}
          <span>
            {t('usage_stats.credentials_quota_history_interval')}: {formatTime(transition.interval_started_at)} → {formatTime(transition.interval_ended_at)}.
          </span>{' '}
          <span>
            {transition.is_direct
              ? t('usage_stats.credentials_quota_history_direct')
              : t('usage_stats.credentials_quota_history_cross_points', { count: transition.percentage_points })}.
          </span>{' '}
          <span>{t('usage_stats.credentials_quota_history_tokens_per_point')}: {formatCompactNumber(transition.tokens_per_point)} Token/1%.</span>{' '}
          <span>
            {t('usage_stats.credentials_quota_history_cost_per_point')}: {costStale
              ? t('usage_stats.credentials_quota_history_cost_missing')
              : `${formatUsd(transition.cost_per_point)}/1%${transition.cost_per_point_available ? '' : ` (${t('usage_stats.credentials_quota_history_cost_incomplete')})`}`}.
          </span>{' '}
          {!transition.is_direct ? (
            <span>
              {t('usage_stats.total_tokens')}: {formatCompactNumber(transition.usage.total_tokens)} Token.{' '}
              {t('usage_stats.total_cost')}: {costStale
                ? t('usage_stats.credentials_quota_history_cost_missing')
                : `${formatUsd(transition.usage.total_cost_usd)}${transition.usage.cost_available ? '' : ` (${t('usage_stats.credentials_quota_history_cost_incomplete')})`}`}.
            </span>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

function CyclesList({
  cycles,
  formatTime,
  summaries,
  onDelete,
  deleteDisabled,
  selectedCycleId,
  onSelect,
  onReturnToChart,
}: {
  cycles: CodexQuotaHistoryCycle[]
  formatTime: QuotaHistoryTimeFormatter
  summaries: Map<number, QuotaCycleSummary>
  onDelete: (cycle: CodexQuotaHistoryCycle) => void
  deleteDisabled: boolean
  selectedCycleId: number | null
  onSelect: (cycleId: number) => void
  onReturnToChart: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className={styles.cycleList} data-codex-quota-cycles="true">
      {cycles.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.credentials_quota_history_no_records')}</div>
      ) : (
        <>
          {cycles.map((cycle) => (
            <CycleCard key={cycle.id} cycle={cycle} formatTime={formatTime} summary={summaries.get(cycle.id) ?? null}
              selected={selectedCycleId === cycle.id} onSelect={onSelect} onReturnToChart={onReturnToChart}
              onDelete={onDelete} deleteDisabled={deleteDisabled} />
          ))}
        </>
      )}
    </div>
  )
}

const CycleCard = memo(function CycleCard({
  cycle,
  formatTime,
  summary,
  onDelete,
  deleteDisabled,
  selected,
  onSelect,
  onReturnToChart,
}: {
  cycle: CodexQuotaHistoryCycle
  formatTime: QuotaHistoryTimeFormatter
  summary: QuotaCycleSummary | null
  onDelete: (cycle: CodexQuotaHistoryCycle) => void
  deleteDisabled: boolean
  selected: boolean
  onSelect: (cycleId: number) => void
  onReturnToChart: () => void
}) {
  const { t } = useTranslation()
  const tooltipCtx = useContext(QuotaHistoryTooltipContext)
  const [expanded, setExpanded] = useState(false)
  const detailsId = useId()
  const deleteTitle = t('usage_stats.credentials_quota_history_delete_title')
  const statusLabel = cycle.status === 'current'
    ? t('usage_stats.credentials_quota_history_status_current')
    : t('usage_stats.credentials_quota_history_status_completed')
  return (
    <article
      className={styles.cycleCard}
      data-codex-quota-cycle-id={cycle.id}
      data-codex-quota-cycle-status={cycle.status}
      data-selected={selected}
    >
      {/* 标题和摘要共用选择区域；原生按钮提供键盘入口，明细和删除保留独立操作。 */}
      <div className={styles.cycleSelection} onClick={() => onSelect(cycle.id)}>
        <button type="button" className={`${styles.boundaryRow} ${styles.startBoundary}`.trim()}
          aria-pressed={selected} data-codex-quota-cycle-select="true">
          <span className={styles.screenReaderOnly}>{t('usage_stats.credentials_quota_history_cycle_label')}</span>
          <div className={styles.cycleHeaderMain}>
            <strong>
              <span className={styles.cycleTimeRange}>
                {formatTime(cycle.effective_started_at)}
                {' → '}
                {formatTime(cycle.status === 'current' ? cycle.reset_at : cycle.effective_ended_at)}
              </span>
              <i className={cycle.status === 'current' ? styles.currentStatus : styles.completedStatus}>{statusLabel}</i>
              {selected ? <span className={styles.selectedIndicator}><IconCheck size={11} aria-hidden="true" />{t('usage_stats.credentials_quota_history_selected')}</span> : null}
            </strong>
            <small className={styles.cycleHeaderMeta}>
              {formatCycleWindowLabel(cycle.window_seconds, t)}
              {' · '}
              {t('usage_stats.credentials_quota_history_first_observed', { value: formatTime(cycle.first_observed_at) })}
            </small>
          </div>
          <div className={styles.cycleHeaderPercent} data-status={quotaRemainingStatus(cycle.last_remaining_percent)}>
            <span className={styles.cycleRemainingValue}>
              {cycle.last_remaining_percent ?? '—'}
              {cycle.last_remaining_percent !== null ? <small>%</small> : null}
            </span>
            <small className={styles.cycleObservationMeta}>
              {t('usage_stats.credentials_quota_history_percent_summary', {
                percent: cycle.last_remaining_percent ?? '—',
                count: cycle.observation_count,
              })}
            </small>
          </div>
        </button>
        <div className={styles.cycleSummaryContainer}>
          <CycleQuotaSummary status={cycle.status} summary={summary} />
        </div>
      </div>
      <div className={styles.cycleActions}>
        <button type="button" className={styles.detailToggle} aria-expanded={expanded} aria-controls={detailsId}
          onClick={() => setExpanded((previous) => !previous)}>
          <IconChevronDown size={12} aria-hidden="true" className={styles.disclosureChevron} />
          {t(expanded ? 'usage_stats.credentials_quota_history_collapse_details' : 'usage_stats.credentials_quota_history_expand_details', {
            count: cycle.transitions.length,
          })}
        </button>
        {selected ? <button type="button" className={styles.returnToChart} onClick={onReturnToChart}>
          {t('usage_stats.credentials_quota_history_return_to_chart')}
        </button> : null}
        <Button type="button" variant="ghost" size="sm" className={styles.deleteCycle}
          onClick={() => { tooltipCtx?.dismiss(); onDelete(cycle) }} disabled={deleteDisabled}
          aria-label={deleteTitle}
          onMouseEnter={(event) => tooltipCtx?.showOnMouseEnter([deleteTitle], event.currentTarget)}
          onMouseLeave={(event) => tooltipCtx?.hideOnMouseLeave(event.currentTarget)}
          onFocus={(event) => tooltipCtx?.showOnFocus([deleteTitle], event.currentTarget)}
          onBlur={(event) => tooltipCtx?.hideOnBlur(event.currentTarget)}>
          <IconTrash2 size={15} aria-hidden="true" />
        </Button>
      </div>
      <div id={detailsId} hidden={!expanded} className={styles.transitionSection}>
        {expanded ? <>
          <div className={styles.transitionHeader} aria-hidden="true">
            <span>{t('usage_stats.credentials_quota_history_change')}</span>
            <span>{t('usage_stats.credentials_quota_history_interval')}</span>
            <span>{t('usage_stats.credentials_quota_history_usage')}</span>
            <span>{t('usage_stats.credentials_quota_history_efficiency')}</span>
          </div>
          {cycle.transitions.length === 0 ? (
            <div className={styles.noCycleTransitions}>{t('usage_stats.credentials_quota_history_no_transition')}</div>
          ) : cycle.transitions.map((transition, index) => (
            <TransitionRow key={`${transition.interval_started_at}:${transition.to_remaining_percent}:${index}`} transition={transition} formatTime={formatTime} costStale={cycle.cost_stale === true} />
          ))}
        </> : null}
      </div>
    </article>
  )
})

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
      <QuotaSummaryRow kind="median" emphasis="primary" label={t('usage_stats.credentials_quota_history_median_per_point')} metrics={summary?.median ?? null} />
      {isCurrent ? <>
        <QuotaSummaryRow
          kind="used"
          emphasis="primary"
          label={t('usage_stats.credentials_quota_history_used')}
          metrics={summary?.used ?? null}
        />
        <QuotaSummaryRow kind="full-estimate" emphasis="secondary" label={t('usage_stats.credentials_quota_history_full_estimate')} metrics={summary?.fullEstimate ?? null} />
      </> : <>
        <QuotaSummaryRow kind="full-estimate" emphasis="secondary" label={t('usage_stats.credentials_quota_history_full_estimate')} metrics={summary?.fullEstimate ?? null} />
        <QuotaSummaryRow
          kind="used"
          emphasis="primary"
          label={t('usage_stats.credentials_quota_history_used')}
          metrics={summary?.used ?? null}
        />
        <QuotaSummaryRow
          kind="estimated-unused"
          emphasis="secondary"
          label={t('usage_stats.credentials_quota_history_estimated_unused')}
          metrics={summary?.estimatedUnused ?? null}
          leadingMetric="percentage"
        />
      </>}
    </dl>
  )
}

function TransitionRow({
  transition,
  formatTime,
  costStale = false,
}: {
  transition: CodexQuotaHistoryTransition
  formatTime: QuotaHistoryTimeFormatter
  costStale?: boolean
}) {
  const { t } = useTranslation()
  const usageReady = isQuotaTransitionReady(transition)
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
        <span>{formatTime(transition.interval_started_at)}</span>
        <span>→ {formatTime(transition.interval_ended_at)}</span>
      </div>
      <div className={styles.valueCell}>
        {usageReady ? <>
          <strong>{formatCompactNumber(transition.usage.total_tokens)} Token</strong>
          <span>
            <QuotaCostValue
              text={formatUsd(transition.usage.total_cost_usd)}
              available={transition.usage.cost_available}
              stale={costStale}
            />
          </span>
        </> : <>
          <strong>—</strong>
          <span>—</span>
        </>}
      </div>
      <div className={styles.valueCell}>
        {usageReady ? <>
          <strong>{formatCompactNumber(transition.tokens_per_point)} Token/1%</strong>
          <span>
            <QuotaCostValue
              text={`${formatUsd(transition.cost_per_point)}/1%`}
              available={transition.cost_per_point_available}
              stale={costStale}
            />
          </span>
        </> : <>
          <strong>—</strong>
          <span>—</span>
        </>}
      </div>
    </div>
  )
}

function buildEfficiencyChart(
  transitions: CodexQuotaHistoryTransition[],
  isDark: boolean,
  t: (key: string) => string,
  formatTime: QuotaHistoryTimeFormatter,
  costStale = false,
): {
  data: QuotaEfficiencyChartData
  options: QuotaEfficiencyChartOptions
  hasUnavailableCost: boolean
} {
  const points = expandEfficiencyPoints(transitions)
  const tokenValues = points.map(({ transition }) => transition.tokens_per_point)
  // 普通缺价继续绘制已统计费用折线（通过星号与提示区分），仅在显式改价中断（costStale）时置空隐藏。
  const costValues = points.map(({ transition }) => costStale ? null : transition.cost_per_point)
  const hasUnavailableCost = costStale || transitions.some((transition) => !transition.cost_per_point_available)
  const chartTheme = getUsageChartTheme(isDark)
  const costLineColor = isDark ? QUOTA_COST_LINE_COLORS.dark : QUOTA_COST_LINE_COLORS.light
  const remainingLineColor = isDark ? QUOTA_REMAINING_LINE_COLORS.dark : QUOTA_REMAINING_LINE_COLORS.light
  const remainingAreaTop = isDark ? QUOTA_REMAINING_AREA_STOPS.dark : QUOTA_REMAINING_AREA_STOPS.light
  const pointHaloBorder = isDark ? '#111827' : '#ffffff'
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
          borderWidth: 0,
          borderRadius: 3,
          borderSkipped: false,
          barPercentage: 0.8,
          categoryPercentage: 0.78,
          maxBarThickness: 36,
          order: 2,
        },
        {
          type: 'line',
          label: t('usage_stats.credentials_quota_history_cost_per_point'),
          data: costValues,
          yAxisID: 'cost',
          borderColor: costLineColor,
          backgroundColor: costLineColor,
          pointBackgroundColor: costLineColor,
          pointBorderColor: pointHaloBorder,
          pointBorderWidth: 2,
          // 连续折线不画圆点；只有无法形成线段的单点才保留可见标记，悬浮带表面反差光晕。
          pointRadius: (context) => isIsolatedCostPoint(costValues, context.dataIndex) ? 3.5 : 0,
          pointHoverRadius: 4.5,
          pointHoverBorderWidth: 2,
          pointHoverBorderColor: pointHaloBorder,
          borderWidth: 2,
          borderDash: [5, 4],
          tension: 0.24,
          spanGaps: false,
          order: 1,
        },
        {
          type: 'line',
          label: t('usage_stats.credentials_quota_history_remaining_percentage'),
          data: points.map(({ remainingPercent }) => remainingPercent),
          yAxisID: 'remaining',
          borderColor: remainingLineColor,
          backgroundColor: (context) => {
            const { chart } = context
            if (!chart.chartArea) return remainingAreaTop
            const gradient = chart.ctx.createLinearGradient(0, chart.chartArea.top, 0, chart.chartArea.bottom)
            gradient.addColorStop(0, remainingAreaTop)
            gradient.addColorStop(1, 'rgba(148, 163, 184, 0)')
            return gradient
          },
          fill: 'origin',
          pointBackgroundColor: remainingLineColor,
          pointBorderColor: pointHaloBorder,
          pointBorderWidth: 1.5,
          pointRadius: 0,
          pointHoverRadius: 3.5,
          pointHoverBorderWidth: 2,
          pointHoverBorderColor: pointHaloBorder,
          borderWidth: 1.5,
          tension: 0.15,
          spanGaps: false,
          order: 3,
        },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...buildUsageChartTooltipStyle(chartTheme),
          boxPadding: 5,
          itemSort: (left, right) => left.datasetIndex - right.datasetIndex,
          callbacks: {
            // 柱体使用渐变函数且 borderWidth 为 0，显式返回纯色色标以保证 Tooltip 色点清晰可见。
            labelColor: (context) => {
              const point = points[context.dataIndex]
              if (context.datasetIndex === 0) {
                const color = point?.transition.is_direct
                  ? QUOTA_EFFICIENCY_BAR_COLORS.direct.base
                  : QUOTA_EFFICIENCY_BAR_COLORS.averaged.base
                return { borderColor: color, backgroundColor: color }
              }
              if (context.datasetIndex === 1) {
                return { borderColor: costLineColor, backgroundColor: costLineColor }
              }
              return { borderColor: remainingLineColor, backgroundColor: remainingLineColor }
            },
            label: (context) => {
              const point = points[context.dataIndex]
              if (!point) return ''
              if (context.datasetIndex === 0) {
                return `${t('usage_stats.credentials_quota_history_tokens_per_point')}: ${formatCompactNumber(point.transition.tokens_per_point)}`
              }
              if (context.datasetIndex === 1) {
                const suffix = point.transition.cost_per_point_available ? '' : '*'
                return `${t('usage_stats.credentials_quota_history_cost_per_point')}: ${formatUsd(point.transition.cost_per_point)}${suffix}`
              }
              return `${t('usage_stats.credentials_quota_history_remaining_percentage')}: ${point.remainingPercent}%`
            },
            afterBody: (items) => {
              const point = points[items[0]?.dataIndex]
              if (!point) return []
              const lines: string[] = []
              if (costStale) {
                lines.push(`${t('usage_stats.credentials_quota_history_cost_per_point')}: ${t('usage_stats.credentials_quota_history_cost_missing')}`)
              }
              const interval = `${t('usage_stats.credentials_quota_history_interval')}: ${formatTime(point.transition.interval_started_at)} → ${formatTime(point.transition.interval_ended_at)}`
              const incompleteCost = !costStale && (!point.transition.cost_per_point_available || !point.transition.usage.cost_available)
              // Direct 与跨百分点样本统一显示真实观察时间；跨百分点再补充整段变化和总用量。
              if (point.transition.is_direct) {
                lines.push(interval)
                if (incompleteCost) lines.push(t('usage_stats.credentials_quota_history_cost_incomplete'))
                return lines
              }
              lines.push(
                `${t('usage_stats.credentials_quota_history_change')}: ${point.transition.from_remaining_percent}% → ${point.transition.to_remaining_percent}%`,
                interval,
                `${t('usage_stats.total_tokens')}: ${formatCompactNumber(point.transition.usage.total_tokens)} Token`,
                `${t('usage_stats.total_cost')}: ${costStale
                  ? t('usage_stats.credentials_quota_history_cost_missing')
                  : `${formatUsd(point.transition.usage.total_cost_usd)}${point.transition.usage.cost_available ? '' : '*'}`}`,
              )
              if (incompleteCost) lines.push(t('usage_stats.credentials_quota_history_cost_incomplete'))
              return lines
            },
          },
        },
      },
      scales: {
        x: {
          afterBuildTicks: (axis) => {
            const ticks = axis.ticks
            const options = axis.options as CategoryScaleOptions
            options.ticks.maxTicksLimit = axis.width >= 560 ? 8 : axis.width >= 200 ? 4 : 3
            // 首尾作为主刻度优先保留，中间由 Chart.js 根据实际文字宽度均匀抽样。
            if (ticks.length === 0) return
            ticks[0].major = true
            ticks[ticks.length - 1].major = true
          },
          ticks: { color: muted, font: { size: 10 }, autoSkip: true, major: { enabled: true }, maxRotation: 0, minRotation: 0 },
          grid: { display: false },
          border: { color: grid },
        },
        tokens: {
          position: 'left',
          beginAtZero: true,
          beforeFit: (axis) => {
            const options = axis.options as LinearScaleOptions
            if (options.title) options.title.display = (axis.chart?.width || 600) >= 480
          },
          ticks: { color: muted, font: { size: 10 }, maxTicksLimit: 5, callback: (value) => formatCompactNumber(Number(value)) },
          grid: { color: grid },
          border: { display: false },
          title: { display: true, color: text, text: 'Token/1%' },
        },
        cost: {
          position: 'right',
          beginAtZero: true,
          beforeFit: (axis) => {
            const options = axis.options as LinearScaleOptions
            if (options.title) options.title.display = (axis.chart?.width || 600) >= 480
          },
          ticks: { color: muted, font: { size: 10 }, maxTicksLimit: 5, callback: (value) => formatUsd(Number(value)) },
          grid: { drawOnChartArea: false },
          border: { display: false },
          title: { display: true, color: text, text: 'USD/1%' },
        },
        remaining: {
          // 保留一个无视觉元素的 0–100 线性尺度；完全隐藏 display 会让
          // Chart.js 在混合图中无法为该 dataset 计算有效绘图区。
          position: 'right',
          display: true,
          min: 0,
          max: 100,
          grid: { display: false },
          border: { display: false },
          ticks: { display: false },
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
      // 柱体代表这一百分点消耗后的剩余值，下降线与 Tooltip 因此使用同一位置口径。
      remainingPercent: from - 1,
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
