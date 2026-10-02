import { describe, expect, it } from 'vitest'
import type { PricingBasePrices, PricingPriceBranch } from '@/lib/types'
import {
  findPricingBranchConflicts,
  makePricingBranchDraft,
  validatePricingBranchDraft,
  type PricingBranchDraft,
} from '../pricingBranchDraft'

const defaultPrices: Record<keyof PricingBasePrices, string> = {
  input: '0', output: '2.5', cache_read: '0.25', cache_write: '1',
}

const makeDraft = (id: string): PricingBranchDraft => ({
  ...makePricingBranchDraft(null, defaultPrices, id), name: id,
  context: { type: 'gt', threshold: '0', min: '', max: '' },
})

const validBranch = (draft: PricingBranchDraft, others: PricingBranchDraft[] = []): PricingPriceBranch => {
  const result = validatePricingBranchDraft(draft, others)
  expect(result.errors).toEqual({})
  expect(result.branch).not.toBeNull()
  return result.branch!
}

describe('pricingBranchDraft', () => {
  it('rejects a condition that replaces the default price for every request', () => {
    const draft = makeDraft('all');
    draft.context.type = 'all'; draft.period.type = 'all';
    const result = validatePricingBranchDraft(draft, []);
    expect(result.branch).toBeNull();
    expect(result.errors).toMatchObject({ context: 'default_conflict', period: 'default_conflict' });
  });
  it('creates an independent draft and preserves the stable saved ID and all four prices', () => {
    const saved: PricingPriceBranch = {
      id: 'saved', name: '夜间', context: { type: 'range', min: 0, max: 20 },
      days: 'weekday',
      period: { type: 'window', start: '20:00', end: '08:00' },
      prices: { input: 0, output: 3, cache_read: 0.5, cache_write: 1.25 },
    }
    const draft = makePricingBranchDraft(saved, defaultPrices, saved.id)
    expect(draft).toEqual({
      id: 'saved', name: '夜间',
      days: 'weekday',
      context: { type: 'range', threshold: '', min: '0', max: '20' },
      period: { type: 'window', start: '20:00', end: '08:00' },
      prices: { input: '0', output: '3', cache_read: '0.5', cache_write: '1.25' },
    })
    draft.prices.input = '9'
    expect(saved.prices.input).toBe(0)
    expect(makePricingBranchDraft(null, defaultPrices, 'new').prices).toEqual(defaultPrices)
    expect(makePricingBranchDraft(saved, defaultPrices, 'copy').id).toBe('copy')
    expect(makePricingBranchDraft(saved, defaultPrices, 'copy').days).toBe('weekday')
    expect(makePricingBranchDraft(null, defaultPrices, 'new').days).toBe('all')
  })

  it('allows date-only branches, keeps weekday and weekend disjoint, and rejects a cross-midnight date window', () => {
    const weekday = makeDraft('weekday')
    weekday.days = 'weekday'; weekday.context.type = 'all'; weekday.period.type = 'all'
    const weekend = makeDraft('weekend')
    weekend.days = 'weekend'; weekend.context.type = 'all'; weekend.period.type = 'all'
    expect(findPricingBranchConflicts([validBranch(weekday), validBranch(weekend)])).toEqual([])
    expect(validBranch(weekday).days).toBe('weekday')
    const all = makeDraft('all')
    expect(findPricingBranchConflicts([validBranch(weekday), validBranch(all)])).toHaveLength(1)
    weekday.period = { type: 'window', start: '20:00', end: '08:00' }
    expect(validatePricingBranchDraft(weekday, []).errors).toEqual({ 'period.end': 'cross_day' })
    weekday.period.end = '20:01'
    expect(validBranch(weekday).period).toEqual({ type: 'window', start: '20:00', end: '20:01' })
  })

  it('accepts safe closed context bounds and rejects gt plus one outside safe integers', () => {
    const lte = makeDraft('lte')
    lte.context = { type: 'lte', threshold: '99', min: '', max: '' }
    const gt = makeDraft('gt')
    gt.context = { type: 'gt', threshold: '99', min: '', max: '' }
    expect(findPricingBranchConflicts([validBranch(lte), validBranch(gt)])).toEqual([])

    const range = makeDraft('range')
    range.context = { type: 'range', threshold: '', min: '100', max: '100' }
    expect(findPricingBranchConflicts([validBranch(gt), validBranch(range)]))
      .toEqual([{ branchIds: ['gt', 'range'], indices: [0, 1] }])
    gt.context.threshold = '9007199254740990'
    expect(validBranch(gt).context).toEqual({ type: 'gt', threshold: 9007199254740990 })
    lte.context.threshold = '9007199254740990'
    expect(findPricingBranchConflicts([validBranch(lte), validBranch(gt)])).toEqual([])
    gt.context.threshold = '9007199254740991'
    expect(validatePricingBranchDraft(gt, []).errors).toEqual({ 'context.threshold': 'invalid' })
    range.context = { type: 'range', threshold: '', min: '20', max: '10' }
    expect(validatePricingBranchDraft(range, []).errors).toEqual({ 'context.min': 'invalid' })
    range.context.min = '-1'
    expect(validatePricingBranchDraft(range, []).errors['context.min']).toBe('invalid')
    range.context = { type: 'range', threshold: '', min: '0', max: '9007199254740992' }
    expect(validatePricingBranchDraft(range, []).errors['context.max']).toBe('invalid')
  })

  it('splits cross-midnight windows and treats the end minute as excluded', () => {
    const night = makeDraft('night')
    night.period = { type: 'window', start: '20:00', end: '08:00' }
    const morning = makeDraft('morning')
    morning.period = { type: 'window', start: '08:00', end: '10:00' }
    expect(findPricingBranchConflicts([validBranch(night), validBranch(morning)])).toEqual([])
    morning.period.start = '07:59'
    expect(findPricingBranchConflicts([validBranch(night), validBranch(morning)]))
      .toEqual([{ branchIds: ['night', 'morning'], indices: [0, 1] }])

    const untilMidnight = makeDraft('until-midnight')
    untilMidnight.period = { type: 'window', start: '22:00', end: '00:00' }
    const afterMidnight = makeDraft('after-midnight')
    afterMidnight.period = { type: 'window', start: '00:00', end: '01:00' }
    expect(findPricingBranchConflicts([validBranch(untilMidnight), validBranch(afterMidnight)])).toEqual([])
    morning.period = { type: 'window', start: '08:00', end: '08:00' }
    expect(validatePricingBranchDraft(morning, []).errors['period.end']).toBe('invalid')
    morning.period.end = '24:00'
    expect(validatePricingBranchDraft(morning, []).errors['period.end']).toBe('invalid')
  })

  it('reports a conflict only when both context and daily period intersect', () => {
    const first = makeDraft('first')
    first.context = { type: 'range', threshold: '', min: '10', max: '20' }
    first.period = { type: 'window', start: '22:00', end: '02:00' }
    const second = makeDraft('second')
    second.context = { type: 'gt', threshold: '19', min: '', max: '' }
    second.period = { type: 'window', start: '01:00', end: '03:00' }
    expect(validatePricingBranchDraft(second, [first])).toMatchObject({
      branch: null, errors: { context: 'conflict', period: 'conflict' },
      conflictBranchIds: ['second', 'first'],
    })
    second.period = { type: 'window', start: '02:00', end: '03:00' }
    expect(validatePricingBranchDraft(second, [first]).errors).toEqual({})
    second.period = { type: 'all', start: '', end: '' }
    second.context.threshold = '20'
    expect(validatePricingBranchDraft(second, [first]).errors).toEqual({})
  })

  it('requires four finite non-negative prices and keeps zero without imposing a maximum fee formula', () => {
    const draft = makeDraft('free')
    draft.prices = { input: '0', output: '0', cache_read: '0', cache_write: '0' }
    expect(validBranch(draft).prices).toEqual({ input: 0, output: 0, cache_read: 0, cache_write: 0 })
    draft.prices.input = ''
    draft.prices.output = '-1'
    draft.prices.cache_read = 'Infinity'
    draft.prices.cache_write = 'NaN'
    expect(validatePricingBranchDraft(draft, []).errors).toMatchObject({
      'prices.input': 'required', 'prices.output': 'invalid',
      'prices.cache_read': 'invalid', 'prices.cache_write': 'invalid',
    })
    draft.prices = { input: '1e300', output: '0', cache_read: '0', cache_write: '0' }
    expect(validBranch(draft).prices.input).toBe(1e300)
  })

  it('requires a unique ID and name but does not restrict condition values in this helper', () => {
    const first = makeDraft('same')
    const second = makeDraft('same')
    second.name = ' '
    expect(validatePricingBranchDraft(second, [first]).errors).toMatchObject({ id: 'duplicate', name: 'required' })
    second.id = 'different'
    second.name = ' another '
    expect(validBranch(second).name).toBe('another')
    const empty = makeDraft('')
    expect(validatePricingBranchDraft(empty, []).errors.id).toBe('required')
  })

  it('allows restricted prices with a default fallback but rejects overlapping branches', () => {
    const catchAll = makeDraft('catch-all')
    expect(findPricingBranchConflicts([validBranch(catchAll)])).toEqual([])
    const another = makeDraft('another')
    expect(findPricingBranchConflicts([validBranch(catchAll), validBranch(another)])).toEqual([
      { branchIds: ['catch-all', 'another'], indices: [0, 1] },
    ])
  })
})
