import type { CodexQuotaHistoryCycle } from '@/lib/types'

export type QuotaHistoryTimeFormatter = (value: string) => string

interface OffsetTime {
  clock: string
  offset: number
  instant: number
}

const offsetTimestamp = /^(\d{4}-\d{2}-\d{2})[T\s](\d{2}:\d{2}):\d{2}(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/i

function readOffsetTime(value: string): OffsetTime | null {
  if (typeof value !== 'string') return null
  const match = value.trim().match(offsetTimestamp)
  if (!match) return null
  const instant = Date.parse(value)
  if (!Number.isFinite(instant)) return null
  const zone = match[3]
  const offset = zone.toUpperCase() === 'Z' ? 0
    : (Number(zone.slice(1, 3)) * 60 + Number(zone.slice(4, 6))) * (zone[0] === '-' ? -1 : 1)
  return { clock: `${match[1]}T${match[2]}`, offset, instant }
}

function offsetSuffix(offset: number): string {
  const absolute = Math.abs(offset)
  const hours = String(Math.floor(absolute / 60)).padStart(2, '0')
  const minutes = String(absolute % 60).padStart(2, '0')
  return ` UTC${offset < 0 ? '-' : '+'}${hours}:${minutes}`
}

function collectQuotaHistoryTimeSuffixes(cycles: readonly CodexQuotaHistoryCycle[]): Set<string> {
  const times = new Map<string, OffsetTime | null>()
  const suffixes = new Set<string>()
  const collect = (value: string) => {
    if (!times.has(value)) times.set(value, readOffsetTime(value))
    return times.get(value)
  }
  const collectPair = (start: string, end: string) => {
    const left = collect(start)
    const right = collect(end)
    if (left && right && left.offset !== right.offset) {
      suffixes.add(start)
      suffixes.add(end)
    }
  }

  // 未选中周期和折叠明细也参与身份判断，避免展开、切周期或打开删除框后标签改变。
  for (const cycle of cycles) {
    collectPair(cycle.window_started_at, cycle.reset_at)
    collectPair(cycle.effective_started_at, cycle.effective_ended_at)
    collectPair(cycle.effective_started_at, cycle.status === 'current' ? cycle.reset_at : cycle.effective_ended_at)
    collectPair(cycle.first_observed_at, cycle.last_observed_at)
    for (const transition of cycle.transitions) collectPair(transition.interval_started_at, transition.interval_ended_at)
  }

  const clocks = new Map<string, { offsets: Set<number>; instants: Set<number> }>()
  for (const time of times.values()) {
    if (!time) continue
    let clock = clocks.get(time.clock)
    if (!clock) {
      clock = { offsets: new Set(), instants: new Set() }
      clocks.set(time.clock, clock)
    }
    clock.offsets.add(time.offset)
    clock.instants.add(time.instant)
  }
  for (const [value, time] of times) {
    if (!time) continue
    const clock = clocks.get(time.clock)!
    if (clock.offsets.size > 1 && clock.instants.size > 1) suffixes.add(value)
  }
  return suffixes
}

// 收集函数的临时身份表可立即释放，显示回调仅保留后缀集合和原 formatter。
export function createQuotaHistoryTimeFormatter(
  cycles: readonly CodexQuotaHistoryCycle[],
  format: QuotaHistoryTimeFormatter,
): QuotaHistoryTimeFormatter {
  const suffixes = collectQuotaHistoryTimeSuffixes(cycles)
  return (value) => {
    const label = format(value)
    if (!suffixes.has(value)) return label
    const time = readOffsetTime(value)
    return time ? label + offsetSuffix(time.offset) : label
  }
}
