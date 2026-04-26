import { formatNumber } from '../../lib/usage'
import styles from '../../pages/usage/UsagePage.module.css'

interface TokenBreakdownCardProps {
  inputTokens: number
  outputTokens: number
  reasoningTokens: number
  cachedTokens: number
}

export function TokenBreakdownCard({
  inputTokens,
  outputTokens,
  reasoningTokens,
  cachedTokens,
}: TokenBreakdownCardProps) {
  const items = [
    { key: 'input', label: 'Input tokens', value: inputTokens, colorClass: styles.tokenInput },
    { key: 'output', label: 'Output tokens', value: outputTokens, colorClass: styles.tokenOutput },
    { key: 'reasoning', label: 'Reasoning tokens', value: reasoningTokens, colorClass: styles.tokenReasoning },
    { key: 'cached', label: 'Cached tokens', value: cachedTokens, colorClass: styles.tokenCached },
  ]

  const total = items.reduce((sum, item) => sum + item.value, 0)

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>Token breakdown</h2>
          <p className={styles.panelSubtitle}>Split of persisted tokens across all request events.</p>
        </div>
        <span>{formatNumber(total)} total</span>
      </div>
      <div className={styles.breakdownList}>
        {items.map((item) => {
          const percentage = total > 0 ? (item.value / total) * 100 : 0
          return (
            <div key={item.key} className={styles.breakdownRow}>
              <div className={styles.breakdownLabelRow}>
                <div className={styles.breakdownLabelWrap}>
                  <span className={`${styles.breakdownDot} ${item.colorClass}`} />
                  <span>{item.label}</span>
                </div>
                <strong>{formatNumber(item.value)}</strong>
              </div>
              <div className={styles.breakdownBarTrack}>
                <div className={`${styles.breakdownBarFill} ${item.colorClass}`} style={{ width: `${percentage}%` }} />
              </div>
              <span className={styles.breakdownPercent}>{percentage.toFixed(1)}%</span>
            </div>
          )
        })}
      </div>
    </section>
  )
}
