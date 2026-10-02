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

// buildPricingRecalculationHours 从后端合法 instant 边界按 step_seconds 生成小时选项，不按浏览器时区取整。
// 普通小时仅显示本地钟点；DST 同日重复小时才附实际偏移，value 始终包含部署时区的 RFC3339 偏移。
export function buildPricingRecalculationHours(options: PricingRecalculationOptions): PricingRecalculationHour[] {
  if (!options.earliest_start || !options.latest_start) return []
  const first = Date.parse(options.earliest_start)
  const last = Date.parse(options.latest_start)
  const step = options.step_seconds * 1000
  if (!Number.isFinite(first) || !Number.isFinite(last) || !Number.isSafeInteger(step) || step <= 0 || first > last) return []
  const formatter = new Intl.DateTimeFormat('en-GB', {
    timeZone: options.timezone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZoneName: 'longOffset',
  })
  const hours: FormattedHour[] = []
  const labelCounts = new Map<string, number>()
  for (let instant = first; instant <= last; instant += step) {
    const hour = formatPricingHour(instant, formatter)
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
