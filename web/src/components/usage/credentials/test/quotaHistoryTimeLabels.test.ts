import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CodexQuotaHistoryCycle, CodexQuotaHistoryTransition } from '@/lib/types'
import { createQuotaHistoryTimeFormatter } from '../quotaHistoryTimeLabels'

const usage: CodexQuotaHistoryCycle['usage'] = {
  requests: 0, successful_requests: 0, failed_requests: 0,
  input_tokens: 0, output_tokens: 0, reasoning_tokens: 0,
  cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 0,
  total_cost_usd: 0, cost_available: true,
}

const cycleAt = (value: string, changes: Partial<CodexQuotaHistoryCycle> = {}): CodexQuotaHistoryCycle => ({
  id: 1, status: 'completed', window_seconds: 18000,
  window_started_at: value, reset_at: value,
  effective_started_at: value, effective_ended_at: value,
  first_observed_at: value, last_observed_at: value,
  first_remaining_percent: null, last_remaining_percent: null, observation_count: 0,
  usage, transitions: [], ...changes,
})

const transition = (start: string, end: string): CodexQuotaHistoryTransition => ({
  from_remaining_percent: 90, to_remaining_percent: 89, percentage_points: 1, is_direct: true,
  interval_started_at: start, interval_ended_at: end,
  usage, tokens_per_point: 0, cost_per_point: 0, cost_per_point_available: true,
})

const base = (value: string) => `base:${value}`

afterEach(() => vi.restoreAllMocks())

describe('quota history timestamp labels', () => {
  it('collects ambiguous identities from every cycle and folded transition without formatting them', () => {
    const first = '2026-11-01T01:30:01.123456789-07:00'
    const second = '2026-11-01T01:30:59-08:00'
    const format = vi.fn(base)
    const intl = vi.spyOn(Intl, 'DateTimeFormat')
    const label = createQuotaHistoryTimeFormatter([
      cycleAt('2026-11-01T00:00:00-07:00', { transitions: [transition(first, first)] }),
      cycleAt(second, { id: 2 }),
    ], format)
    expect(format).not.toHaveBeenCalled()
    expect(intl).not.toHaveBeenCalled()
    expect(label(first)).toBe(`${base(first)} UTC-07:00`)
    expect(label(second)).toBe(`${base(second)} UTC-08:00`)
    expect(format).toHaveBeenCalledTimes(2)
    expect(label('not in this response')).toBe('base:not in this response')
  })

  it.each([
    ['LA fallback', '2026-11-01T01:45:00-07:00', '2026-11-01T01:45:00-08:00', '-07:00', '-08:00'],
    ['Lord Howe half hour', '2026-04-05T01:45:00+11:00', '2026-04-05T01:45:00+10:30', '+11:00', '+10:30'],
  ])('distinguishes repeated local minutes for %s using only response offsets', (_name, first, second, firstOffset, secondOffset) => {
    const label = createQuotaHistoryTimeFormatter([cycleAt(first), cycleAt(second)], base)
    expect(label(first)).toBe(`${base(first)} UTC${firstOffset}`)
    expect(label(second)).toBe(`${base(second)} UTC${secondOffset}`)
  })

  it.each([
    ['theoretical/deletion', 'window_started_at', 'reset_at'],
    ['effective', 'effective_started_at', 'effective_ended_at'],
    ['observed', 'first_observed_at', 'last_observed_at'],
  ] as const)('marks both %s range ends when offsets differ even if wall minutes differ', (_name, leftKey, rightKey) => {
    const first = '2026-11-01T01:50:00-07:00'
    const second = '2026-11-01T01:20:00-08:00'
    const label = createQuotaHistoryTimeFormatter([
      cycleAt('2026-10-30T00:00:00-07:00', { [leftKey]: first, [rightKey]: second }),
    ], base)
    expect(label(first)).toBe(`${base(first)} UTC-07:00`)
    expect(label(second)).toBe(`${base(second)} UTC-08:00`)
  })

  it('marks the actual current-cycle display range in addition to raw effective/theoretical ranges', () => {
    const start = '2026-11-01T01:50:00-07:00'
    const end = '2026-11-01T02:20:00-08:00'
    const label = createQuotaHistoryTimeFormatter([
      cycleAt('2026-11-01T00:00:00-08:00', {
        status: 'current', reset_at: end,
        effective_started_at: start, effective_ended_at: '2026-11-01T01:55:00-07:00',
      }),
    ], base)
    expect(label(start)).toBe(`${base(start)} UTC-07:00`)
    expect(label(end)).toBe(`${base(end)} UTC-08:00`)
  })

  it('marks a folded cross-offset transition before its cycle is selected', () => {
    const start = '2026-11-01T01:50:00-07:00'
    const end = '2026-11-01T01:20:00-08:00'
    const label = createQuotaHistoryTimeFormatter([
      cycleAt('2026-10-30T00:00:00-07:00'),
      cycleAt('2026-11-01T00:00:00-07:00', { id: 2, transitions: [transition(start, end)] }),
    ], base)
    expect(label(start)).toBe(`${base(start)} UTC-07:00`)
    expect(label(end)).toBe(`${base(end)} UTC-08:00`)
  })

  it('uses a plus sign for UTC and preserves fractional/positive offsets', () => {
    const start = '2026-10-21T00:00:00Z'
    const end = '2026-10-21T06:00:00+05:30'
    const label = createQuotaHistoryTimeFormatter([cycleAt(start, { reset_at: end })], base)
    expect(label(start)).toBe(`${base(start)} UTC+00:00`)
    expect(label(end)).toBe(`${base(end)} UTC+05:30`)
  })

  it.each([
    ['same offset within one minute', '2026-10-21T01:30:00-07:00', '2026-10-21T01:30:59-07:00'],
    ['same minute on different dates', '2026-10-21T01:30:00-07:00', '2026-11-01T01:30:00-08:00'],
    ['same instant with different wall clocks', '2026-11-01T01:30:00-07:00', '2026-11-01T00:30:00-08:00'],
    ['equivalent zero offsets', '2026-10-21T01:30:00Z', '2026-10-21T01:30:30+00:00'],
    ['negative zero offset', '2026-10-21T01:30:00Z', '2026-10-21T01:30:30-00:00'],
  ])('keeps ordinary text unchanged for %s', (_name, first, second) => {
    const label = createQuotaHistoryTimeFormatter([cycleAt(first), cycleAt(second)], base)
    expect(label(first)).toBe(base(first))
    expect(label(second)).toBe(base(second))
  })

  it('leaves missing, invalid and unzoned legacy values to the original fallback', () => {
    const values = [undefined as unknown as string, '', 'invalid timestamp', '2026-11-01T01:30:00', '2026-11-01T01:30:00+99:00']
    const label = createQuotaHistoryTimeFormatter(values.map((value) => cycleAt(value)), (value) => value || '—')
    for (const value of values) expect(label(value)).toBe(value || '—')
  })

  it('replaces suffix identities for a new response while an earlier formatter retains its own result', () => {
    const first = '2026-11-01T01:30:00-07:00'
    const second = '2026-11-01T01:30:00-08:00'
    const earlier = createQuotaHistoryTimeFormatter([cycleAt(first), cycleAt(second)], base)
    const next = createQuotaHistoryTimeFormatter([cycleAt(first)], base)
    expect(earlier(first)).toBe(`${base(first)} UTC-07:00`)
    expect(next(first)).toBe(base(first))
  })
})
