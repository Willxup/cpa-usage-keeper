// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { BasicPlatform, Chart as ChartJS, type ChartData, type ChartOptions, type Plugin } from 'chart.js'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/api'
import type { CodexQuotaHistoryResponse } from '@/lib/types'
import { useThemeStore } from '@/stores'
import { CodexQuotaHistoryPanel } from '../CodexQuotaHistoryPanel'

const fetchCodexQuotaHistory = vi.fn()
const deleteCodexQuotaHistoryCycle = vi.fn()
const translationState = vi.hoisted(() => ({ language: 'en-US' }))
type QuotaEfficiencyChartType = 'bar' | 'line'
type QuotaEfficiencyChartData = ChartData<QuotaEfficiencyChartType, Array<number | null>, string>
type QuotaEfficiencyChartOptions = ChartOptions<QuotaEfficiencyChartType>

let latestChartData: QuotaEfficiencyChartData | null = null
let latestChartOptions: QuotaEfficiencyChartOptions | null = null
let latestChartPlugins: Plugin<QuotaEfficiencyChartType>[] = []

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchCodexQuotaHistory: (...args: unknown[]) => fetchCodexQuotaHistory(...args),
    deleteCodexQuotaHistoryCycle: (...args: unknown[]) => deleteCodexQuotaHistoryCycle(...args),
  }
})

vi.mock('react-chartjs-2', () => ({
  Chart: (props: { data: QuotaEfficiencyChartData; options: QuotaEfficiencyChartOptions; plugins?: Plugin<QuotaEfficiencyChartType>[] }) => {
    latestChartData = props.data
    latestChartOptions = props.options
    latestChartPlugins = props.plugins ?? []
    return <div data-testid="quota-efficiency-chart" />
  },
}))

