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

    expect(html).toContain('aria-label="10:00 - 10:10|usage_stats.credentials_health_status_success|99/1|99.0%" style="height:21.9px"')
    expect(html).toContain('aria-label="10:10 - 10:20|usage_stats.credentials_health_status_success|9/1|90.0%" style="height:20.8px"')
    expect(html).toContain('aria-label="10:20 - 10:30|usage_stats.credentials_health_status_warning|1/1|50.0%" style="height:16px"')
    expect(html).toContain('aria-label="10:30 - 10:40|usage_stats.credentials_health_status_failure|1/2|33.3%" style="height:14px"')
    expect(html).toContain('aria-label="10:40 - 10:50|usage_stats.credentials_health_status_failure|0/1|0.0%" style="height:10px"')
    expect(html).toMatch(/usage_stats\.credentials_health_status_empty\|0\/0\|0\.0%" style="height:5px"/)
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

  it('uses the same sample-aware policy for the 5-hour summary', () => {
    const renderSummary = (success: number, failure: number) => renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={healthWithBuckets([bucket(0, success, failure)])}
      />,
    )

    expect(renderSummary(9, 1)).toContain('usage_stats.credentials_health_summary_healthy')
    expect(renderSummary(90, 10)).toContain('usage_stats.credentials_health_summary_degraded')
    expect(renderSummary(95, 5)).toContain('usage_stats.credentials_health_summary_healthy')
    expect(renderSummary(1, 1)).toContain('usage_stats.credentials_health_summary_degraded')
    expect(renderSummary(1, 2)).toContain('usage_stats.credentials_health_summary_unhealthy')
  })
})
