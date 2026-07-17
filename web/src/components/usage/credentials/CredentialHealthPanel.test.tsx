import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialHealthPanel } from './CredentialHealthPanel'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => {
      if (key === 'usage_stats.credentials_health_grid_aria') {
        return `${params?.name} ${key}`
      }
      if (key === 'usage_stats.credentials_health_bucket_aria') {
        return `${params?.timeRange} ${params?.status} ${params?.successCount} ${params?.failureCount} ${params?.rate} ${key}`
      }
      if (key === 'usage_stats.credentials_health_last_failure') {
        return `last failure ${params?.timeRange}`
      }
      if (key === 'usage_stats.credentials_health_failures_5h') {
        return `Failed requests in 5h: ${params?.count} · latest ${params?.timeRange}`
      }
      if (key === 'usage_stats.credentials_health_time_summary') {
        return `used ${params?.lastUsed} updated ${params?.statsUpdated}`
      }
      if (key === 'usage_stats.credentials_health_time_summary_used') {
        return `used ${params?.lastUsed}`
      }
      if (key === 'usage_stats.credentials_health_time_summary_updated') {
        return `updated ${params?.statsUpdated}`
      }
      return key
    },
  }),
}))

describe('CredentialHealthPanel', () => {
  it('renders compact 5h health buckets with contextual health metadata', () => {
    vi.setSystemTime(new Date('2026-05-10T10:30:00Z'))
    try {
      const html = renderToStaticMarkup(
        <CredentialHealthPanel
          displayName="Provider Key"
          lastUsedAt="2026-05-10T10:00:00Z"
          statsUpdatedAt="2026-05-10T10:02:00Z"
        />,
      )

      expect(html).not.toContain('<button')
      expect(html.match(/role="tooltip"/g)).toHaveLength(30)
      expect(html.match(/role="tooltip" aria-hidden="true"/g)).toHaveLength(30)
      expect(html).toContain('usage_stats.credentials_health_last_5h')
      expect(html).toContain('0.0%')
      expect(html).toContain('usage_stats.credentials_health_ok')
      expect(html).toContain('usage_stats.credentials_health_fail')
      expect(html).toContain('usage_stats.credentials_health_status_empty')
      expect(html).toContain('usage_stats.credentials_health_summary_quiet')
      expect(html).toContain('usage_stats.credentials_health_no_requests_5h')
      expect(html).toContain('05/10 10:00')
      expect(html).toContain('05/10 10:02')
      expect(html.match(/<svg/g) ?? []).toHaveLength(2)
      expect(html).not.toContain('used 05/10 10:00 updated 05/10 10:02')
      expect(html).not.toContain('>usage_stats.credentials_last_used<')
      expect(html).not.toContain('>usage_stats.credentials_stats_updated<')
      expect(html).toContain('Provider Key usage_stats.credentials_health_grid_aria')
      expect(html).not.toContain('mock request health')
    } finally {
      vi.useRealTimers()
    }
  })

  it('renders API credential health buckets instead of the empty placeholder', () => {
    const html = renderToStaticMarkup(
      <CredentialHealthPanel
        displayName="Provider Key"
        health={{
          window_seconds: 18_000,
          bucket_seconds: 600,
          window_start: '2026-05-10T05:30:00+08:00',
          window_end: '2026-05-10T10:30:00+08:00',
          total_success: 2,
          total_failure: 1,
          success_rate: 66.6667,
          buckets: [
            {
              start_time: '2026-05-10T10:10:00+08:00',
              end_time: '2026-05-10T10:20:00+08:00',
              success: 2,
              failure: 1,
              rate: 0.666667,
            },
          ],
        }}
      />,
    )

    expect(html).not.toContain('<button')
    expect(html.match(/role="tooltip"/g)).toHaveLength(30)
    expect(html.match(/role="tooltip" aria-hidden="true"/g)).toHaveLength(30)
    expect(html).toContain('66.7%')
    expect(html).toContain('usage_stats.credentials_health_ok')
    expect(html).toContain('usage_stats.credentials_health_fail')
    expect(html).toContain('10:10 - 10:20')
    expect(html).toContain('10:10 - 10:20 usage_stats.credentials_health_status_warning 2 1 66.7% usage_stats.credentials_health_bucket_aria')
    expect(html).toContain('66.7%')
    expect(html).toContain('usage_stats.credentials_health_summary_degraded')
    expect(html).toContain('Failed requests in 5h: 1 · latest 10:10 - 10:20')
  })
})
