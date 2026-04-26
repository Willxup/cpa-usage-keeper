import { useMemo, type CSSProperties } from 'react'
import type { SummaryCardValue } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface SummaryCardProps {
  card: SummaryCardValue
}

const ICONS: Record<string, string> = {
  requests: '◉',
  tokens: '◆',
  rpm: '◌',
  tpm: '↗',
  cost: '$',
}

export function SummaryCard({ card }: SummaryCardProps) {
  const accentStyle = useMemo(
    () => ({
      '--summary-accent': card.accent,
    }) as CSSProperties,
    [card.accent],
  )

  return (
    <article className={styles.summaryCard} style={accentStyle}>
      <div className={styles.summaryAccent} />
      <div className={styles.summaryHeader}>
        <p className={styles.summaryLabel}>{card.label}</p>
        <span className={styles.summaryIconBadge} aria-hidden="true">
          {ICONS[card.key] ?? '•'}
        </span>
      </div>
      <p className={styles.summaryValue}>{card.value}</p>
      {card.hint ? (
        <div className={styles.summaryMetaRow}>
          <span className={styles.summaryMetaItem}>{card.hint}</span>
        </div>
      ) : null}
      {card.hint ? <p className={styles.summaryHint}>{card.hint}</p> : null}
    </article>
  )
}
