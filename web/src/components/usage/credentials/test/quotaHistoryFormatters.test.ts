import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatCompactNumber } from '@/utils/usage'

const NativeDateTimeFormat = Intl.DateTimeFormat
const NativeNumberFormat = Intl.NumberFormat

// C3 原函数作逐字参考，包含相同 Date 解析、墙上时间映射和固定 Intl options。
function originalFormatDateTime(value: string, locale?: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value || '—'
  const offsetTime = value.trim().match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i)
  const displayDate = offsetTime
    ? new Date(Date.UTC(
      Number(offsetTime[1]), Number(offsetTime[2]) - 1, Number(offsetTime[3]),
      Number(offsetTime[4]), Number(offsetTime[5]), Number(offsetTime[6]),
    ))
    : date
  return new NativeDateTimeFormat(locale, {
    month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
    ...(offsetTime ? { timeZone: 'UTC' } : {}),
  }).format(displayDate)
}

function originalSummaryRequests(value: number, browserLocale?: string): string {
  if (Math.abs(value) >= 1_000) return formatCompactNumber(value)
  return new NativeNumberFormat(browserLocale, {
    maximumFractionDigits: Math.abs(value) >= 100 ? 0 : Math.abs(value) >= 10 ? 1 : 2,
  }).format(value)
}

function originalSummaryPercentage(value: number, browserLocale?: string): string {
  return `${new NativeNumberFormat(browserLocale, { maximumFractionDigits: 2 }).format(value)}%`
}

const dateValues = [
  '2026-08-21T00:00:00Z', '2026-12-31T23:59:59.999999999+14:00', '2027-01-01T00:00:00-12:00',
  '2026-08-21T23:59:00+05:45', '2026-08-21 12:34:56+08:00', '2026-08-21T12:34:56z',
  '2026-11-01T01:30:00-07:00', '2026-11-01T01:30:59-08:00',
  '2026-04-05T01:45:00+11:00', '2026-04-05T01:45:00+10:30',
  '2026-03-08T01:59:59-08:00', '2026-03-08T03:00:00-07:00',
  '2026-08-21T12:34:56+00:00', '2026-08-21T12:34:56-00:00',
  '2026-08-21T12:34:56', '2026-08-21 12:34:56', '2026-08-21',
  'Fri, 21 Aug 2026 04:34:56 GMT', '2026-02-30T12:00:00+08:00',
  ' 2026-08-21T12:34:56Z ', 'invalid timestamp', '2026-08-21T12:34:56+99:00', '',
  undefined as unknown as string,
  ...Array.from({ length: 10 }, (_, digits) => `2026-08-21T12:34:56${digits ? `.${'123456789'.slice(0, digits)}` : ''}Z`),
]

const numberValues = [
  0, -0, Number.MIN_VALUE, 0.0049, 0.005, 0.009, 1, 1.234, 9.9949, 9.995, 9.9999,
  10, 10.04, 10.05, 99.94, 99.95, 99.99, 99.995, 100, 100.005,
  999.49, 999.5, 999.99, 1000, 1001.234, 999_999.99, 1_000_000, 1_000_000_000,
  Number.MAX_VALUE, Number.POSITIVE_INFINITY, Number.NaN, undefined as unknown as number,
].flatMap((value) => [value, -value])

beforeEach(() => vi.resetModules())
afterEach(() => vi.restoreAllMocks())

describe('quota history formatter equivalence', () => {
  it.each([undefined, 'en', 'en-US', 'zh-CN', 'zh-TW', 'de-DE', 'ar-EG', 'en-US-u-ca-japanese-hc-h12-nu-arab', 'zz-ZZ'])(
    'preserves every original date string for locale %s', async (locale) => {
      const { createQuotaHistoryDateTimeFormatter } = await import('../quotaHistoryFormatters')
      const format = createQuotaHistoryDateTimeFormatter(locale)
      for (const value of dateValues) expect(format(value), `${locale}: ${value}`).toBe(originalFormatDateTime(value, locale))
      // 原函数本来不接受 null，不把它静默变成新的 fallback。
      expect(() => originalFormatDateTime(null as unknown as string, locale)).toThrow(TypeError)
      expect(() => format(null as unknown as string)).toThrow(TypeError)
    },
  )

  it.each(['en-US', 'zh-CN', 'de-DE', 'ar-EG'])('preserves requests/percentages in browser locale %s independently of app language', async (browserLocale) => {
    const { createQuotaHistoryDateTimeFormatter, formatQuotaSummaryRequests, formatQuotaSummaryPercentage } = await import('../quotaHistoryFormatters')
    const constructors = vi.spyOn(Intl, 'NumberFormat').mockImplementation(function (locale, options) {
      return new NativeNumberFormat(locale ?? browserLocale, options)
    })
    for (const appLocale of ['en-US', 'zh-CN']) {
      const formatDate = createQuotaHistoryDateTimeFormatter(appLocale)
      expect(formatDate(dateValues[0])).toBe(originalFormatDateTime(dateValues[0], appLocale))
      for (const value of numberValues) {
        expect(formatQuotaSummaryRequests(value), `requests: ${value}`).toBe(originalSummaryRequests(value, browserLocale))
        expect(formatQuotaSummaryPercentage(value), `percentage: ${value}`).toBe(originalSummaryPercentage(value, browserLocale))
      }
    }
    expect(constructors).toHaveBeenCalledTimes(3)
    expect(constructors.mock.calls.map(([locale]) => locale)).toEqual([undefined, undefined, undefined])
    expect(constructors.mock.calls.map(([, options]) => options?.maximumFractionDigits).sort()).toEqual([0, 1, 2])
  })
})

