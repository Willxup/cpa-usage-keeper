import { useMemo, useState } from 'react'
import { formatNumber } from '../../lib/usage'
import type { PricingEntry } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface PricingCardProps {
  usedModels: string[]
  pricing: PricingEntry[]
  saving: boolean
  error: string
  onSave: (model: string, payload: Omit<PricingEntry, 'model'>) => Promise<void>
}

export function PricingCard({ usedModels, pricing, saving, error, onSave }: PricingCardProps) {
  const [selectedModel, setSelectedModel] = useState('')
  const [promptPrice, setPromptPrice] = useState('')
  const [completionPrice, setCompletionPrice] = useState('')
  const [cachePrice, setCachePrice] = useState('')

  const pricingMap = useMemo(() => new Map(pricing.map((entry) => [entry.model, entry])), [pricing])
  const sortedPricing = useMemo(() => [...pricing].sort((left, right) => left.model.localeCompare(right.model)), [pricing])

  function handleModelChange(model: string) {
    setSelectedModel(model)
    const existing = pricingMap.get(model)
    setPromptPrice(existing ? String(existing.prompt_price_per_1m) : '')
    setCompletionPrice(existing ? String(existing.completion_price_per_1m) : '')
    setCachePrice(existing ? String(existing.cache_price_per_1m) : '')
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedModel) return

    await onSave(selectedModel, {
      prompt_price_per_1m: Number(promptPrice) || 0,
      completion_price_per_1m: Number(completionPrice) || 0,
      cache_price_per_1m: Number(cachePrice) || 0,
    })
  }

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>Price settings</h2>
          <p className={styles.panelSubtitle}>Persist model pricing in the backend so cost analytics can be calculated.</p>
        </div>
        <span className={styles.rowsCount}>{usedModels.length} used models</span>
      </div>

      <form className={styles.pricingForm} onSubmit={handleSubmit}>
        <label className={styles.pricingField}>
          <span>Model</span>
          <select value={selectedModel} onChange={(event) => handleModelChange(event.target.value)}>
            <option value="">Select a used model</option>
            {usedModels.map((model) => (
              <option key={model} value={model}>
                {model}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.pricingField}>
          <span>Prompt / 1M</span>
          <input type="number" min="0" step="0.0001" value={promptPrice} onChange={(event) => setPromptPrice(event.target.value)} />
        </label>
        <label className={styles.pricingField}>
          <span>Completion / 1M</span>
          <input type="number" min="0" step="0.0001" value={completionPrice} onChange={(event) => setCompletionPrice(event.target.value)} />
        </label>
        <label className={styles.pricingField}>
          <span>Cache / 1M</span>
          <input type="number" min="0" step="0.0001" value={cachePrice} onChange={(event) => setCachePrice(event.target.value)} />
        </label>
        <button type="submit" className={styles.secondaryButton} disabled={saving || !selectedModel}>
          {saving ? 'Saving...' : 'Save pricing'}
        </button>
      </form>

      {error ? <p className={styles.inlineError}>{error}</p> : null}

      <div className={styles.tableScroller}>
        <div className={styles.table}>
          <div className={styles.tableHeaderCompact}>
            <span>Model</span>
            <span>Prompt / 1M</span>
            <span>Completion / 1M</span>
            <span>Cache / 1M</span>
            <span>Seen in usage</span>
          </div>
          {sortedPricing.length > 0 ? (
            sortedPricing.map((entry) => (
              <div key={entry.model} className={styles.tableRowCompact}>
                <span>{entry.model}</span>
                <span>${formatNumber(entry.prompt_price_per_1m)}</span>
                <span>${formatNumber(entry.completion_price_per_1m)}</span>
                <span>${formatNumber(entry.cache_price_per_1m)}</span>
                <span>{usedModels.includes(entry.model) ? 'Used' : 'Unknown'}</span>
              </div>
            ))
          ) : (
            <div className={styles.emptyInline}>No pricing saved yet.</div>
          )}
        </div>
      </div>
    </section>
  )
}
