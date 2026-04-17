import { useMemo, useState } from 'react'
import { formatNumber } from '../../lib/usage'
import type { ApiSummaryItem } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface ApiOverviewCardProps {
  items: ApiSummaryItem[]
}

type SortKey = 'apiName' | 'totalRequests' | 'totalTokens' | 'totalCost'
type SortDirection = 'asc' | 'desc'

export function ApiOverviewCard({ items }: ApiOverviewCardProps) {
  const [expandedApis, setExpandedApis] = useState<string[]>([])
  const [sortKey, setSortKey] = useState<SortKey>('totalRequests')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')

  const sortedItems = useMemo(() => {
    const direction = sortDirection === 'asc' ? 1 : -1
    return [...items].sort((left, right) => {
      if (sortKey === 'apiName') {
        return direction * left.apiName.localeCompare(right.apiName)
      }
      return direction * (left[sortKey] - right[sortKey])
    })
  }, [items, sortDirection, sortKey])

  function toggleExpand(apiName: string) {
    setExpandedApis((current) =>
      current.includes(apiName) ? current.filter((item) => item !== apiName) : [...current, apiName],
    )
  }

  function handleSort(key: SortKey) {
    if (sortKey === key) {
      setSortDirection((current) => (current === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortKey(key)
    setSortDirection(key === 'apiName' ? 'asc' : 'desc')
  }

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>API details</h2>
          <p className={styles.panelSubtitle}>Requests, tokens, and estimated cost grouped by API provider.</p>
        </div>
        <span>{items.length} APIs</span>
      </div>

      <div className={styles.sortBar}>
        <button type="button" className={styles.sortButton} onClick={() => handleSort('apiName')}>API</button>
        <button type="button" className={styles.sortButton} onClick={() => handleSort('totalRequests')}>Requests</button>
        <button type="button" className={styles.sortButton} onClick={() => handleSort('totalTokens')}>Tokens</button>
        <button type="button" className={styles.sortButton} onClick={() => handleSort('totalCost')}>Cost</button>
      </div>

      <div className={styles.expandList}>
        {sortedItems.map((item) => {
          const expanded = expandedApis.includes(item.apiName)
          return (
            <div key={item.apiName} className={styles.expandItem}>
              <button type="button" className={styles.expandHeader} onClick={() => toggleExpand(item.apiName)}>
                <div>
                  <strong>{item.apiName}</strong>
                  <div className={styles.expandMeta}>
                    <span>{formatNumber(item.totalRequests)} requests</span>
                    <span>{formatNumber(item.totalTokens)} tokens</span>
                    <span>${formatNumber(item.totalCost)}</span>
                  </div>
                </div>
                <span>{expanded ? '▼' : '▶'}</span>
              </button>
              {expanded ? (
                <div className={styles.expandBody}>
                  <div className={styles.tableHeaderCompact}>
                    <span>Model</span>
                    <span>Requests</span>
                    <span>Success / fail</span>
                    <span>Tokens</span>
                    <span>Cost</span>
                  </div>
                  {item.models.map((model) => (
                    <div key={`${item.apiName}-${model.modelName}`} className={styles.tableRowCompact}>
                      <span>{model.modelName}</span>
                      <span>{formatNumber(model.totalRequests)}</span>
                      <span>{formatNumber(model.successCount)} / {formatNumber(model.failureCount)}</span>
                      <span>{formatNumber(model.totalTokens)}</span>
                      <span>${formatNumber(model.totalCost)}</span>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          )
        })}
      </div>
    </section>
  )
}
