import type { PricingRecalculationOptions } from '@/lib/types'

export interface PricingRecalculationHour {
  value: string
  date: string
  hourLabel: string
}

interface FormattedHour {
  value: string
  date: string
  clock: string
  offset: string
}

// formatPricingHour 按服务端部署 IANA 时区还原一个绝对小时，保留该 instant 的实际 UTC 偏移。
function formatPricingHour(instant: number, formatter: Intl.DateTimeFormat): FormattedHour {
  const parts = formatter.formatToParts(new Date(instant))
  const valueOf = (type: Intl.DateTimeFormatPartTypes): string => parts.find((part) => part.type === type)?.value ?? ''
  const year = valueOf('year')
  const month = valueOf('month')
  const day = valueOf('day')
  const clock = `${valueOf('hour')}:${valueOf('minute')}`
  const zone = valueOf('timeZoneName')
  const match = /^(?:GMT|UTC)(?:([+-])(\d{1,2})(?::(\d{2}))?)?$/.exec(zone)
  if (!match) throw new Error(`Unsupported timezone offset: ${zone}`)
  const offset = match[1] ? `${match[1]}${match[2].padStart(2, '0')}:${match[3] ?? '00'}` : '+00:00'
  const date = `${year}-${month}-${day}`
  return { value: `${date}T${clock}:00${offset}`, date, clock, offset }
}

function pricingFormatter(timezone: string): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat('en-GB', {
    timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZoneName: 'longOffset',
  })
}

export function pricingRecalculationDate(value: string | null, timezone: string): string {
  if (!value) return ''
  const instant = Date.parse(value)
  return Number.isFinite(instant) ? formatPricingHour(instant, pricingFormatter(timezone)).date : ''
}

// Generate only the selected local date, anchored to the server's absolute-hour grid.
// The bounded UTC window covers timezone offsets and DST transitions without scanning history.
export function buildPricingRecalculationHours(options: PricingRecalculationOptions, date: string): PricingRecalculationHour[] {
  if (!options.earliest_start || !options.latest_start) return []
  const first = Date.parse(options.earliest_start)
  const last = Date.parse(options.latest_start)
  const step = options.step_seconds * 1000
  const midnight = Date.parse(`${date}T00:00:00Z`)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(midnight) || new Date(midnight).toISOString().slice(0, 10) !== date ||
      !Number.isFinite(first) || !Number.isFinite(last) || step !== 3600000 || first > last) return []
  const formatter = pricingFormatter(options.timezone)
  const lower = Math.max(first, midnight - 48 * 3600000)
  const upper = Math.min(last, midnight + 72 * 3600000)
  const firstCandidate = first + Math.ceil((lower - first) / step) * step
  const hours: FormattedHour[] = []
  const labelCounts = new Map<string, number>()
  for (let instant = firstCandidate; instant <= upper; instant += step) {
    const hour = formatPricingHour(instant, formatter)
    if (hour.date !== date) continue
    hours.push(hour)
    const key = `${hour.date} ${hour.clock}`
    labelCounts.set(key, (labelCounts.get(key) ?? 0) + 1)
  }
  return hours.map((hour) => ({
    value: hour.value,
    date: hour.date,
    hourLabel: (labelCounts.get(`${hour.date} ${hour.clock}`) ?? 0) > 1
      ? `${hour.clock} UTC${hour.offset}` : hour.clock,
  }))
}
