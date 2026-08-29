// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import type { ChartData, ChartOptions } from 'chart.js'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CodexQuotaHistoryResponse } from '@/lib/types'
import { useThemeStore } from '@/stores'
import { CodexQuotaHistoryPanel } from '../CodexQuotaHistoryPanel'

const fetchCodexQuotaHistory = vi.fn()
type QuotaEfficiencyChartType = 'bar' | 'line'
type QuotaEfficiencyChartData = ChartData<QuotaEfficiencyChartType, Array<number | null>, string>
type QuotaEfficiencyChartOptions = ChartOptions<QuotaEfficiencyChartType>

let latestChartData: QuotaEfficiencyChartData | null = null
let latestChartOptions: QuotaEfficiencyChartOptions | null = null

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchCodexQuotaHistory: (...args: unknown[]) => fetchCodexQuotaHistory(...args),
  }
})

vi.mock('react-chartjs-2', () => ({
  Bar: (props: { data: QuotaEfficiencyChartData; options: QuotaEfficiencyChartOptions }) => {
    latestChartData = props.data
    latestChartOptions = props.options
    return <div data-testid="quota-efficiency-chart" />
  },
  Chart: (props: { data: QuotaEfficiencyChartData; options: QuotaEfficiencyChartOptions }) => {
    latestChartData = props.data
    latestChartOptions = props.options
    return <div data-testid="quota-efficiency-chart" />
  },
}))

