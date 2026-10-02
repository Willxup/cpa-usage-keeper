import type { PricingBasePrices, PricingContextCondition, PricingDaysCondition, PricingPeriodCondition, PricingPriceBranch } from '@/lib/types'

type PriceKey = keyof PricingBasePrices
const priceKeys: PriceKey[] = ['input', 'output', 'cache_read', 'cache_write']

export interface PricingBranchDraft {
  id: string
  name: string
  days: PricingDaysCondition
  context: { type: PricingContextCondition['type']; threshold: string; min: string; max: string }
  period: { type: PricingPeriodCondition['type']; start: string; end: string }
  prices: Record<PriceKey, string>
}

export interface PricingBranchValidation {
  branch: PricingPriceBranch | null
  errors: Record<string, string>
  conflictBranchIds: string[]
}

export interface PricingBranchConflict {
  branchIds: [string, string]
  indices: [number, number]
}

type TokenInterval = { min: number; max: number }
type MinuteInterval = { start: number; end: number }

// 编辑器显式传入原 ID 或复制后的新 ID；所有数值留作独立字符串草稿。
export function makePricingBranchDraft(
  branch: PricingPriceBranch | null,
  defaultPrices: Record<PriceKey, string>,
  id: string,
): PricingBranchDraft {
  const context = branch?.context
  const period = branch?.period
  return {
    id,
    name: branch?.name ?? '',
    days: branch?.days ?? 'all',
    context: {
      type: context?.type ?? 'all',
      threshold: context && 'threshold' in context ? String(context.threshold) : '',
      min: context && 'min' in context ? String(context.min) : '',
      max: context && 'max' in context ? String(context.max) : '',
    },
    period: {
      type: period?.type ?? 'all',
      start: period && 'start' in period ? period.start : '',
      end: period && 'end' in period ? period.end : '',
    },
    prices: branch ? {
      input: String(branch.prices.input), output: String(branch.prices.output),
      cache_read: String(branch.prices.cache_read), cache_write: String(branch.prices.cache_write),
    } : { ...defaultPrices },
  }
}

// 上下文阈值只接收非负安全整数，空白与非法输入分别标记。
function parseSafeToken(raw: string, path: string, errors: Record<string, string>): number | null {
  if (!raw.trim()) {
    errors[path] = 'required'
    return null
  }
  const value = Number(raw)
  if (!Number.isSafeInteger(value) || value < 0) {
    errors[path] = 'invalid'
    return null
  }
  return value
}

// 按当前条件类型输出唯一合法字段，并限制 gt 加一后的安全整数范围。
function parseContext(draft: PricingBranchDraft['context'], errors: Record<string, string>): PricingContextCondition | null {
  switch (draft.type) {
    case 'all': return { type: 'all' }
    case 'gt':
    case 'lte': {
      const threshold = parseSafeToken(draft.threshold, 'context.threshold', errors)
      if (threshold === null) return null
      if (draft.type === 'gt' && threshold === Number.MAX_SAFE_INTEGER) {
        errors['context.threshold'] = 'invalid'
        return null
      }
      return { type: draft.type, threshold }
    }
    case 'range': {
      const min = parseSafeToken(draft.min, 'context.min', errors)
      const max = parseSafeToken(draft.max, 'context.max', errors)
      if (min === null || max === null) return null
      if (min > max) {
        errors['context.min'] = 'invalid'
        return null
      }
      return { type: 'range', min, max }
    }
    default:
      errors['context.type'] = 'invalid'
      return null
  }
}

// 每日时段只接受严格 HH:mm，并转换为当天分钟位置。
const clockMinute = (value: string): number | null => {
  if (!/^(?:[01][0-9]|2[0-3]):[0-5][0-9]$/.test(value)) return null
  return Number(value.slice(0, 2)) * 60 + Number(value.slice(3, 5))
}

// 日期不限保留跨午夜窗口；工作日和周末窗口必须在同一天结束。
function parsePeriod(draft: PricingBranchDraft['period'], days: PricingDaysCondition, errors: Record<string, string>): PricingPeriodCondition | null {
  if (draft.type === 'all') return { type: 'all' }
  if (draft.type !== 'window') {
    errors['period.type'] = 'invalid'
    return null
  }
  const start = clockMinute(draft.start)
  const end = clockMinute(draft.end)
  if (start === null) errors['period.start'] = draft.start.trim() ? 'invalid' : 'required'
  if (end === null) errors['period.end'] = draft.end.trim() ? 'invalid' : 'required'
  if (start === null || end === null) return null
  if (start === end) {
    errors['period.end'] = 'invalid'
    return null
  }
  if (days !== 'all' && start > end) {
    errors['period.end'] = 'cross_day'
    return null
  }
  return { type: 'window', start: draft.start, end: draft.end }
}