vi.mock('react-i18next', () => {
  const t = (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key
  return {
    initReactI18next: { type: '3rdParty', init: () => undefined },
    useTranslation: () => ({ t, i18n: { resolvedLanguage: translationState.language, language: translationState.language } }),
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

const completedTransition = (costAvailable = true) => ({
  from_remaining_percent: 93,
  to_remaining_percent: 92,
  percentage_points: 1,
  is_direct: true,
  interval_started_at: '2026-08-10T03:00:00Z',
  interval_ended_at: '2026-08-16T23:50:00Z',
  usage: usage(1000, 1, costAvailable, 10),
  tokens_per_point: 1000,
  cost_per_point: 1,
  cost_per_point_available: costAvailable,
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
    usage: usage(5000, 5, true, 80),
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
    usage: usage(2000, 2, true, 12),
    transitions: [],
  }],
}

const cloneResponse = (): CodexQuotaHistoryResponse => structuredClone(response)

describe('CodexQuotaHistoryPanel', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    translationState.language = 'en-US'
    useThemeStore.setState({ theme: 'light', resolvedTheme: 'light' })
    latestChartData = null
    latestChartOptions = null
    latestChartPlugins = []
    fetchCodexQuotaHistory.mockReset()
    deleteCodexQuotaHistoryCycle.mockReset()
    deleteCodexQuotaHistoryCycle.mockResolvedValue(undefined)
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

  const renderPanel = async (authIndex = 'codex-auth') => {
    await act(async () => root.render(<CodexQuotaHistoryPanel authIndex={authIndex} />))
  }

  const cycleCard = (id: number) => container.querySelector<HTMLElement>(`[data-codex-quota-cycle-id="${id}"]`)!
  const detailButton = (id: number) => cycleCard(id).querySelector<HTMLButtonElement>('button[aria-expanded]')!
  const metricText = (summary: Element | null | undefined, metric: string) =>
    summary?.querySelector(`[data-codex-quota-summary-metric="${metric}"]`)?.textContent
  const click = async (element: HTMLElement) => { await act(async () => element.click()) }
  const selectCycle = (id: number) => click(cycleCard(id).querySelector<HTMLButtonElement>('[data-codex-quota-cycle-select]')!)

  const windowButton = (role: 'primary' | 'secondary') =>
    container.querySelectorAll<HTMLButtonElement>('[aria-label="usage_stats.credentials_quota_history_window_selector"] button')[role === 'primary' ? 0 : 1]!

  it('animates the selected cycle and successful refresh without replaying pending or failed requests', async () => {
    const initial = cloneResponse()
    initial.cycles[1].transitions = [completedTransition()]
    fetchCodexQuotaHistory.mockResolvedValueOnce(initial)
    await renderPanel()
    const reset = vi.fn()
    const chart = { stop: vi.fn(), reset } as unknown as ChartJS<QuotaEfficiencyChartType>
    const update = () => {
      const lifecycle = latestChartPlugins.find(plugin => plugin.id === 'keeperChartLifecycle')
      expect(lifecycle?.beforeDatasetsUpdate).toBeTypeOf('function')
      lifecycle!.beforeDatasetsUpdate!(chart, { mode: 'default', cancelable: true }, {})
    }
    update()
    expect(reset).toHaveBeenCalledTimes(1)
    await selectCycle(1)
    update()
    expect(reset).toHaveBeenCalledTimes(2)
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
    await selectCycle(1)
    update()
    expect(reset).toHaveBeenCalledTimes(2)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()

    let reject!: (error: Error) => void
    fetchCodexQuotaHistory.mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    await click(windowButton('primary'))
    update()
    expect(reset).toHaveBeenCalledTimes(2)
    await act(async () => reject(new Error('refresh failed')))
    update()
    expect(reset).toHaveBeenCalledTimes(2)

    fetchCodexQuotaHistory.mockResolvedValueOnce(structuredClone(initial))
    await click(windowButton('primary'))
    update()
    expect(reset).toHaveBeenCalledTimes(3)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    update()
    expect(reset).toHaveBeenCalledTimes(3)
  })

  it('keeps every cycle summary visible while all percentage details are initially unmounted', async () => {
    await renderPanel()
    expect(container.querySelectorAll('[data-codex-quota-cycle-summary]')).toHaveLength(2)
    expect(container.querySelectorAll('[data-codex-quota-transition]')).toHaveLength(0)
    expect(container.querySelectorAll('[data-testid="quota-efficiency-chart"]')).toHaveLength(1)
    expect(container.querySelector('[data-codex-quota-chart-summary]')).toBeNull()
    expect(container.textContent).not.toContain('usage_stats.credentials_quota_history_records_title')
    const disclosure = detailButton(2)
    expect(disclosure.textContent).toBe('usage_stats.credentials_quota_history_expand_details:{"count":2}')
    expect(disclosure.getAttribute('aria-expanded')).toBe('false')
    await click(disclosure)
    expect(disclosure.getAttribute('aria-expanded')).toBe('true')
    expect(document.getElementById(disclosure.getAttribute('aria-controls')!)?.querySelectorAll('[data-codex-quota-transition]')).toHaveLength(2)
    await click(disclosure)
    expect(container.querySelectorAll('[data-codex-quota-transition]')).toHaveLength(0)
    expect(cycleCard(2).querySelector('[data-codex-quota-cycle-select]')?.getAttribute('aria-pressed')).toBe('true')
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
  })

  it('selects a completed cycle for the single chart using its effective range and historical duration', async () => {
    const next = cloneResponse()
    const completed = next.cycles[1]
    completed.window_seconds = 18000
    completed.effective_started_at = '2026-08-10T01:00:00Z'
    completed.effective_ended_at = '2026-08-10T06:00:00Z'
    completed.transitions = [completedTransition(false)]
    fetchCodexQuotaHistory.mockResolvedValue(next)
    await renderPanel()
    await selectCycle(1)
    const chart = container.querySelector('[data-codex-quota-chart-cycle-id="1"]')!
    expect(chart.querySelector('h3')?.textContent).toContain('usage_stats.credentials_quota_history_window_five_hour')
    expect(chart.querySelector('h3')?.textContent).toContain('usage_stats.credentials_quota_history_status_completed')
    expect(chart.querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain('"start":"Aug 10, 01:00","end":"Aug 10, 06:00"')
    expect(chart.querySelector('dt')?.textContent).toBe('usage_stats.credentials_quota_history_last_remaining')
    expect(chart.querySelector('[data-codex-quota-cost-warning]')).not.toBeNull()
    expect(chart.querySelector('[data-codex-quota-accessible-summary]')?.textContent).toContain('93% → 92%')
    expect(chart.querySelector('[data-codex-quota-accessible-summary]')?.textContent).not.toContain('90% → 89%')
    expect(latestChartData?.labels).toEqual(['93% → 92%'])
    expect(latestChartData?.datasets[1].data).toEqual([null])
    expect(container.querySelectorAll('[data-testid="quota-efficiency-chart"]')).toHaveLength(1)
    expect(cycleCard(1).querySelector('[data-codex-quota-cycle-select]')?.getAttribute('aria-pressed')).toBe('true')
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
  })

  it('selects a cycle when clicking its summary and keeps disclosure and deletion independent', async () => {
    await renderPanel()
    const completedSummary = cycleCard(1).querySelector<HTMLElement>('[data-codex-quota-cycle-summary]')!
    await click(completedSummary)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    const disclosure = detailButton(2)
    await click(disclosure)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    await openDelete(2)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    await click(modalButton('common.cancel'))
    expect(container.querySelector('button button')).toBeNull()
  })

  it('returns to and focuses the chart only on demand without changing selection', async () => {
    await renderPanel()
    const chart = container.querySelector<HTMLElement>('[data-codex-quota-chart-cycle-id="2"]')!
    const scroll = vi.fn()
    chart.scrollIntoView = scroll
    await selectCycle(1)
    expect(scroll).not.toHaveBeenCalled()
    const returnButton = [...container.querySelectorAll<HTMLButtonElement>('[data-codex-quota-cycle-id="1"] button')]
      .find((button) => button.textContent === 'usage_stats.credentials_quota_history_return_to_chart')!
    await click(returnButton)
    expect(scroll).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(chart)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
  })

  it('returns below the actual sticky tab height inside the drawer scroll container', async () => {
    container.className = 'modal-body'
    const tabs = document.createElement('div')
    tabs.setAttribute('data-credential-detail-tab-bar', '')
    container.appendChild(tabs)
    await renderPanel()
    // React 挂载会替换容器原有内容，真实抽屉的标签栏是面板的兄弟节点。
    container.prepend(tabs)
    const chart = container.querySelector<HTMLElement>('[data-codex-quota-chart-cycle-id="2"]')!
    container.scrollTop = 900
    vi.spyOn(container, 'getBoundingClientRect').mockReturnValue({ top: 80 } as DOMRect)
    vi.spyOn(chart, 'getBoundingClientRect').mockReturnValue({ top: -620 } as DOMRect)
    vi.spyOn(tabs, 'getBoundingClientRect').mockReturnValue({ height: 64 } as DOMRect)
    const scroll = vi.fn()
    container.scrollTo = scroll
    const returnButton = [...container.querySelectorAll<HTMLButtonElement>('[data-codex-quota-cycle-id="2"] button')]
      .find((button) => button.textContent === 'usage_stats.credentials_quota_history_return_to_chart')!
    await click(returnButton)
    expect(scroll).toHaveBeenCalledWith({ top: 124, behavior: 'smooth' })
    expect(document.activeElement).toBe(chart)
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList)
    await click(returnButton)
    expect(scroll).toHaveBeenLastCalledWith({ top: 124, behavior: 'instant' })
    expect(document.activeElement).toBe(chart)
  })

  it('updates same-id summaries, expanded details and tooltips after a stored-cost retry', async () => {
    const initial = cloneResponse()
    initial.cycles[1].transitions = [completedTransition()]
    fetchCodexQuotaHistory.mockResolvedValueOnce(initial)
    await renderPanel()
    await selectCycle(1)
    await click(detailButton(1))
    const card = () => cycleCard(1)
    const before = card().textContent
    let fail!: (error: Error) => void
    fetchCodexQuotaHistory.mockReturnValueOnce(new Promise((_, reject) => { fail = reject }))
    await click(windowButton('primary'))
    const deleteButtons = () => [...container.querySelectorAll<HTMLButtonElement>('[data-codex-quota-cycle-id] button[aria-label="usage_stats.credentials_quota_history_delete_title"]')]
    expect(deleteButtons().every((button) => button.disabled)).toBe(true)
    expect(card().querySelector('button[aria-expanded]')?.getAttribute('aria-expanded')).toBe('true')
    await act(async () => fail(new ApiError('busy', 503, 'costs_busy')))
    expect(card().textContent).toBe(before)
    expect(deleteButtons().every((button) => !button.disabled)).toBe(true)
    expect(container.querySelector('[role="status"]')?.textContent).toContain('usage_stats.costs_busy')

    const updated = structuredClone(initial)
    const cycle = updated.cycles[1]
    cycle.usage = usage(6400, 6.4, false, 24)
    cycle.first_observed_at = '2026-08-10T04:00:00Z'
    cycle.last_observed_at = '2026-08-16T23:55:00Z'
    cycle.last_remaining_percent = 92
    cycle.observation_count = 9
    cycle.transitions[0] = {
      ...cycle.transitions[0], interval_started_at: cycle.first_observed_at, interval_ended_at: cycle.last_observed_at,
      usage: usage(2400, 7.25, true, 14), tokens_per_point: 2400, cost_per_point: 7.25,
    }
    fetchCodexQuotaHistory.mockResolvedValueOnce(updated)
    await act(async () => container.querySelector<HTMLButtonElement>('[role="status"] button')!.click())
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith('codex-auth', { windowRole: 'primary' }, expect.any(AbortSignal))
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    expect(card().querySelector('button[aria-expanded]')?.getAttribute('aria-expanded')).toBe('true')
    const used = card().querySelector('[data-codex-quota-summary="used"]')!
    expect(used.querySelector('[data-codex-quota-summary-metric="requests"]')?.textContent).toBe('24')
    expect(used.querySelector('[data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('6.40K')
    expect(used.querySelector('[data-codex-quota-summary-metric="cost"]')?.textContent).toBe('usage_stats.credentials_quota_history_cost_missing')
    expect(card().querySelector('[data-codex-quota-transition]')?.textContent).toContain('Aug 10, 04:00')
    expect(card().querySelector('[data-codex-quota-transition]')?.textContent).toContain('$7.25/1%')
    expect(latestChartData?.datasets[0].data).toEqual([2400])
    expect(latestChartData?.datasets[1].data).toEqual([7.25])
    const label = latestChartOptions?.plugins?.tooltip?.callbacks?.label as (context: { dataIndex: number; datasetIndex: number }) => string[]
    expect(label({ dataIndex: 0, datasetIndex: 0 })).toEqual([
      'usage_stats.credentials_quota_history_tokens_per_point: 2.40K',
      'usage_stats.credentials_quota_history_cost_per_point: $7.25',
    ])
    await openDelete(1)
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('usage_stats.credentials_quota_history_percent_summary:{"percent":92,"count":9}')
    await click(modalButton('common.cancel'))
    expect(card().querySelector('button[aria-expanded]')?.getAttribute('aria-expanded')).toBe('true')
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(3)
  })

  it('updates an expanded unselected card on language changes and preserves it through theme changes', async () => {
    const initial = cloneResponse()
    initial.cycles[1].transitions = [completedTransition()]
    fetchCodexQuotaHistory.mockResolvedValueOnce(initial)
    await renderPanel()
    await click(detailButton(1))
    const card = () => cycleCard(1)
    const english = card().textContent
    const lightLine = latestChartData?.datasets[2].borderColor
    await act(async () => useThemeStore.setState({ theme: 'dark', resolvedTheme: 'dark' }))
    expect(latestChartData?.datasets[2].borderColor).not.toEqual(lightLine)
    expect(card().textContent).toBe(english)
    expect(card().querySelector('button[aria-expanded]')?.getAttribute('aria-expanded')).toBe('true')
    translationState.language = 'zh-CN'
    await renderPanel()
    const first = new Intl.DateTimeFormat('zh-CN', { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: 'UTC' }).format(new Date(initial.cycles[1].first_observed_at))
    expect(card().querySelector('[data-codex-quota-transition]')?.textContent).toContain(first)
    expect(card().textContent).not.toBe(english)
    expect(card().querySelector('button[aria-expanded]')?.getAttribute('aria-expanded')).toBe('true')
    translationState.language = 'en-US'
    await renderPanel()
    expect(card().textContent).toBe(english)
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="2"]')).not.toBeNull()
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
  })

  it('retains a valid historical selection on refresh and falls back after its deletion', async () => {
    await renderPanel()
    await selectCycle(1)
    const refreshed = cloneResponse()
    refreshed.cycles[1].transitions = [completedTransition()]
    fetchCodexQuotaHistory.mockResolvedValueOnce(refreshed)
    await click(windowButton('primary'))
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    expect(latestChartData?.labels).toEqual(['93% → 92%'])
    await openDelete(1)
    fetchCodexQuotaHistory.mockResolvedValueOnce({ ...refreshed, cycles: [refreshed.cycles[0]] })
    await click(modalButton('common.delete'))
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="2"]')).not.toBeNull()
  })

  it('retains the selected cycle when a different cycle is deleted', async () => {
    await renderPanel()
    await selectCycle(1)
    await openDelete(2)
    fetchCodexQuotaHistory.mockResolvedValueOnce({ ...response, cycles: [response.cycles[1]] })
    await click(modalButton('common.delete'))
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
  })

  it('resets cycle selection and expanded details on role and account changes even when ids overlap', async () => {
    await renderPanel()
    await selectCycle(1)
    await click(detailButton(1))
    const secondary = cloneResponse()
    secondary.selected_window = secondary.windows[1]
    fetchCodexQuotaHistory.mockResolvedValueOnce(secondary)
    await click(windowButton('secondary'))
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="2"]')).not.toBeNull()
    expect(container.querySelectorAll('button[aria-expanded="true"]')).toHaveLength(0)
    await selectCycle(1)
    fetchCodexQuotaHistory.mockResolvedValueOnce(structuredClone(secondary))
    await click(windowButton('secondary'))
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="1"]')).not.toBeNull()
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith('codex-auth', { windowRole: 'secondary' }, expect.any(AbortSignal))
    await renderPanel('other-auth')
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="2"]')).not.toBeNull()
  })

  it('defaults to the latest available cycle and shows selected empty and cost states', async () => {
    const next = cloneResponse()
    next.cycles[0].status = 'completed'
    next.cycles[0].transitions = []
    next.cycles[1].transitions = [completedTransition(false)]
    fetchCodexQuotaHistory.mockResolvedValueOnce(next)
    await renderPanel()
    const chart = container.querySelector('[data-codex-quota-chart-cycle-id="2"]')!
    expect(chart.textContent).toContain('usage_stats.credentials_quota_history_no_transition')
    expect(chart.querySelector('[data-codex-quota-cost-warning]')).toBeNull()
    await selectCycle(1)
    expect(chart.textContent).not.toContain('usage_stats.credentials_quota_history_no_transition')
    expect(chart.querySelector('[data-codex-quota-cost-warning]')).not.toBeNull()
  })

  const openDelete = (cycleId = 2) => click(cycleCard(cycleId).querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_quota_history_delete_title"]')!)
  const modalButton = (label: string) => Array.from(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')).find((button) => button.textContent === label)!

  const createLayoutChart = () => {
    const canvas = document.createElement('canvas')
    canvas.width = 840
    canvas.height = 286
    const context = {
      canvas,
      measureText: (text: string) => ({ width: text.length * 5 }),
      save() {}, restore() {}, resetTransform() {}, setTransform() {}, clearRect() {},
      createLinearGradient: () => ({ addColorStop() {} }),
    } as unknown as CanvasRenderingContext2D
    vi.spyOn(canvas, 'getContext').mockReturnValue(context)
    // 使用真实 Chart.js 刻度生成和缩放流程，只省略 Canvas 绘制。
    return new ChartJS(canvas, {
      type: 'bar',
      data: latestChartData!,
      options: { ...latestChartOptions!, responsive: false },
      platform: BasicPlatform,
      plugins: [{ id: 'layout-only', beforeDraw: () => false }],
    })
  }

  it('keeps both percentage endpoints while reducing ticks on resize without dropping chart data', async () => {
    const next = cloneResponse()
    next.cycles[0].last_remaining_percent = 5
    next.cycles[0].transitions = [{
      ...next.cycles[0].transitions[0], from_remaining_percent: 100, to_remaining_percent: 5, percentage_points: 95, is_direct: false,
    }]
    fetchCodexQuotaHistory.mockResolvedValueOnce(next)
    await renderPanel('auth-1')
    const chart = createLayoutChart()
    try {
      for (const [width, count] of [[840, 8], [340, 4], [254, 3], [840, 8]]) {
        chart.resize(width, 286)
        const ticks = chart.scales.x.ticks
        expect(ticks).toHaveLength(count)
        expect(ticks[0].label).toBe('100% → 99%')
        expect(ticks.at(-1)?.label).toBe('6% → 5%')
        expect(chart.getDatasetMeta(0).data).toHaveLength(95)
      }
    } finally {
      chart.destroy()
    }
  })

  it.each([1, 2, 3, 4])('shows every tick when only %s percentage samples exist', async (count) => {
    const next = cloneResponse()
    next.cycles[0].transitions = [{
      ...next.cycles[0].transitions[0], from_remaining_percent: 76, to_remaining_percent: 76 - count, percentage_points: count,
    }]
    fetchCodexQuotaHistory.mockResolvedValueOnce(next)
    await renderPanel('auth-1')
    const chart = createLayoutChart()
    try {
      expect(chart.scales.x.ticks.map((tick) => tick.label)).toEqual(latestChartData?.labels)
    } finally {
      chart.destroy()
    }
  })

  it.each([
    [100, 'ok'], [50, 'ok'], [49, 'warning'], [20, 'warning'], [19, 'danger'], [0, 'danger'], [null, 'unknown'],
  ] as const)('shows the latest remaining percentage %s with quota status %s in the header', async (percent, status) => {
    const next = cloneResponse()
    next.cycles[0].last_remaining_percent = percent
    fetchCodexQuotaHistory.mockResolvedValueOnce(next)
    await renderPanel('auth-1')

    const remaining = container.querySelector('[data-codex-quota-chart-cycle-id="2"] header dl')
    expect(remaining?.querySelector('dt')?.textContent).toBe('usage_stats.credentials_quota_history_current_remaining')
    expect(remaining?.querySelector('dd')?.textContent).toBe(percent === null ? '—' : `${percent}%`)
    expect(remaining?.getAttribute('data-status')).toBe(status)
  })

  it('does not present completed-cycle percentages as current', async () => {
    const next = cloneResponse()
    next.cycles = [next.cycles[1]]
    fetchCodexQuotaHistory.mockResolvedValueOnce(next)
    await renderPanel('auth-1')
    const remaining = container.querySelector('[data-codex-quota-chart-cycle-id="1"] header dl')
    expect(remaining?.querySelector('dt')?.textContent).toBe('usage_stats.credentials_quota_history_last_remaining')
    expect(remaining?.querySelector('dd')?.textContent).toBe('93%')
  })

  it('confirms deletion of a current cycle and reloads history with the selected window', async () => {
    await renderPanel('auth-1')
    await openDelete()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('usage_stats.credentials_quota_history_delete_description')
    expect(deleteCodexQuotaHistoryCycle).not.toHaveBeenCalled()
    const next = cloneResponse()
    next.cycles = [{ ...next.cycles[1], status: 'current' }]
    fetchCodexQuotaHistory.mockResolvedValueOnce(next)
    await click(modalButton('common.delete'))
    expect(deleteCodexQuotaHistoryCycle).toHaveBeenCalledWith('auth-1', 2, expect.any(AbortSignal))
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith('auth-1', { windowRole: 'primary' }, expect.any(AbortSignal))
    expect(cycleCard(2)).toBeNull()
    expect(cycleCard(1)?.getAttribute('data-codex-quota-cycle-status')).toBe('current')
  })

  it('distinguishes roles whose remaining histories have the same window title', async () => {
    const sameKind = cloneResponse()
    sameKind.windows[1] = { ...sameKind.windows[0], window_role: 'secondary' }
    fetchCodexQuotaHistory.mockResolvedValueOnce(sameKind)
    await renderPanel('auth-1')
    const labels = Array.from(container.querySelectorAll('button[aria-pressed]'), (button) => button.textContent)
    expect(labels[0]).not.toBe(labels[1])
    expect(labels[0]).toContain('Primary')
    expect(labels[1]).toContain('Secondary')
  })

  it('can cancel deletion of a completed cycle', async () => {
    await renderPanel('auth-1')
    await openDelete(1)
    await click(modalButton('common.cancel'))
    expect(deleteCodexQuotaHistoryCycle).not.toHaveBeenCalled()
    expect(cycleCard(1)).not.toBeNull()
  })

  it('selects a remaining window after deleting the last cycle of the selected role', async () => {
    await renderPanel('auth-1')
    await act(async () => (container.querySelector('button[aria-pressed="true"]') as HTMLButtonElement).click())
    await openDelete()
    const remaining = cloneResponse()
    remaining.windows = [remaining.windows[1]]
    remaining.selected_window = remaining.windows[0]
    remaining.cycles = [{ ...remaining.cycles[0], id: 3, window_seconds: 18000 }]
    fetchCodexQuotaHistory.mockResolvedValueOnce({ ...remaining, selected_window: null, cycles: [] })
      .mockResolvedValueOnce(remaining)
    await click(modalButton('common.delete'))
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith('auth-1', {}, expect.any(AbortSignal))
    expect(cycleCard(3)).not.toBeNull()
  })

  it('preserves the cycle and allows retry after a delete failure', async () => {
    deleteCodexQuotaHistoryCycle.mockRejectedValueOnce(new Error('delete failed'))
    await renderPanel('auth-1')
    await openDelete()
    await click(modalButton('common.delete'))
    expect(document.querySelector('[role="alert"]')?.textContent).toBe('delete failed')
    expect(cycleCard(2)).not.toBeNull()
    expect(modalButton('common.delete').disabled).toBe(false)
  })

  it('ignores completion of a delete after switching accounts', async () => {
    let finish!: () => void
    deleteCodexQuotaHistoryCycle.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve }))
    await renderPanel('auth-1')
    await openDelete()
    await click(modalButton('common.delete'))
    expect(modalButton('common.delete').disabled).toBe(true)
    await renderPanel('auth-2')
    const calls = fetchCodexQuotaHistory.mock.calls.length
    await act(async () => finish())
    expect(fetchCodexQuotaHistory.mock.calls).toHaveLength(calls)
    expect(deleteCodexQuotaHistoryCycle.mock.calls[0][2].aborted).toBe(true)
  })

  it('expands crossed intervals and estimates full quota only from settled percentage points', async () => {
    await renderPanel()

    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
    expect(fetchCodexQuotaHistory).toHaveBeenCalledWith('codex-auth', {}, expect.any(AbortSignal))
    const windowButtons = [...document.body.querySelectorAll<HTMLButtonElement>('[aria-label="usage_stats.credentials_quota_history_window_selector"] button')]
    expect(windowButtons).toHaveLength(2)
    expect(windowButtons[0]?.textContent).toBe('usage_stats.credentials_quota_history_window_weekly')
    expect(windowButtons[1]?.textContent).toBe('usage_stats.credentials_quota_history_window_five_hour')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_role_primary')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_role_secondary')
    expect(latestChartData?.labels).toEqual(['90% → 89%', '89% → 88%', '88% → 87%', '87% → 86%'])
    expect(latestChartData?.datasets).toEqual([
      expect.objectContaining({ type: 'bar', yAxisID: 'tokens', data: [1000, 1000, 1000, 1000] }),
      expect.objectContaining({ type: 'line', yAxisID: 'cost', data: [1, 1, 1, 1] }),
      expect.objectContaining({ type: 'line', yAxisID: 'remaining', data: [89, 88, 87, 86] }),
    ])
    // 直接观测和跨点均摊仍需在图上区分，但不绑定具体色值。
    const barBackground = latestChartData!.datasets[0].backgroundColor as
      (context: { dataIndex: number; chart: object }) => string
    expect(barBackground({ dataIndex: 0, chart: {} })).not.toBe(barBackground({ dataIndex: 1, chart: {} }))
    expect(barBackground({ dataIndex: 1, chart: {} })).toBe(barBackground({ dataIndex: 2, chart: {} }))
    const pointRadius = latestChartData!.datasets[1].pointRadius as (context: { dataIndex: number }) => number
    expect([0, 1, 2, 3].map((dataIndex) => pointRadius({ dataIndex }))).toEqual([0, 0, 0, 0])
    expect(latestChartOptions?.scales?.remaining).toMatchObject({ display: true, min: 0, max: 100 })
    expect(document.body.querySelector('[aria-label="usage_stats.credentials_quota_history_metric_selector"]')).toBeNull()
    expect(document.body.querySelector('[data-codex-quota-cycle-id="2"][data-codex-quota-cycle-status="current"]')).not.toBeNull()
    expect(document.body.querySelector('[data-codex-quota-cycle-id="1"][data-codex-quota-cycle-status="completed"]')).not.toBeNull()
    const chartSummary = cycleCard(2).querySelector('[data-codex-quota-cycle-summary]')
    const usedSummary = chartSummary?.querySelector('[data-codex-quota-summary="used"]')
    const fullEstimateSummary = chartSummary?.querySelector('[data-codex-quota-summary="full-estimate"]')
    const medianSummary = chartSummary?.querySelector('[data-codex-quota-summary="median"]')
    expect([...chartSummary!.querySelectorAll(':scope > [data-codex-quota-summary]')]
      .map((summary) => summary.getAttribute('data-codex-quota-summary'))).toEqual(['median', 'used', 'full-estimate'])
    expect(usedSummary?.textContent).toContain('usage_stats.credentials_quota_history_used')
    expect(metricText(usedSummary, 'requests')).toBe('80')
    expect(metricText(usedSummary, 'tokens')).toBe('5.00K')
    expect(metricText(usedSummary, 'cost')).toBe('$5.00')
    expect(fullEstimateSummary?.textContent).toContain('usage_stats.credentials_quota_history_full_estimate')
    expect(metricText(fullEstimateSummary, 'requests')).toBe('1.75K')
    expect(metricText(fullEstimateSummary, 'tokens')).toBe('100.00K')
    expect(metricText(fullEstimateSummary, 'cost')).toBe('$100.00')
    expect(medianSummary?.textContent).toContain('usage_stats.credentials_quota_history_median_per_point')
    expect(metricText(medianSummary, 'requests')).toBe('20')
    expect(metricText(medianSummary, 'tokens')).toBe('1.00K')
    expect(metricText(medianSummary, 'cost')).toBe('$1.00')
    for (const summary of [usedSummary, fullEstimateSummary, medianSummary]) {
      const metrics = [...summary!.querySelectorAll('[data-codex-quota-summary-metric]')]
      expect(metrics.map((metric) => metric.getAttribute('data-codex-quota-summary-metric'))).toEqual(['requests', 'tokens', 'cost'])
      for (const metric of metrics) {
        const icon = metric.querySelector('img')
        expect(icon?.getAttribute('alt')).toBe('')
        expect(icon?.getAttribute('aria-hidden')).toBe('true')
      }
    }
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_cycle_label')
    expect(cycleCard(1).querySelector('[data-codex-quota-cycle-select] strong')?.textContent).toContain('Aug 10, 00:00 → Aug 17, 00:00')
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_first_observed')
    const accessibleSummary = document.body.querySelector('[data-codex-quota-accessible-summary]')
    expect(accessibleSummary?.textContent).toContain('90% → 89%')
    expect(accessibleSummary?.textContent).toContain('1.00K Token/1%')
    expect(accessibleSummary?.textContent).toContain('$1.00/1%')
    expect(accessibleSummary?.textContent).toContain('usage_stats.credentials_quota_history_interval: Aug 20, 11:00 → Aug 20, 11:30')
    expect(document.body.querySelector('[data-codex-quota-efficiency-chart]')?.getAttribute('aria-hidden')).toBe('true')

    const tooltipCallbacks = latestChartOptions?.plugins?.tooltip?.callbacks
    const label = tooltipCallbacks?.label as (context: { dataIndex: number; datasetIndex: number }) => string[]
    const afterBody = tooltipCallbacks?.afterBody as (items: Array<{ dataIndex: number }>) => string[]
    expect(label({ dataIndex: 0, datasetIndex: 0 })).toEqual([
      'usage_stats.credentials_quota_history_tokens_per_point: 1.00K',
      'usage_stats.credentials_quota_history_cost_per_point: $1.00',
    ])
    expect(afterBody([{ dataIndex: 0 }])).toEqual([
      'usage_stats.credentials_quota_history_remaining_percentage: 89%',
      'usage_stats.credentials_quota_history_interval: Aug 20, 10:00 → Aug 20, 10:10',
    ])
    expect(afterBody([{ dataIndex: 1 }])).toEqual([
      'usage_stats.credentials_quota_history_remaining_percentage: 88%',
      'usage_stats.credentials_quota_history_change: 89% → 86%',
      'usage_stats.credentials_quota_history_interval: Aug 20, 11:00 → Aug 20, 11:30',
      'usage_stats.total_tokens: 3.00K Token',
      'usage_stats.total_cost: $3.00',
    ])
  })

  it('shows three current-cycle summaries and adds estimated unused only to completed cycles', async () => {
    await renderPanel()

    const currentRecord = cycleCard(2)
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
    expect(currentSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="requests"]')?.textContent).toBe('80')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="full-estimate"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('100.00K')
    expect(currentSummary?.querySelector('[data-codex-quota-summary="estimated-unused"]')).toBeNull()

    const completedRecord = cycleCard(1)
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
    expect(completedSummary?.querySelector('[data-codex-quota-summary="full-estimate"]')?.textContent).toContain('—')
    const estimatedUnused = completedSummary?.querySelector('[data-codex-quota-summary="estimated-unused"]')
    expect(estimatedUnused?.textContent).toContain('usage_stats.credentials_quota_history_estimated_unused')
    expect(estimatedUnused?.textContent).toContain('—')
    expect(estimatedUnused?.querySelector('[data-codex-quota-summary-metric]')).toBeNull()
  })

  it.each([
    {
      name: 'derives completed full-quota and unused estimates from settled transitions',
      cycleCostAvailable: true, transitionCostAvailable: true,
      fullCost: '$100.00', unusedCost: '$98.00',
    },
    {
      name: 'keeps completed settled estimates visible while marking unavailable Cost',
      cycleCostAvailable: false, transitionCostAvailable: false,
      fullCost: 'usage_stats.credentials_quota_history_cost_missing', unusedCost: 'usage_stats.credentials_quota_history_cost_missing',
    },
    {
      name: 'excludes unavailable unsettled Cost only from the full-quota estimate',
      cycleCostAvailable: false, transitionCostAvailable: true,
      fullCost: '$100.00', unusedCost: 'usage_stats.credentials_quota_history_cost_missing',
    },
  ].map(testCase => [testCase.name, testCase] as const))('%s', async (_name, { cycleCostAvailable, transitionCostAvailable, fullCost, unusedCost }) => {
    const next = cloneResponse()
    const completedCycle = next.cycles[1]
    completedCycle.last_remaining_percent = 92
    completedCycle.usage.cost_available = cycleCostAvailable
    completedCycle.transitions = [completedTransition(transitionCostAvailable)]
    fetchCodexQuotaHistory.mockResolvedValue(next)
    await renderPanel()

    const completedSummary = cycleCard(1).querySelector('[data-codex-quota-cycle-summary]')
    const fullEstimate = completedSummary?.querySelector('[data-codex-quota-summary="full-estimate"]')
    expect(metricText(fullEstimate, 'tokens')).toBe('100.00K')
    expect(metricText(fullEstimate, 'cost')).toBe(fullCost)
    const estimatedUnused = completedSummary?.querySelector('[data-codex-quota-summary="estimated-unused"]')
    expect(metricText(estimatedUnused, 'percentage')).toBe('98%')
    expect(metricText(estimatedUnused, 'tokens')).toBe('98.00K')
    expect(metricText(estimatedUnused, 'cost')).toBe(unusedCost)
  })

  it('keeps a current 76 percent baseline visible in the cycle list before any transition exists', async () => {
    const baselineResponse = cloneResponse()
    baselineResponse.cycles[0].transitions = []
    baselineResponse.cycles[0].first_remaining_percent = 76
    baselineResponse.cycles[0].last_remaining_percent = 76
    baselineResponse.cycles[0].observation_count = 8
    fetchCodexQuotaHistory.mockResolvedValue(baselineResponse)

    await renderPanel()

    const currentRecord = document.body.querySelector('[data-codex-quota-cycle-id="2"][data-codex-quota-cycle-status="current"]')
    expect(currentRecord?.textContent).toContain('usage_stats.credentials_quota_history_percent_summary:{"percent":76,"count":8}')
    expect(currentRecord?.querySelector('[data-codex-quota-cycle-select] strong')?.textContent).toContain('Aug 17, 00:00 → Aug 24, 00:00')
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_no_transition')
    expect(document.body.querySelector('[data-codex-quota-efficiency-chart]')).toBeNull()
    expect(container.querySelector('[data-codex-quota-chart-cycle-id="2"] header dd')?.textContent).toBe('76%')
    const chartSummary = currentRecord?.querySelector('[data-codex-quota-cycle-summary]')
    expect(chartSummary?.querySelector('[data-codex-quota-summary="median"]')?.textContent).toContain('—')
    expect(chartSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="tokens"]')?.textContent).toBe('5.00K')
    expect(chartSummary?.querySelector('[data-codex-quota-summary="full-estimate"]')?.textContent).toContain('—')
  })

  it('shows the selected single window and cycle range in the current efficiency card', async () => {
    const singleWindowResponse = cloneResponse()
    singleWindowResponse.windows = [singleWindowResponse.windows[0]]
    singleWindowResponse.selected_window = singleWindowResponse.windows[0]
    singleWindowResponse.cycles = [singleWindowResponse.cycles[0]]
    fetchCodexQuotaHistory.mockResolvedValue(singleWindowResponse)

    await renderPanel()

    const currentCard = document.body.querySelector('[data-codex-quota-chart-cycle-id="2"]')
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

    await renderPanel('free-codex-auth')

    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_window_monthly')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_role_primary')
    expect(document.body.querySelector('[aria-label="usage_stats.credentials_quota_history_window_selector"]')).toBeNull()
  })

  it('keeps an ended cycle total Cost unavailable without inventing estimates when no transition exists', async () => {
    const missingCycleCostResponse = cloneResponse()
    missingCycleCostResponse.cycles[1].usage.cost_available = false
    fetchCodexQuotaHistory.mockResolvedValue(missingCycleCostResponse)
    await renderPanel()

    const completedSummary = cycleCard(1).querySelector('[data-codex-quota-cycle-summary]')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="used"] [data-codex-quota-summary-metric="cost"]')?.textContent)
      .toBe('usage_stats.credentials_quota_history_cost_missing')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="full-estimate"]')?.textContent).toContain('—')
    expect(completedSummary?.querySelector('[data-codex-quota-summary="estimated-unused"]')?.textContent).toContain('—')
  })

  it('queries only the newly selected real window series', async () => {
    const secondary = cloneResponse()
    secondary.selected_window = secondary.windows[1]
    secondary.cycles[0].last_remaining_percent = 19
    fetchCodexQuotaHistory.mockResolvedValueOnce(response).mockResolvedValueOnce(secondary)
    await renderPanel()
    await click(windowButton('secondary'))
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith(
      'codex-auth',
      { windowRole: 'secondary' },
      expect.any(AbortSignal),
    )
    const remaining = container.querySelector('[data-codex-quota-chart-cycle-id="2"] header dl')
    expect(remaining?.querySelector('dd')?.textContent).toBe('19%')
    expect(remaining?.getAttribute('data-status')).toBe('danger')
  })

  it('retries the real window that failed instead of falling back to the default window', async () => {
    fetchCodexQuotaHistory.mockReset()
    fetchCodexQuotaHistory
      .mockResolvedValueOnce(response)
      .mockRejectedValueOnce(new Error('secondary failed'))
      .mockResolvedValueOnce(response)
    await renderPanel()
    await click(windowButton('secondary'))
    expect(document.body.textContent).toContain('secondary failed')
    const retryButton = document.body.querySelector<HTMLButtonElement>('[role="status"] button')
    await act(async () => {
      retryButton!.click()
    })
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(3)
    expect(document.body.textContent).not.toContain('secondary failed')
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith(
      'codex-auth',
      { windowRole: 'secondary' },
      expect.any(AbortSignal),
    )
  })

  it('keeps the previous stored-cost cycle and retry action during costs_busy', async () => {
    fetchCodexQuotaHistory.mockReset()
      .mockResolvedValueOnce(response)
      .mockRejectedValueOnce(new ApiError('busy', 503, 'costs_busy'))
      .mockResolvedValueOnce(response)
    await renderPanel()
    expect(cycleCard(2)).not.toBeNull()
    await click(windowButton('secondary'))
    expect(cycleCard(2)).not.toBeNull()
    expect(document.body.textContent).toContain('usage_stats.costs_busy')
    await act(async () => document.body.querySelector<HTMLButtonElement>('[role="status"] button')!.click())
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(3)
  })

  it('draws an isolated Cost sample without adding points to a continuous line', async () => {
    const singleSampleResponse = cloneResponse()
    singleSampleResponse.cycles[0].transitions = singleSampleResponse.cycles[0].transitions.slice(0, 1)
    fetchCodexQuotaHistory.mockResolvedValue(singleSampleResponse)
    await renderPanel()
    const pointRadius = latestChartData!.datasets[1].pointRadius as (context: { dataIndex: number }) => number
    expect(pointRadius({ dataIndex: 0 })).toBeGreaterThan(0)
  })

  it('shows the pricing hint and hides the partial Cost median', async () => {
    const partialCostResponse = cloneResponse()
    const missingCostTransition = partialCostResponse.cycles[0].transitions[1]
    missingCostTransition.usage.cost_available = false
    missingCostTransition.cost_per_point_available = false
    partialCostResponse.cycles[0].usage.cost_available = false
    fetchCodexQuotaHistory.mockResolvedValue(partialCostResponse)
    await renderPanel()
    const warning = document.body.querySelector('[data-codex-quota-cost-warning]')
    expect(warning?.closest('header')).toBe(container.querySelector('[data-codex-quota-chart-cycle-id="2"] header'))
    expect(warning?.textContent).toBe('usage_stats.credentials_quota_history_cost_unavailable')
    expect(document.body.querySelector('[data-codex-quota-summary="median"]')?.textContent).toContain(
      'usage_stats.credentials_quota_history_cost_missing',
    )
    expect(document.body.querySelector('[data-codex-quota-summary="full-estimate"]')?.textContent).toContain(
      'usage_stats.credentials_quota_history_cost_missing',
    )
    expect(latestChartData?.datasets[1]?.data).toEqual([1, null, null, null])
    const pointRadius = latestChartData!.datasets[1].pointRadius as (context: { dataIndex: number }) => number
    expect(pointRadius({ dataIndex: 0 })).toBeGreaterThan(0)
    expect([1, 2, 3].map((dataIndex) => pointRadius({ dataIndex }))).toEqual([0, 0, 0])
  })

  it('uses a 24-hour clock while preserving the project-timezone wall clock from API timestamps', async () => {
    const offsetResponse = cloneResponse()
    offsetResponse.cycles[0].first_observed_at = '2026-08-21T13:01:00+08:00'
    offsetResponse.cycles[0].last_observed_at = '2026-08-21T14:02:00+08:00'
    fetchCodexQuotaHistory.mockResolvedValue(offsetResponse)
    await renderPanel()
    expect(document.body.textContent).toContain('"start":"Aug 21, 13:01"')
    expect(document.body.textContent).toContain('"end":"Aug 21, 14:02"')
    expect(document.body.textContent).toContain('"start":"Aug 17, 00:00"')
  })

  it('retains the existing dash fallback when an older response omits a timestamp', async () => {
    const missing = cloneResponse()
    missing.cycles[0].first_observed_at = undefined as unknown as string
    fetchCodexQuotaHistory.mockResolvedValueOnce(missing)
    await renderPanel()
    expect(container.querySelector('[data-codex-quota-observed-range]')?.textContent).toContain('"start":"—"')
    expect(cycleCard(2).querySelector('[data-codex-quota-cycle-select]')?.textContent).toContain('"value":"—"')
  })

  const fallbackResponse = (): CodexQuotaHistoryResponse => {
    const next = cloneResponse()
    Object.assign(next.cycles[0], {
      window_started_at: '2026-11-01T01:30:00-07:00', reset_at: '2026-11-01T04:00:00-08:00',
      effective_started_at: '2026-11-01T01:30:00-07:00', effective_ended_at: '2026-11-01T04:00:00-08:00',
      first_observed_at: '2026-11-01T01:50:00-07:00', last_observed_at: '2026-11-01T01:30:00-08:00',
    })
    Object.assign(next.cycles[0].transitions[0], {
      interval_started_at: '2026-11-01T01:50:00-07:00', interval_ended_at: '2026-11-01T01:20:00-08:00',
    })
    Object.assign(next.cycles[0].transitions[1], {
      interval_started_at: '2026-11-01T01:20:00-08:00', interval_ended_at: '2026-11-01T01:30:00-08:00',
    })
    Object.assign(next.cycles[1], {
      window_started_at: '2026-11-01T00:00:00-07:00', reset_at: '2026-11-01T01:30:00-07:00',
      effective_started_at: '2026-11-01T00:00:00-07:00', effective_ended_at: '2026-11-01T01:30:00-07:00',
      first_observed_at: '2026-11-01T00:10:00-07:00', last_observed_at: '2026-11-01T01:30:00-07:00',
      transitions: [{ ...completedTransition(), interval_started_at: '2026-11-01T00:10:00-07:00', interval_ended_at: '2026-11-01T01:30:00-07:00' }],
    })
    return next
  }

  it.each(['en-US', 'zh-CN'])('disambiguates every fallback timestamp surface in %s before selecting or expanding cycles', async (locale) => {
    translationState.language = locale
    fetchCodexQuotaHistory.mockResolvedValue(fallbackResponse())
    await renderPanel()
    const wallLabel = (clock: string) => new Intl.DateTimeFormat(locale, {
      month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: 'UTC',
    }).format(new Date(`2026-11-01T${clock}:00Z`))
    const first = `${wallLabel('01:30')} UTC-07:00`
    const second = `${wallLabel('01:30')} UTC-08:00`
    const before = `${wallLabel('01:50')} UTC-07:00`
    const after = `${wallLabel('01:20')} UTC-08:00`
    const chart = () => container.querySelector('[data-codex-quota-chart-cycle-id]')!
    const selection = () => cycleCard(2).querySelector('[data-codex-quota-cycle-select]')!
    const initialSelection = selection().textContent
    expect(initialSelection).toContain(first)
    expect(chart().querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain(first)
    expect(chart().querySelector('[data-codex-quota-observed-range]')?.textContent).toContain(before)
    expect(chart().querySelector('[data-codex-quota-observed-range]')?.textContent).toContain(second)
    expect(chart().querySelector('[data-codex-quota-accessible-summary]')?.textContent).toContain(`${before} → ${after}`)
    const tooltip = latestChartOptions!.plugins!.tooltip!.callbacks!.afterBody as unknown as (items: { dataIndex: number }[]) => string[]
    expect(tooltip([{ dataIndex: 0 }])).toContain(`usage_stats.credentials_quota_history_interval: ${before} → ${after}`)
    await click(detailButton(2))
    expect(cycleCard(2).querySelector('[data-codex-quota-transition]')?.textContent).toContain(before)
    expect(cycleCard(2).querySelector('[data-codex-quota-transition]')?.textContent).toContain(after)
    await selectCycle(1)
    expect(chart().querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain(first)
    await selectCycle(2)
    expect(selection().textContent).toBe(initialSelection)
    await openDelete(2)
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(first)
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(`${wallLabel('04:00')} UTC-08:00`)
    await click(modalButton('common.cancel'))
    expect(selection().textContent).toBe(initialSelection)
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
  })

  it('rebuilds timestamp suffixes for a new role response and language without changing normal text', async () => {
    fetchCodexQuotaHistory.mockResolvedValueOnce(fallbackResponse())
    await renderPanel()
    expect(container.textContent).toContain('UTC-07:00')
    const normal = cloneResponse()
    normal.selected_window = normal.windows[1]
    fetchCodexQuotaHistory.mockResolvedValueOnce(normal)
    await click(windowButton('secondary'))
    expect(container.textContent).not.toContain('UTC-')
    expect(container.querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain('"start":"Aug 17, 00:00","end":"Aug 24, 00:00"')
    translationState.language = 'zh-CN'
    await renderPanel()
    const start = new Intl.DateTimeFormat('zh-CN', { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: 'UTC' }).format(new Date('2026-08-17T00:00:00Z'))
    expect(container.querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain(start)
    expect(container.textContent).not.toContain('UTC-')
    fetchCodexQuotaHistory.mockResolvedValueOnce(fallbackResponse())
    await click(windowButton('primary'))
    expect(container.querySelector('[data-codex-quota-cycle-range]')?.textContent).toContain('UTC-07:00')
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(3)
  })
})
