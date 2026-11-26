// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import type { ChartData, ChartOptions } from 'chart.js'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CodexQuotaHistoryResponse } from '@/lib/types'
import { useThemeStore } from '@/stores'
import { CodexQuotaHistoryPanel } from '../CodexQuotaHistoryPanel'

const fetchCodexQuotaHistory = vi.fn()
const deleteCodexQuotaHistoryCycle = vi.fn()

let latestChartData: ChartData<'bar' | 'line', Array<number | null>, string> | null = null

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchCodexQuotaHistory: (...args: unknown[]) => fetchCodexQuotaHistory(...args),
    deleteCodexQuotaHistoryCycle: (...args: unknown[]) => deleteCodexQuotaHistoryCycle(...args),
  }
})

vi.mock('react-chartjs-2', () => ({
  Chart: (props: { data: ChartData<'bar' | 'line', Array<number | null>, string>; options: ChartOptions<'bar' | 'line'> }) => {
    latestChartData = props.data
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

const emptyUsage = {
  requests: 0,
  successful_requests: 0,
  failed_requests: 0,
  input_tokens: 0,
  output_tokens: 0,
  reasoning_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  total_tokens: 0,
  total_cost_usd: 0,
  cost_available: true,
}

describe('CodexQuotaHistoryPanel usage_ready placeholders', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    latestChartData = null
    fetchCodexQuotaHistory.mockReset()
    deleteCodexQuotaHistoryCycle.mockReset()
    useThemeStore.setState({ resolvedTheme: 'light' })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('shows dash placeholders for uncomputed cycles and excludes uncomputed transitions from chart, summaries, and accessible list', async () => {
    const payload: CodexQuotaHistoryResponse = {
      generated_at: '2026-11-25T12:00:00Z',
      range_start: '2026-10-26T12:00:00Z',
      windows: [
        { window_role: 'primary', window_kind: 'five_hour', window_seconds: 18000, has_current_cycle: true, last_observed_at: '2026-11-25T11:50:00Z' },
      ],
      selected_window: { window_role: 'primary', window_kind: 'five_hour', window_seconds: 18000, has_current_cycle: true, last_observed_at: '2026-11-25T11:50:00Z' },
      cycles: [
        {
          id: 101,
          status: 'completed',
          window_seconds: 18000,
          window_started_at: '2026-11-25T05:00:00Z',
          reset_at: '2026-11-25T10:00:00Z',
          effective_started_at: '2026-11-25T05:00:00Z',
          effective_ended_at: '2026-11-25T10:00:00Z',
          first_observed_at: '2026-11-25T06:00:00Z',
          last_observed_at: '2026-11-25T09:00:00Z',
          first_remaining_percent: 90,
          last_remaining_percent: 88,
          observation_count: 3,
          usage_ready: false,
          usage: emptyUsage,
          transitions: [
            {
              from_remaining_percent: 90,
              to_remaining_percent: 89,
              percentage_points: 1,
              is_direct: true,
              interval_started_at: '2026-11-25T06:00:00Z',
              interval_ended_at: '2026-11-25T07:00:00Z',
              usage_ready: true,
              usage: { ...emptyUsage, requests: 2, successful_requests: 2, total_tokens: 2000, total_cost_usd: 0.40 },
              tokens_per_point: 2000,
              cost_per_point: 0.40,
              cost_per_point_available: true,
            },
            {
              from_remaining_percent: 89,
              to_remaining_percent: 88,
              percentage_points: 1,
              is_direct: true,
              interval_started_at: '2026-11-25T07:00:00Z',
              interval_ended_at: '2026-11-25T09:00:00Z',
              usage_ready: false,
              usage: emptyUsage,
              tokens_per_point: 0,
              cost_per_point: 0,
              cost_per_point_available: true,
            },
          ],
        },
      ],
    }
    fetchCodexQuotaHistory.mockResolvedValue(payload)

    await act(async () => {
      root.render(<CodexQuotaHistoryPanel authIndex="auth-placeholder" />)
    })

    // 图表与无障碍摘要必须过滤掉 usage_ready=false 的 89% -> 88% 段，仅保留 90% -> 89%。
    expect(latestChartData?.labels).toEqual(['90% → 89%'])
    const accessibleItems = container.querySelectorAll('[data-codex-quota-accessible-summary="true"] li')
    expect(accessibleItems).toHaveLength(1)
    expect(accessibleItems[0].textContent).toContain('90% → 89%')

    // 周期卡片的 Used 和 Estimated unused 在 usage_ready=false 时显示 —，不能伪造 0。
    const usedRow = container.querySelector('[data-codex-quota-summary="used"]')
    expect(usedRow?.textContent).toContain('—')
    expect(usedRow?.querySelector('[data-codex-quota-summary-metric]')).toBeNull()
    const unusedRow = container.querySelector('[data-codex-quota-summary="estimated-unused"]')
    expect(unusedRow?.textContent).toContain('—')

    // 展开明细后，未就绪下降段显示 —，已就绪下降段正常展示数值。
    const toggle = container.querySelector('button[aria-expanded="false"]') as HTMLButtonElement
    await act(async () => {
      toggle.click()
    })
    const rows = container.querySelectorAll('[data-codex-quota-transition]')
    expect(rows).toHaveLength(2)
    expect(rows[0].textContent).toContain('Token')
    expect(rows[0].textContent).toContain('$0.40')
    expect(rows[1].textContent).toContain('—')
    expect(rows[1].textContent).not.toContain('0 Token')
  })
})
