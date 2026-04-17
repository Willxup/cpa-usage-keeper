import { useMemo, useState } from 'react'
import { formatNumber } from '../../lib/usage'
import type { ModelSummaryItem } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface ModelDetailsCardProps {
  items: ModelSummaryItem[]
}

type SortKey = 'modelName' | 'totalRequests' | 'totalTokens' | 'averageLatencyMs' | 'successRate' | 'totalCost'
type SortDirection = 'asc' | 'desc'

export function ModelDetailsCard({ items }: ModelDetailsCardProps) {
  const [sortKey, setSortKey] = useState<SortKey>('totalRequests')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')

  const sortedItems = useMemo(() => {
    const direction = sortDirection === 'asc' ? 1 : -1
    return [...items].sort((left, right) => {
      if (sortKey === 'modelName') {
        return direction * left.modelName.localeCompare(right.modelName)
      }
      return direction * (left[sortKey] - right[sortKey])
    })
  }, [items, sortDirection, sortKey])

  function handleSort(key: SortKey) {
    if (sortKey === key) {
      setSortDirection((current) => (current === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortKey(key)
    setSortDirection(key === 'modelName' ? 'asc' : 'desc')
  }

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>Model stats</h2>
          <p className={styles.panelSubtitle}>Sortable model-level requests, latency, success rate, and cost.</p>
        </div>
        <span>{items.length} rows</span>
      </div>
      <div className={styles.tableScroller}>
        <div className={styles.table}>
          <div className={styles.modelHeaderRow}>
            <button type="button" className={styles.sortHeaderButton} onClick={() => handleSort('modelName')}>Model</button>
            <button type="button" className={styles.sortHeaderButton} onClick={() => handleSort('totalRequests')}>Requests</button>
            <button type="button" className={styles.sortHeaderButton} onClick={() => handleSort('totalTokens')}>Tokens</button>
            <button type="button" className={styles.sortHeaderButton} onClick={() => handleSort('averageLatencyMs')}>Avg latency</button>
            <button type="button" className={styles.sortHeaderButton} onClick={() => handleSort('successRate')}>Success rate</button>
            <button type="button" className={styles.sortHeaderButton} onClick={() => handleSort('totalCost')}>Cost</button>
          </div>
          {sortedItems.map((item) => (
            <div key={`${item.apiName}-${item.modelName}`} className={styles.modelDataRow}>
              <span>
                <strong>{item.modelName}</strong>
                <small className={styles.subtleBlock}>{item.apiName}</small>
              </span>
              <span>{formatNumber(item.totalRequests)}</span>
              <span>{formatNumber(item.totalTokens)}</span>
              <span>{formatNumber(item.averageLatencyMs)} ms</span>
              <span className={item.successRate >= 95 ? styles.statusSuccess : item.successRate >= 80 ? styles.statusNeutral : styles.statusFailed}>
                {item.successRate.toFixed(1)}%
              </span>
              <span>${formatNumber(item.totalCost)}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
