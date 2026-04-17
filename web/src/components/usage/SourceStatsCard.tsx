import { formatNumber } from '../../lib/usage'
import styles from '../../pages/usage/UsagePage.module.css'

interface SourceStat {
  source: string
  requests: number
  tokens: number
}

interface SourceStatsCardProps {
  items: SourceStat[]
}

export function SourceStatsCard({ items }: SourceStatsCardProps) {
  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>Source stats</h2>
          <p className={styles.panelSubtitle}>Credential-like source activity summary for the selected range.</p>
        </div>
        <span>{items.length} sources</span>
      </div>
      <div className={styles.tableScroller}>
        <div className={styles.table}>
          <div className={styles.tableHeaderCompact}>
            <span>Source</span>
            <span>Requests</span>
            <span>Tokens</span>
            <span>Share</span>
            <span>Status</span>
          </div>
          {items.map((item, index) => (
            <div key={item.source} className={styles.tableRowCompact}>
              <span>{item.source}</span>
              <span>{formatNumber(item.requests)}</span>
              <span>{formatNumber(item.tokens)}</span>
              <span>#{index + 1}</span>
              <span>{item.requests > 0 ? 'Active' : 'Idle'}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