vi.mock('react-i18next', () => {
  const t = (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key
  return {
    initReactI18next: { type: '3rdParty', init: () => undefined },
    useTranslation: () => ({ t, i18n: { resolvedLanguage: 'en-US', language: 'en-US' } }),
  }
})

const usage = (tokens: number, cost: number, available = true, requests = 1) => ({
  requests,
  successful_requests: requests,
  failed_requests: 0,
  input_tokens: tokens,
  output_tokens: 0,
  reasoning_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  total_tokens: tokens,
  total_cost_usd: cost,
  cost_available: available,
})

const response: CodexQuotaHistoryResponse = {
  generated_at: '2026-08-21T12:00:00Z',
  range_start: '2026-07-22T12:00:00Z',
  windows: [
    { window_role: 'primary', window_kind: 'weekly', window_seconds: 604800, has_current_cycle: true, last_observed_at: '2026-08-21T11:50:00Z' },
    { window_role: 'secondary', window_kind: 'five_hour', window_seconds: 18000, has_current_cycle: true, last_observed_at: '2026-08-21T11:50:00Z' },
  ],
  selected_window: { window_role: 'primary', window_kind: 'weekly', window_seconds: 604800, has_current_cycle: true, last_observed_at: '2026-08-21T11:50:00Z' },
  cycles: [{
    id: 2,
    status: 'current',
    window_seconds: 604800,
    window_started_at: '2026-08-17T00:00:00Z',
    reset_at: '2026-08-24T00:00:00Z',
    effective_started_at: '2026-08-17T00:00:00Z',
    effective_ended_at: '2026-08-24T00:00:00Z',
    first_observed_at: '2026-08-17T02:00:00Z',
    last_observed_at: '2026-08-21T11:50:00Z',
    first_remaining_percent: 90,
    last_remaining_percent: 86,
    observation_count: 3,
    usage: usage(5000, 5, true, 14),
    transitions: [
      {
        from_remaining_percent: 90,
        to_remaining_percent: 89,
        percentage_points: 1,
        is_direct: true,
        interval_started_at: '2026-08-20T10:00:00Z',
        interval_ended_at: '2026-08-20T10:10:00Z',
        usage: usage(1000, 1, true, 10),
        tokens_per_point: 1000,
        cost_per_point: 1,
        cost_per_point_available: true,
      },
      {
        from_remaining_percent: 89,
        to_remaining_percent: 86,
        percentage_points: 3,
        is_direct: false,
        interval_started_at: '2026-08-20T11:00:00Z',
        interval_ended_at: '2026-08-20T11:30:00Z',
        usage: usage(3000, 3, true, 60),
        tokens_per_point: 1000,
        cost_per_point: 1,
        cost_per_point_available: true,
      },
    ],
  }, {
    id: 1,
    status: 'completed',
    window_seconds: 604800,
    window_started_at: '2026-08-10T00:00:00Z',
    reset_at: '2026-08-17T00:00:00Z',
    effective_started_at: '2026-08-10T00:00:00Z',
    effective_ended_at: '2026-08-17T00:00:00Z',
    first_observed_at: '2026-08-10T03:00:00Z',
    last_observed_at: '2026-08-16T23:50:00Z',
    first_remaining_percent: 93,
    last_remaining_percent: 93,
    observation_count: 2,
    usage: usage(2000, 2),
    transitions: [],
  }],
}

const cloneResponse = (): CodexQuotaHistoryResponse => JSON.parse(JSON.stringify(response)) as CodexQuotaHistoryResponse

describe('CodexQuotaHistoryPanel', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    useThemeStore.setState({ theme: 'light', resolvedTheme: 'light' })
    latestChartData = null
    latestChartOptions = null
    fetchCodexQuotaHistory.mockReset()
    fetchCodexQuotaHistory.mockResolvedValue(response)
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  it('expands crossed intervals into one-percent estimates and shows Token plus Cost together', async () => {
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
    expect(fetchCodexQuotaHistory).toHaveBeenCalledWith('codex-auth', {}, expect.any(AbortSignal))
    const windowButtons = [...document.body.querySelectorAll<HTMLButtonElement>('[aria-label="usage_stats.credentials_quota_history_window_selector"] button')]
    expect(windowButtons).toHaveLength(2)
    expect(windowButtons[0]?.textContent).toBe('usage_stats.credentials_quota_history_window_weekly')
    expect(windowButtons[1]?.textContent).toBe('usage_stats.credentials_quota_history_window_five_hour')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_role_primary')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_role_secondary')
    expect(latestChartData?.labels).toEqual(['90% → 89%', '89% → 88%', '88% → 87%', '87% → 86%'])
    expect(latestChartData?.datasets).toHaveLength(2)
    expect(latestChartData?.datasets[0]).toMatchObject({
      type: 'bar',
      yAxisID: 'tokens',
      data: [1000, 1000, 1000, 1000],
      backgroundColor: expect.any(Function),
      borderColor: ['#2563eb', '#d97706', '#d97706', '#d97706'],
    })
    expect(latestChartData?.datasets[0]).not.toHaveProperty('borderRadius')
    expect(latestChartData?.datasets[0]).not.toHaveProperty('borderSkipped')
    const barBackground = latestChartData?.datasets[0]?.backgroundColor as unknown as
      ((context: { dataIndex: number; chart: { chartArea?: undefined } }) => string) | undefined
    expect(barBackground?.({ dataIndex: 0, chart: {} })).toBe('#2563eb')
    expect(barBackground?.({ dataIndex: 1, chart: {} })).toBe('#d97706')
    expect(latestChartData?.datasets[1]).toMatchObject({
      type: 'line',
      yAxisID: 'cost',
      data: [1, 1, 1, 1],
      borderColor: '#ff5a40',
      backgroundColor: '#ff5a40',
      pointBackgroundColor: '#ff5a40',
      pointRadius: expect.any(Function),
      pointHoverRadius: 5,
      borderDash: [6, 4],
    })
    const pointRadius = latestChartData?.datasets[1]?.pointRadius as unknown as ((context: { dataIndex: number }) => number)
    expect([0, 1, 2, 3].map((dataIndex) => pointRadius({ dataIndex }))).toEqual([0, 0, 0, 0])
    expect(latestChartOptions?.scales?.x?.ticks).toMatchObject({ autoSkip: true, maxTicksLimit: 8, maxRotation: 0 })
    expect(latestChartOptions?.scales?.tokens?.ticks).toMatchObject({ maxTicksLimit: 5 })
    expect(latestChartOptions?.scales?.cost?.ticks).toMatchObject({ maxTicksLimit: 5 })
    expect(document.body.querySelector<HTMLElement>('[data-codex-quota-cost-legend]')?.style.getPropertyValue('--quota-cost-line-color')).toBe('#ff5a40')
    expect(document.body.querySelector('[aria-label="usage_stats.credentials_quota_history_metric_selector"]')).toBeNull()
    expect(document.body.querySelector('[data-codex-quota-cycle-id="2"][data-codex-quota-cycle-status="current"]')).not.toBeNull()
    expect(document.body.querySelector('[data-codex-quota-cycle-id="1"][data-codex-quota-cycle-status="completed"]')).not.toBeNull()
    const chartSummary = document.body.querySelector('[data-codex-quota-chart-summary]')
    const usedSummary = chartSummary?.querySelector('[data-codex-quota-summary="used"]')
    const fullEstimateSummary = chartSummary?.querySelector('[data-codex-quota-summary="full-estimate"]')
    const medianSummary = chartSummary?.querySelector('[data-codex-quota-summary="median"]')
    expect([...document.body.querySelectorAll('[data-codex-quota-chart-summary] > [data-codex-quota-summary]')]
      .map((summary) => summary.getAttribute('data-codex-quota-summary'))).toEqual(['median', 'used', 'full-estimate'])
    expect(usedSummary?.textContent).toContain('usage_stats.credentials_quota_history_used')
    expect(usedSummary?.querySelector('[data-codex-quota-summary-metric="requests"]')?.textContent).toBe('14')
    expect(usedSummary?.querySelector('[data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('5.00K')
    expect(usedSummary?.querySelector('[data-codex-quota-summary-metric="cost"]')?.textContent).toBe('$5.00')
    expect(fullEstimateSummary?.textContent).toContain('usage_stats.credentials_quota_history_full_estimate')
    expect(fullEstimateSummary?.querySelector('[data-codex-quota-summary-metric="requests"]')?.textContent).toBe('100')
    expect(fullEstimateSummary?.querySelector('[data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('35.71K')
    expect(fullEstimateSummary?.querySelector('[data-codex-quota-summary-metric="cost"]')?.textContent).toBe('$35.71')
    expect(medianSummary?.textContent).toContain('usage_stats.credentials_quota_history_median_per_point')
    expect(medianSummary?.querySelector('[data-codex-quota-summary-metric="requests"]')?.textContent).toBe('20')
    expect(medianSummary?.querySelector('[data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('1.00K')
    expect(medianSummary?.querySelector('[data-codex-quota-summary-metric="cost"]')?.textContent).toBe('$1.00')
    for (const summary of [usedSummary, fullEstimateSummary, medianSummary]) {
      const metrics = [...(summary?.querySelectorAll('[data-codex-quota-summary-metric]') ?? [])]
      expect(metrics.map((metric) => metric.getAttribute('data-codex-quota-summary-metric'))).toEqual(['requests', 'tokens', 'cost'])
      for (const metric of metrics) {
        const icon = metric.querySelector('img')
        expect(icon?.getAttribute('alt')).toBe('')
        expect(icon?.getAttribute('aria-hidden')).toBe('true')
      }
    }
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_cycle_start')
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_cycle_end')
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_first_observed')
    const accessibleSummary = document.body.querySelector('[data-codex-quota-current-cycle-summary]')
    expect(accessibleSummary?.textContent).toContain('90% → 89%')
    expect(accessibleSummary?.textContent).toContain('1.00K Token/1%')
    expect(accessibleSummary?.textContent).toContain('$1.00/1%')
    expect(accessibleSummary?.textContent).toContain('usage_stats.credentials_quota_history_interval: Aug 20, 11:00 AM → Aug 20, 11:30 AM')
    expect(document.body.querySelector('[data-codex-quota-efficiency-chart]')?.getAttribute('aria-hidden')).toBe('true')

    const tooltipCallbacks = latestChartOptions?.plugins?.tooltip?.callbacks
    expect(latestChartOptions?.plugins?.tooltip).toMatchObject({
      backgroundColor: 'rgba(255, 255, 255, 0.98)',
      titleColor: '#111827',
      bodyColor: '#374151',
      footerColor: '#374151',
      borderColor: 'rgba(17, 24, 39, 0.10)',
      borderWidth: 1,
      padding: 10,
      titleSpacing: 2,
      titleMarginBottom: 6,
      bodySpacing: 2,
      footerSpacing: 2,
      footerMarginTop: 6,
      displayColors: false,
      usePointStyle: true,
    })
    const label = tooltipCallbacks?.label as ((context: { dataIndex: number; datasetIndex: number }) => string[]) | undefined
    const afterBody = tooltipCallbacks?.afterBody as ((items: Array<{ dataIndex: number }>) => string[]) | undefined
    expect(label?.({ dataIndex: 0, datasetIndex: 0 })).toEqual([
      'usage_stats.credentials_quota_history_tokens_per_point: 1.00K',
      'usage_stats.credentials_quota_history_cost_per_point: $1.00',
    ])
    expect(afterBody?.([{ dataIndex: 0 }])).toEqual([
      'usage_stats.credentials_quota_history_interval: Aug 20, 10:00 AM → Aug 20, 10:10 AM',
    ])
    expect(afterBody?.([{ dataIndex: 1 }])).toEqual([
      'usage_stats.credentials_quota_history_change: 89% → 86%',
      'usage_stats.credentials_quota_history_interval: Aug 20, 11:00 AM → Aug 20, 11:30 AM',
      'usage_stats.total_tokens: 3.00K Token',
      'usage_stats.total_cost: $3.00',
    ])
  })

  it('shows three current-cycle summaries and adds estimated unused only to completed cycles', async () => {
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    const currentRecord = document.body.querySelector('[data-codex-quota-cycle-id="2"]')
    const currentSummary = currentRecord?.querySelector('[data-codex-quota-cycle-summary]')
    expect([...currentSummary!.querySelectorAll(':scope > [data-codex-quota-summary]')]
      .map((summary) => summary.getAttribute('data-codex-quota-summary'))).toEqual([
      'median',
      'used',
      'full-estimate',
    ])
    const medianRequestMetric = currentSummary?.querySelector('[data-codex-quota-summary="median"] [data-codex-quota-summary-metric="requests"]')
    expect(medianRequestMetric?.textContent).toBe('20')
    expect(medianRequestMetric?.getAttribute('role')).toBe('img')
    expect(medianRequestMetric?.getAttribute('aria-label')).toBe('usage_stats.total_requests: 20')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="median"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('1.00K')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="median"] [data-codex-quota-summary-metric="cost"]')?.textContent).toBe('$1.00')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="requests"]')?.textContent).toBe('14')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="full-estimate"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('35.71K')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="estimated-unused"]')).toBeNull()

    const completedRecord = document.body.querySelector('[data-codex-quota-cycle-id="1"]')
    const completedSummary = completedRecord?.querySelector('[data-codex-quota-cycle-summary]')
    expect([...completedSummary!.querySelectorAll(':scope > [data-codex-quota-summary]')]
      .map((summary) => summary.getAttribute('data-codex-quota-summary'))).toEqual([
      'median',
      'full-estimate',
      'used',
      'estimated-unused',
    ])
    expect(completedSummary?.querySelector('[data-codex-quota-summary="median"]')?.textContent).toContain('—')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('2.00K')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="full-estimate"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('28.57K')
    const estimatedUnused = completedSummary?.querySelector('[data-codex-quota-summary="estimated-unused"]')
    expect(estimatedUnused?.textContent).toContain('usage_stats.credentials_quota_history_estimated_unused')
    expect([...estimatedUnused!.querySelectorAll('[data-codex-quota-summary-metric]')]
      .map((metric) => metric.getAttribute('data-codex-quota-summary-metric'))).toEqual(['percentage', 'tokens', 'cost'])
    expect(estimatedUnused?.querySelector('[data-codex-quota-summary-metric="percentage"]')?.textContent).toBe('93%')
    expect(estimatedUnused?.querySelector('[data-codex-quota-summary-metric="percentage"] img')?.getAttribute('src')).toContain('Unused%20percentage')
    expect(estimatedUnused?.querySelector('[data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('26.57K')
    expect(estimatedUnused?.querySelector('[data-codex-quota-summary-metric="cost"]')?.textContent).toBe('$26.57')
  })

  it('keeps a current 76 percent baseline visible in the cycle list before any transition exists', async () => {
    const baselineResponse = cloneResponse()
    baselineResponse.cycles[0].transitions = []
    baselineResponse.cycles[0].first_remaining_percent = 76
    baselineResponse.cycles[0].last_remaining_percent = 76
    baselineResponse.cycles[0].observation_count = 8
    fetchCodexQuotaHistory.mockResolvedValue(baselineResponse)

    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    const currentRecord = document.body.querySelector('[data-codex-quota-cycle-id="2"][data-codex-quota-cycle-status="current"]')
    expect(currentRecord?.textContent).toContain('usage_stats.credentials_quota_history_percent_summary:{"percent":76,"count":8}')
    expect(currentRecord?.textContent).toContain('usage_stats.credentials_quota_history_cycle_expected_reset')
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_no_transition')
    expect(document.body.querySelector('[data-codex-quota-efficiency-chart]')).toBeNull()
    const chartSummary = document.body.querySelector('[data-codex-quota-current-cycle="true"] [data-codex-quota-chart-summary]')
    expect(chartSummary?.querySelector('[data-codex-quota-summary="median"]')?.textContent).toContain('—')
    expect(chartSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('5.00K')
    expect(chartSummary?.querySelector('[data-codex-quota-summary="full-estimate"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('20.83K')
  })

  it('shows the selected single window and cycle range in the current efficiency card', async () => {
    const singleWindowResponse = cloneResponse()
    singleWindowResponse.windows = [singleWindowResponse.windows[0]]
    singleWindowResponse.selected_window = singleWindowResponse.windows[0]
    singleWindowResponse.cycles = [singleWindowResponse.cycles[0]]
    fetchCodexQuotaHistory.mockResolvedValue(singleWindowResponse)

    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    const currentCard = document.body.querySelector('[data-codex-quota-current-cycle="true"]')
    expect(currentCard?.textContent).toContain('usage_stats.credentials_quota_history_window_weekly')
    expect(currentCard?.querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain('usage_stats.credentials_quota_history_cycle_range')
    expect(currentCard?.querySelector('[data-codex-quota-observed-range]')?.textContent).toContain('usage_stats.credentials_quota_history_observed_range')
    expect(document.body.querySelector('[aria-label="usage_stats.credentials_quota_history_window_selector"]')).toBeNull()
  })

  it('shows a Free monthly Primary as Monthly without exposing its role', async () => {
    const monthlyResponse = cloneResponse()
    monthlyResponse.windows = [{
      window_role: 'primary',
      window_kind: 'monthly',
      window_seconds: 2592000,
      has_current_cycle: true,
      last_observed_at: '2026-08-21T11:50:00Z',
    }]
    monthlyResponse.selected_window = monthlyResponse.windows[0]
    monthlyResponse.cycles = [monthlyResponse.cycles[0]]
    monthlyResponse.cycles[0].window_seconds = 2592000
    fetchCodexQuotaHistory.mockResolvedValue(monthlyResponse)

    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="free-codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_window_monthly')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_role_primary')
    expect(document.body.querySelector('[aria-label="usage_stats.credentials_quota_history_window_selector"]')).toBeNull()
  })

  it('explains why an ended cycle total Cost is unavailable', async () => {
    const missingCycleCostResponse = cloneResponse()
    missingCycleCostResponse.cycles[1].usage.cost_available = false
    fetchCodexQuotaHistory.mockResolvedValue(missingCycleCostResponse)
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    const completedSummary = document.body.querySelector('[data-codex-quota-cycle-id="1"] [data-codex-quota-cycle-summary]')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="cost"]')?.textContent)
      .toBe('usage_stats.credentials_quota_history_cost_missing')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="full-estimate"] [data-codex-quota-summary-metric="cost"]')?.textContent)
      .toBe('usage_stats.credentials_quota_history_cost_missing')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="estimated-unused"] [data-codex-quota-summary-metric="cost"]')?.textContent)
      .toBe('usage_stats.credentials_quota_history_cost_missing')
  })

  it('uses the Analysis tooltip surface in dark mode', async () => {
    useThemeStore.setState({ theme: 'dark', resolvedTheme: 'dark' })
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(latestChartOptions?.plugins?.tooltip).toMatchObject({
      backgroundColor: 'rgba(17, 24, 39, 0.94)',
      titleColor: '#f5f1e8',
      bodyColor: 'rgba(255, 255, 255, 0.86)',
      footerColor: 'rgba(255, 255, 255, 0.86)',
      borderColor: 'rgba(255, 255, 255, 0.10)',
    })
    expect(latestChartData?.datasets[1]).toMatchObject({
      borderColor: '#ff5a40',
      backgroundColor: '#ff5a40',
    })
    expect(document.body.querySelector<HTMLElement>('[data-codex-quota-cost-legend]')?.style.getPropertyValue('--quota-cost-line-color')).toBe('#ff5a40')
  })

  it('queries only the newly selected real window series', async () => {
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })
    const secondaryButton = [...document.body.querySelectorAll<HTMLButtonElement>('[aria-label="usage_stats.credentials_quota_history_window_selector"] button')]
      .find((button) => button.textContent === 'usage_stats.credentials_quota_history_window_five_hour')
    await act(async () => {
      secondaryButton?.click()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith(
      'codex-auth',
      { windowRole: 'secondary' },
      expect.any(AbortSignal),
    )
  })

  it('retries the real window that failed instead of falling back to the default window', async () => {
    fetchCodexQuotaHistory.mockReset()
    fetchCodexQuotaHistory
      .mockResolvedValueOnce(response)
      .mockRejectedValueOnce(new Error('secondary failed'))
      .mockResolvedValueOnce(response)
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })
    const secondaryButton = [...document.body.querySelectorAll<HTMLButtonElement>('[aria-label="usage_stats.credentials_quota_history_window_selector"] button')]
      .find((button) => button.textContent === 'usage_stats.credentials_quota_history_window_five_hour')
    await act(async () => {
      secondaryButton?.click()
      await Promise.resolve()
      await Promise.resolve()
    })
    const retryButton = document.body.querySelector<HTMLButtonElement>('[role="status"] button')
    await act(async () => {
      retryButton?.click()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith(
      'codex-auth',
      { windowRole: 'secondary' },
      expect.any(AbortSignal),
    )
  })

  it('draws an isolated Cost sample without adding points to a continuous line', async () => {
    const singleSampleResponse = cloneResponse()
    singleSampleResponse.cycles[0].transitions = singleSampleResponse.cycles[0].transitions.slice(0, 1)
    fetchCodexQuotaHistory.mockResolvedValue(singleSampleResponse)
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })
    const pointRadius = latestChartData?.datasets[1]?.pointRadius as unknown as ((context: { dataIndex: number }) => number)
    expect(pointRadius({ dataIndex: 0 })).toBe(3)
  })

  it('shows the Analysis-style pricing hint and hides the partial Cost median', async () => {
    const partialCostResponse = cloneResponse()
    const missingCostTransition = partialCostResponse.cycles[0].transitions[1]
    missingCostTransition.usage.cost_available = false
    missingCostTransition.cost_per_point_available = false
    fetchCodexQuotaHistory.mockResolvedValue(partialCostResponse)
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })
    const warning = document.body.querySelector('[data-codex-quota-cost-warning]')
    expect(warning?.parentElement?.tagName).toBe('HEADER')
    expect(warning?.textContent).toBe('usage_stats.credentials_quota_history_cost_unavailable')
    expect(document.body.querySelector('[data-codex-quota-summary="median"]')?.textContent).toContain(
      'usage_stats.credentials_quota_history_cost_missing',
    )
    expect(latestChartData?.datasets[1]?.data).toEqual([1, null, null, null])
    const pointRadius = latestChartData?.datasets[1]?.pointRadius as unknown as ((context: { dataIndex: number }) => number)
    expect([0, 1, 2, 3].map((dataIndex) => pointRadius({ dataIndex }))).toEqual([3, 0, 0, 0])
  })

  it('preserves the project-timezone wall clock from API timestamps', async () => {
    const offsetResponse = cloneResponse()
    offsetResponse.cycles[0].first_observed_at = '2026-08-21T13:01:00+08:00'
    offsetResponse.cycles[0].last_observed_at = '2026-08-21T14:02:00+08:00'
    fetchCodexQuotaHistory.mockResolvedValue(offsetResponse)
    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="codex-auth" />)
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(document.body.textContent).toContain('"start":"Aug 21, 01:01 PM"')
    expect(document.body.textContent).toContain('"end":"Aug 21, 02:02 PM"')
  })
})