describe('quota history formatter lifetime', () => {
  it('resolves defaults once per context and reuses UTC/local instances without constructing Intl during lookups', async () => {
    const { createQuotaHistoryDateTimeFormatter } = await import('../quotaHistoryFormatters')
    const constructors = vi.spyOn(Intl, 'DateTimeFormat')
    const format = createQuotaHistoryDateTimeFormatter('en-US')
    expect(constructors).toHaveBeenCalledTimes(1)
    for (let index = 0; index < 50; index++) {
      expect(format('2026-08-21T00:00:00Z')).toBe(originalFormatDateTime('2026-08-21T00:00:00Z', 'en-US'))
      expect(format('2026-08-21T00:00:00')).toBe(originalFormatDateTime('2026-08-21T00:00:00', 'en-US'))
    }
    expect(constructors).toHaveBeenCalledTimes(3)
    const next = createQuotaHistoryDateTimeFormatter('en-us')
    next('2026-08-21T01:00:00Z')
    next('2026-08-21T01:00:00')
    expect(constructors).toHaveBeenCalledTimes(4)
  })

  it('keys local formatters by the resolved browser timezone while sharing UTC formatters', async () => {
    const { createQuotaHistoryDateTimeFormatter } = await import('../quotaHistoryFormatters')
    let browserZone = 'Asia/Shanghai'
    const constructors = vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function (locale, options) {
      return new NativeDateTimeFormat(locale, { ...options, timeZone: options?.timeZone ?? browserZone })
    })
    const localValue = '2026-08-21T00:00:00'
    const utcValue = '2026-08-21T00:00:00Z'
    const expectedLocal = (zone: string) => new NativeDateTimeFormat('en-US', {
      month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: zone,
    }).format(new Date(localValue))
    const shanghai = createQuotaHistoryDateTimeFormatter('en-US')
    expect(shanghai(localValue)).toBe(expectedLocal(browserZone))
    expect(shanghai(utcValue)).toBe(originalFormatDateTime(utcValue, 'en-US'))
    browserZone = 'America/Los_Angeles'
    const losAngeles = createQuotaHistoryDateTimeFormatter('en-US')
    expect(losAngeles(localValue)).toBe(expectedLocal(browserZone))
    expect(losAngeles(utcValue)).toBe(originalFormatDateTime(utcValue, 'en-US'))
    expect(shanghai(localValue)).toBe(expectedLocal('Asia/Shanghai'))
    expect(constructors.mock.calls.filter(([, options]) => options?.month === 'short')).toHaveLength(3)
  })

  it('evicts after 16 keys and retains recently used instances across responses', async () => {
    const { createQuotaHistoryDateTimeFormatter } = await import('../quotaHistoryFormatters')
    const constructors = vi.spyOn(Intl, 'DateTimeFormat')
    const locales = ['en-US', 'zh-CN', 'fr-FR', 'de-DE', 'ja-JP', 'ko-KR', 'es-ES', 'it-IT', 'pt-BR', 'ru-RU', 'nl-NL', 'pl-PL', 'sv-SE', 'fi-FI', 'tr-TR', 'ar-EG', 'hi-IN']
    const value = '2026-08-21T12:34:56Z'
    const formats = locales.slice(0, 16).map(createQuotaHistoryDateTimeFormatter)
    formats.forEach((format) => format(value))
    const countInstances = () => constructors.mock.calls.filter(([, options]) => options?.month === 'short').length
    expect(countInstances()).toBe(16)
    formats[0](value)
    createQuotaHistoryDateTimeFormatter(locales[16])(value)
    expect(countInstances()).toBe(17)
    formats[0](value)
    expect(countInstances()).toBe(17)
    expect(formats[1](value)).toBe(originalFormatDateTime(value, locales[1]))
    expect(countInstances()).toBe(18)
  })
})