// 分支四项单价逐项必填且有限非负，明确的零价保留为零。
function parsePrices(draft: PricingBranchDraft['prices'], errors: Record<string, string>): PricingBasePrices | null {
  const prices = {} as PricingBasePrices
  for (const key of priceKeys) {
    const raw = draft[key]
    const path = `prices.${key}`
    if (!raw.trim()) {
      errors[path] = 'required'
      continue
    }
    const value = Number(raw)
    if (!Number.isFinite(value) || value < 0) {
      errors[path] = 'invalid'
      continue
    }
    prices[key] = value
  }
  return priceKeys.some((key) => errors[`prices.${key}`]) ? null : prices
}

// 将上下文条件转为闭区间，gt 从阈值加一开始匹配。
function contextInterval(context: PricingContextCondition): TokenInterval {
  switch (context.type) {
    case 'gt': return { min: context.threshold + 1, max: Number.POSITIVE_INFINITY }
    case 'lte': return { min: 0, max: context.threshold }
    case 'range': return { min: context.min, max: context.max }
    default: return { min: 0, max: Number.POSITIVE_INFINITY }
  }
}

// 每日窗口按半开分钟段比较；跨午夜在 00:00 拆开，结束点本身不相交。
function minuteIntervals(period: PricingPeriodCondition): MinuteInterval[] {
  if (period.type === 'all') return [{ start: 0, end: 1440 }]
  const start = clockMinute(period.start)!
  const end = clockMinute(period.end)!
  if (start < end) return [{ start, end }]
  if (end === 0) return [{ start, end: 1440 }]
  return [{ start, end: 1440 }, { start: 0, end }]
}

// 日期、闭区间 Token 和半开每日时段三项均相交时，分支才冲突。
function branchesOverlap(
  left: Pick<PricingPriceBranch, 'days' | 'context' | 'period'>,
  right: Pick<PricingPriceBranch, 'days' | 'context' | 'period'>,
): boolean {
  if (left.days && right.days && left.days !== 'all' && right.days !== 'all' && left.days !== right.days) return false
  const a = contextInterval(left.context)
  const b = contextInterval(right.context)
  if (a.min > b.max || b.min > a.max) return false
  return minuteIntervals(left.period).some((first) => minuteIntervals(right.period).some((second) =>
    first.start < second.end && second.start < first.end))
}

// 主保存对已解析分支做日期、上下文和时段交集预检；默认基础价不在分支集合内。
export function findPricingBranchConflicts(branches: PricingPriceBranch[]): PricingBranchConflict[] {
  const conflicts: PricingBranchConflict[] = []
  for (let first = 0; first < branches.length; first += 1) {
    for (let second = first + 1; second < branches.length; second += 1) {
      if (branchesOverlap(branches[first], branches[second])) {
        conflicts.push({
          branchIds: [branches[first].id, branches[second].id], indices: [first, second],
        })
      }
    }
  }
  return conflicts
}

// 子编辑仅转换草稿、检查必填数值和其他草稿的条件交集；组合最坏费用仍由服务端判定。
export function validatePricingBranchDraft(
  draft: PricingBranchDraft,
  otherBranches: PricingBranchDraft[],
): PricingBranchValidation {
  const errors: Record<string, string> = {}
  const id = draft.id.trim()
  const name = draft.name.trim()
  if (!id) errors.id = 'required'
  if (!name) errors.name = 'required'
  if (id && otherBranches.some((other) => other.id.trim() === id)) errors.id = 'duplicate'
  const context = parseContext(draft.context, errors)
  if (!['all', 'weekday', 'weekend'].includes(draft.days)) errors.days = 'invalid'
  const period = parsePeriod(draft.period, draft.days, errors)
  const prices = parsePrices(draft.prices, errors)
  if (draft.days === 'all' && context?.type === 'all' && period?.type === 'all') {
    errors.context = 'default_conflict'
    errors.period = 'default_conflict'
  }
  const conflictBranchIds = new Set<string>()
  if (context && period) {
    for (const other of otherBranches) {
      const otherContext = parseContext(other.context, {})
      const otherPeriod = parsePeriod(other.period, other.days, {})
      if (!otherContext || !otherPeriod || !branchesOverlap({ days: draft.days, context, period }, { days: other.days, context: otherContext, period: otherPeriod })) continue
      conflictBranchIds.add(id)
      conflictBranchIds.add(other.id.trim())
      errors.context = 'conflict'
      errors.period = 'conflict'
    }
  }
  return {
    branch: Object.keys(errors).length ? null : { id, name, days: draft.days, context: context!, period: period!, prices: prices! },
    errors,
    conflictBranchIds: [...conflictBranchIds],
  }
}
