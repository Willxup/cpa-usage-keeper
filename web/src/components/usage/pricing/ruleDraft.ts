import type { PricingRule, ReplacePricingRuleInput } from '@/lib/types'

export interface PricingRuleDraft {
  id: string
  key: string
  value: string
  multiplier: string
}

export interface PricingRuleDraftError {
  key?: 'required' | 'duplicate'
  value?: 'required'
  multiplier?: 'invalid'
}

export interface PricingRuleSubmission {
  rules: ReplacePricingRuleInput[] | null
  errors: Record<string, PricingRuleDraftError>
}

let nextDraftID = 0

export const createPricingRuleDraft = (rule?: PricingRule): PricingRuleDraft => ({
  id: `pricing-rule-${++nextDraftID}`,
  key: rule?.key ?? '',
  value: rule?.value ?? '',
  multiplier: rule ? String(rule.multiplier) : '',
})

export const pricingRulesToDrafts = (rules: PricingRule[]): PricingRuleDraft[] => (
  rules.length > 0 ? rules.map(createPricingRuleDraft) : [createPricingRuleDraft()]
)

export const reconcilePricingRuleDuplicateErrors = (
  drafts: PricingRuleDraft[],
  currentErrors: Record<string, PricingRuleDraftError>,
): Record<string, PricingRuleDraftError> => {
  const errors: Record<string, PricingRuleDraftError> = {}
  for (const [id, rowError] of Object.entries(currentErrors)) {
    const nextRowError = { ...rowError }
    if (nextRowError.key === 'duplicate') delete nextRowError.key
    if (Object.keys(nextRowError).length > 0) errors[id] = nextRowError
  }

  const seen = new Set<string>()
  for (const draft of drafts) {
    const key = draft.key.trim().toLowerCase()
    const value = draft.value.trim()
    if (!key || !value) continue
    const identity = `${key}\u0000${value}`
    if (seen.has(identity)) {
      errors[draft.id] = { ...errors[draft.id], key: 'duplicate' }
    } else {
      seen.add(identity)
    }
  }
  return errors
}

export const buildPricingRuleSubmission = (drafts: PricingRuleDraft[]): PricingRuleSubmission => {
  const rules: ReplacePricingRuleInput[] = []
  const errors: Record<string, PricingRuleDraftError> = {}
  const seen = new Set<string>()

  for (const draft of drafts) {
    const key = draft.key.trim().toLowerCase()
    const value = draft.value.trim()
    const multiplierText = draft.multiplier.trim()
    if (!key && !value && !multiplierText) continue

    const rowError: PricingRuleDraftError = {}
    if (!key) rowError.key = 'required'
    if (!value) rowError.value = 'required'

    let multiplier: number | undefined
    if (multiplierText) {
      multiplier = Number(multiplierText)
      if (!Number.isFinite(multiplier) || multiplier < 0) {
        rowError.multiplier = 'invalid'
      }
    }

    if (key && value) {
      const identity = `${key}\u0000${value}`
      if (seen.has(identity)) {
        rowError.key = 'duplicate'
      } else {
        seen.add(identity)
      }
    }

    if (Object.keys(rowError).length > 0) {
      errors[draft.id] = rowError
      continue
    }

    rules.push({
      key,
      value,
      ...(multiplier === undefined ? {} : { multiplier }),
    })
  }

  return {
    rules: Object.keys(errors).length > 0 ? null : rules,
    errors,
  }
}
