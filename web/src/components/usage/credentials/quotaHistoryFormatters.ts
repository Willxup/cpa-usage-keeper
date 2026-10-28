import { formatCompactNumber } from '@/utils/usage'
import type { QuotaHistoryTimeFormatter } from './quotaHistoryTimeLabels'

const dateTimeFormatters = new Map<string, Intl.DateTimeFormat>()
const dateTimeFormatterLimit = 16
const dateTimeOptions: Intl.DateTimeFormatOptions = {
  month: 'short',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
}

function getDateTimeFormatter(key: string, locale: string, timeZone: string): Intl.DateTimeFormat {
  let formatter = dateTimeFormatters.get(key)
  if (formatter) {
    dateTimeFormatters.delete(key)
  } else {
    formatter = new Intl.DateTimeFormat(locale, { ...dateTimeOptions, timeZone })
    if (dateTimeFormatters.size >= dateTimeFormatterLimit) {
      dateTimeFormatters.delete(dateTimeFormatters.keys().next().value!)
    }
  }
  dateTimeFormatters.set(key, formatter)
  return formatter
}

export function createQuotaHistoryDateTimeFormatter(locale?: string): QuotaHistoryTimeFormatter {
  // 每次建立显示上下文时解析一次默认 locale/TZ，逐个时间标签只查有界实例缓存。
  const resolved = new Intl.DateTimeFormat(locale).resolvedOptions()
  const utcKey = JSON.stringify([resolved.locale, 'utc', 'UTC'])
  const localKey = JSON.stringify([resolved.locale, 'local', resolved.timeZone])
  return (value) => {
    const date = new Date(value)
    if (!Number.isFinite(date.getTime())) return value || '—'
    // 沿用原 API offset 墙上时间到 UTC 的映射，不让浏览器再次换区。
    const offsetTime = value.trim().match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i)
    const displayDate = offsetTime
      ? new Date(Date.UTC(
        Number(offsetTime[1]),
        Number(offsetTime[2]) - 1,
        Number(offsetTime[3]),
        Number(offsetTime[4]),
        Number(offsetTime[5]),
        Number(offsetTime[6]),
      ))
      : date
    return getDateTimeFormatter(
      offsetTime ? utcKey : localKey,
      resolved.locale,
      offsetTime ? 'UTC' : resolved.timeZone,
    ).format(displayDate)
  }
}

const summaryNumberFormatters: Partial<Record<0 | 1 | 2, Intl.NumberFormat>> = {}

function summaryNumberFormatter(maximumFractionDigits: 0 | 1 | 2): Intl.NumberFormat {
  let formatter = summaryNumberFormatters[maximumFractionDigits]
  if (!formatter) {
    // 数字一直使用浏览器默认 locale，与应用语言独立。
    formatter = new Intl.NumberFormat(undefined, { maximumFractionDigits })
    summaryNumberFormatters[maximumFractionDigits] = formatter
  }
  return formatter
}

export function formatQuotaSummaryRequests(value: number): string {
  if (Math.abs(value) >= 1_000) return formatCompactNumber(value)
  return summaryNumberFormatter(Math.abs(value) >= 100 ? 0 : Math.abs(value) >= 10 ? 1 : 2).format(value)
}

export function formatQuotaSummaryPercentage(value: number): string {
  return `${summaryNumberFormatter(2).format(value)}%`
}
