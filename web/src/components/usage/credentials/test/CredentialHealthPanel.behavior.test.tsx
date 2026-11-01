// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import type { UsageCredentialHealth } from '@/lib/types'
import { CredentialHealthPanel } from '../CredentialHealthPanel'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => {
      if (key === 'usage_stats.credentials_health_bucket_aria') {
        return `${params?.timeRange}|${params?.status}|${params?.successCount}/${params?.failureCount}|${params?.rate}`
      }
      if (key === 'usage_stats.credentials_health_failures_5h') {
        return `Failed requests in 5h: ${params?.count} · latest ${params?.timeRange}`
      }
      return key
    },
  }),
}))

const healthWithBuckets = (
  buckets: UsageCredentialHealth['buckets'],
  overrides: Partial<UsageCredentialHealth> = {},
): UsageCredentialHealth => {
  const totalSuccess = buckets.reduce((total, bucket) => total + bucket.success, 0)
  const totalFailure = buckets.reduce((total, bucket) => total + bucket.failure, 0)
  const total = totalSuccess + totalFailure
  return {
    window_seconds: 18_000,
    bucket_seconds: 600,
    window_start: '2026-07-16T10:00:00+08:00',
    window_end: '2026-07-16T15:00:00+08:00',
    total_success: totalSuccess,
    total_failure: totalFailure,
    success_rate: total > 0 ? totalSuccess / total * 100 : 0,
    input_tokens: 0,
    cache_read_tokens: 0,
    buckets,
    ...overrides,
  }
}

const bucket = (minute: number, success: number, failure: number): UsageCredentialHealth['buckets'][number] => ({
  start_time: `2026-07-16T10:${String(minute).padStart(2, '0')}:00+08:00`,
  end_time: `2026-07-16T10:${String(minute + 10).padStart(2, '0')}:00+08:00`,
  success,
  failure,
  rate: success + failure > 0 ? success / (success + failure) : 0,
})

