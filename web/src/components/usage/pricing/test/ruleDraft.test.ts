import { describe, expect, it } from 'vitest'
import { buildPricingRuleSubmission, type PricingRuleDraft } from '../ruleDraft'

const draft = (
  id: string,
  key = '',
  value = '',
  multiplier = '',
): PricingRuleDraft => ({ id, key, value, multiplier })

describe('buildPricingRuleSubmission', () => {
  it('normalizes keys and values while omitting a blank default multiplier', () => {
    expect(buildPricingRuleSubmission([
      draft('first', ' Service_Tier ', ' priority ', ''),
      draft('second', 'reasoning_effort', 'xhigh', '0'),
    ])).toEqual({
      rules: [
        { key: 'service_tier', value: 'priority' },
        { key: 'reasoning_effort', value: 'xhigh', multiplier: 0 },
      ],
      errors: {},
    })
  })

  it('ignores a completely empty placeholder row', () => {
    expect(buildPricingRuleSubmission([draft('placeholder')])).toEqual({ rules: [], errors: {} })
  })

  it('reports partially filled and invalid rows without producing a submission', () => {
    expect(buildPricingRuleSubmission([
      draft('missing-value', 'service_tier'),
      draft('missing-key', '', 'priority'),
      draft('negative', 'reasoning_effort', 'xhigh', '-1'),
      draft('infinite', 'reasoning_effort', 'high', '1e309'),
    ])).toEqual({
      rules: null,
      errors: {
        'missing-value': { value: 'required' },
        'missing-key': { key: 'required' },
        negative: { multiplier: 'invalid' },
        infinite: { multiplier: 'invalid' },
      },
    })
  })

  it('detects duplicates after key and value normalization without folding value case', () => {
    const duplicate = buildPricingRuleSubmission([
      draft('first', 'Service_Tier', ' priority '),
      draft('second', 'service_tier', 'priority', '2'),
    ])
    expect(duplicate.rules).toBeNull()
    expect(duplicate.errors.second).toEqual({ key: 'duplicate' })

    expect(buildPricingRuleSubmission([
      draft('lower', 'service_tier', 'priority'),
      draft('upper', 'service_tier', 'PRIORITY'),
    ]).rules).toEqual([
      { key: 'service_tier', value: 'priority' },
      { key: 'service_tier', value: 'PRIORITY' },
    ])
  })
})
