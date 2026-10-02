import { describe, expect, it } from 'vitest'
import type { PricingRecalculationOptions } from '@/lib/types'
import { buildPricingRecalculationHours } from '../pricingRecalculationHours'

const options = (overrides: Partial<PricingRecalculationOptions>): PricingRecalculationOptions => ({
  timezone: 'UTC', earliest_start: null, latest_start: null,
  step_seconds: 3600, max_days: 30, config_revision: 1, ...overrides,
})

describe('buildPricingRecalculationHours', () => {
  it('preserves the deployment timezone half-hour offset while stepping by actual hours', () => {
    expect(buildPricingRecalculationHours(options({
      timezone: 'Asia/Kolkata',
      earliest_start: '2026-09-22T09:30:00+05:30',
      latest_start: '2026-09-22T11:30:00+05:30',
    }))).toEqual([
      { value: '2026-09-22T09:30:00+05:30', date: '2026-09-22', hourLabel: '09:30' },
      { value: '2026-09-22T10:30:00+05:30', date: '2026-09-22', hourLabel: '10:30' },
      { value: '2026-09-22T11:30:00+05:30', date: '2026-09-22', hourLabel: '11:30' },
    ])
  })

  it('keeps both fall-back hours and labels their distinct offsets without adding labels to ordinary hours', () => {
    expect(buildPricingRecalculationHours(options({
      timezone: 'America/New_York',
      earliest_start: '2026-11-01T00:00:00-04:00',
      latest_start: '2026-11-01T03:00:00-05:00',
    }))).toEqual([
      { value: '2026-11-01T00:00:00-04:00', date: '2026-11-01', hourLabel: '00:00' },
      { value: '2026-11-01T01:00:00-04:00', date: '2026-11-01', hourLabel: '01:00 UTC-04:00' },
      { value: '2026-11-01T01:00:00-05:00', date: '2026-11-01', hourLabel: '01:00 UTC-05:00' },
      { value: '2026-11-01T02:00:00-05:00', date: '2026-11-01', hourLabel: '02:00' },
      { value: '2026-11-01T03:00:00-05:00', date: '2026-11-01', hourLabel: '03:00' },
    ])
  })

  it('does not invent the missing spring-forward hour or a range when the server has no legal start', () => {
    expect(buildPricingRecalculationHours(options({
      timezone: 'America/New_York',
      earliest_start: '2026-03-08T01:00:00-05:00',
      latest_start: '2026-03-08T04:00:00-04:00',
    })).map((hour) => hour.hourLabel)).toEqual(['01:00', '03:00', '04:00'])
    expect(buildPricingRecalculationHours(options({ earliest_start: null, latest_start: null }))).toEqual([])
  })
})