describe('CredentialHealthPanel health semantics', () => {
  it.each([
    { label: 'undefined', rate: undefined, expected: null },
    { label: 'null', rate: null, expected: '—' },
    { label: 'zero', rate: 0, expected: '0.00%' },
  ])('handles a $label 5h cache rate explicitly', ({ rate, expected }) => {
    const html = renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([])}
        windowCacheReadRate={rate}
      />,
    )

    if (expected === null) {
      expect(html).not.toContain('usage_stats.credentials_health_cache_rate_5h')
      return
    }
    expect(html).toContain('usage_stats.credentials_health_cache_rate_5h')
    expect(html).toContain(`>${expected}<`)
  })

  it('raises the green threshold logarithmically with the request sample size', () => {
    const html = renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([
          bucket(0, 9, 1),
          bucket(10, 90, 10),
          bucket(20, 95, 5),
          bucket(30, 1, 1),
          bucket(40, 1, 2),
        ])}
      />,
    )

    expect(html).toContain('10:00 - 10:10|usage_stats.credentials_health_status_success|9/1|90.0%')
    expect(html).toContain('10:10 - 10:20|usage_stats.credentials_health_status_warning|90/10|90.0%')
    expect(html).toContain('10:20 - 10:30|usage_stats.credentials_health_status_success|95/5|95.0%')
    expect(html).toContain('10:30 - 10:40|usage_stats.credentials_health_status_warning|1/1|50.0%')
    expect(html).toContain('10:40 - 10:50|usage_stats.credentials_health_status_failure|1/2|33.3%')
  })

  it('scales every populated bar continuously by success rate above the empty baseline', () => {
    const html = renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([
          bucket(0, 99, 1),
          bucket(10, 9, 1),
          bucket(20, 1, 1),
          bucket(30, 1, 2),
          bucket(40, 0, 1),
        ])}
      />,
    )

    const bars = Array.from(html.matchAll(/role="listitem"[^>]*aria-label="([^"]+)" style="height:([\d.]+)px"/g),
      ([, label, height]) => ({ label, height: Number(height) }))
    expect(bars).toHaveLength(30)
    expect(bars.slice(0, 5).map(({ label }) => label.split('|').at(-1))).toEqual([
      '99.0%', '90.0%', '50.0%', '33.3%', '0.0%',
    ])
    const heights = bars.slice(0, 6).map(({ height }) => height)
    for (let index = 1; index < heights.length; index += 1) {
      expect(heights[index - 1]).toBeGreaterThan(heights[index])
    }
    expect(heights[5]).toBeGreaterThan(0)

  })

  it('uses exact request totals when the API success rate is unavailable', () => {
    const html = renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([bucket(0, 99, 1)], { success_rate: Number.NaN })}
      />,
    )

    expect(html).toContain('<strong>99.0%</strong>')
    expect(html).toContain('usage_stats.credentials_health_summary_healthy')
    expect(html).toContain('Failed requests in 5h: 1 · latest 10:00 - 10:10')
    expect(html).not.toContain('usage_stats.credentials_health_summary_degraded')
  })

  it.each(['full', 'compact'] as const)('shows all four 5-hour summary states with the same sample-aware policy in %s panels', (variant) => {
    const renderSummary = (success: number, failure: number) => renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([bucket(0, success, failure)])}
        variant={variant}
        showCompactMeta
        lastUsedAt="2026-07-16T14:58:00+08:00"
        windowCacheReadRate={null}
      />,
    )

    expect(renderSummary(9, 1)).toContain('usage_stats.credentials_health_summary_healthy')
    expect(renderSummary(90, 10)).toContain('usage_stats.credentials_health_summary_degraded')
    expect(renderSummary(95, 5)).toContain('usage_stats.credentials_health_summary_healthy')
    expect(renderSummary(1, 1)).toContain('usage_stats.credentials_health_summary_degraded')
    expect(renderSummary(1, 2)).toContain('usage_stats.credentials_health_summary_unhealthy')
    const quiet = renderSummary(0, 0)
    expect(quiet).toContain('usage_stats.credentials_health_summary_quiet')
    expect(quiet).toContain('0.0%')
    expect(quiet).toContain('—')
    expect(quiet).toContain(variant === 'compact' ? '07-16 14:58' : '07/16 14:58')
  })

  it('keeps the same ordered buckets and tooltip details in compact cards and the default full panel', () => {
    const health = healthWithBuckets([
      bucket(0, 9, 1),
      bucket(10, 90, 10),
      bucket(20, 1, 2),
    ])
    const renderPanel = (variant?: 'compact') => {
      const element = document.createElement('div')
      element.innerHTML = renderToStaticMarkup(<CredentialHealthPanel
        displayName="Provider Key"
        health={health}
        variant={variant}
        lastUsedAt="2026-07-16T14:58:00+08:00"
        statsUpdatedAt="2026-07-16T15:00:00+08:00"
        windowCacheReadRate={37.5}
      />)
      return element
    }
    const compact = renderPanel('compact')
    const full = renderPanel()
    const bucketLabels = (element: HTMLElement) => Array.from(element.querySelectorAll('[role="listitem"]'), (cell) => cell.getAttribute('aria-label'))
    const bucketTooltips = (element: HTMLElement) => Array.from(element.querySelectorAll('[role="tooltip"]'), (tooltip) => tooltip.textContent)

    expect(bucketLabels(compact)).toHaveLength(30)
    expect(bucketLabels(compact)).toEqual(bucketLabels(full))
    expect(bucketLabels(compact).slice(0, 4)).toEqual([
      '10:00 - 10:10|usage_stats.credentials_health_status_success|9/1|90.0%',
      '10:10 - 10:20|usage_stats.credentials_health_status_warning|90/10|90.0%',
      '10:20 - 10:30|usage_stats.credentials_health_status_failure|1/2|33.3%',
      '10:30 - 10:40|usage_stats.credentials_health_status_empty|0/0|0.0%',
    ])
    expect(bucketLabels(compact).at(-1)).toContain('14:50 - 15:00')
    expect(bucketTooltips(compact)).toEqual(bucketTooltips(full))
    expect(full.textContent).toContain('Failed requests in 5h: 13 · latest 10:20 - 10:30')
    expect(full.textContent).toContain('37.50%')
    expect(full.textContent).toContain('07/16 14:58')
    expect(full.textContent).toContain('07/16 15:00')
  })

  it('updates bucket details and accessible names when health data changes', async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const renderHealth = async (success: number, failure: number) => {
      await act(async () => root.render(<CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([bucket(0, success, failure)])}
        variant="compact"
      />))
    }
    try {
      await renderHealth(9, 1)
      expect(container.querySelector('[role="tooltip"]')?.textContent).toContain('90.0%')

      await renderHealth(1, 2)

      const cell = container.querySelector<HTMLElement>('[role="listitem"]')!
      expect(cell.getAttribute('aria-label')).toBe('10:00 - 10:10|usage_stats.credentials_health_status_failure|1/2|33.3%')
      const tooltip = cell.querySelector('[role="tooltip"]')!
      expect(tooltip.textContent).toContain('usage_stats.credentials_health_ok 1')
      expect(tooltip.textContent).toContain('usage_stats.credentials_health_fail 2')
      expect(tooltip.textContent).toContain('33.3%')
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  })

})
