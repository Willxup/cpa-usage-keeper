import { useMemo, useState } from 'react'
import { formatNumber } from '../../lib/usage'
import type { EventRow } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface RequestEventsCardProps {
  items: EventRow[]
}

const ALL = '__all__'

function downloadFile(filename: string, content: string, type: string) {
  const blob = new Blob([content], { type })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}

export function RequestEventsCard({ items }: RequestEventsCardProps) {
  const [modelFilter, setModelFilter] = useState(ALL)
  const [sourceFilter, setSourceFilter] = useState(ALL)
  const [authFilter, setAuthFilter] = useState(ALL)

  const modelOptions = useMemo(() => [ALL, ...new Set(items.map((item) => item.modelName))], [items])
  const sourceOptions = useMemo(() => [ALL, ...new Set(items.map((item) => item.source))], [items])
  const authOptions = useMemo(() => [ALL, ...new Set(items.map((item) => item.authIndex))], [items])

  const filteredItems = useMemo(
    () =>
      items.filter((item) => {
        if (modelFilter !== ALL && item.modelName !== modelFilter) return false
        if (sourceFilter !== ALL && item.source !== sourceFilter) return false
        if (authFilter !== ALL && item.authIndex !== authFilter) return false
        return true
      }),
    [authFilter, items, modelFilter, sourceFilter],
  )

  function handleExportCsv() {
    const header = ['time', 'api', 'model', 'source', 'auth', 'status', 'latency_ms', 'input_tokens', 'output_tokens', 'reasoning_tokens', 'cached_tokens', 'total_tokens']
    const rows = filteredItems.map((item) => [
      item.timestamp,
      item.apiName,
      item.modelName,
      item.source,
      item.authIndex,
      item.failed ? 'failed' : 'success',
      item.latencyMs,
      item.inputTokens,
      item.outputTokens,
      item.reasoningTokens,
      item.cachedTokens,
      item.totalTokens,
    ])
    const csv = [header.join(','), ...rows.map((row) => row.map((value) => `"${String(value).replace(/"/g, '""')}"`).join(','))].join('\n')
    downloadFile('usage-events.csv', csv, 'text/csv;charset=utf-8')
  }

  function handleExportJson() {
    downloadFile('usage-events.json', JSON.stringify(filteredItems, null, 2), 'application/json;charset=utf-8')
  }

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>Request event details</h2>
          <p className={styles.panelSubtitle}>Filter and export request-level usage details derived from persisted events.</p>
        </div>
        <span className={styles.rowsCount}>{filteredItems.length} rows</span>
      </div>

      <div className={styles.filtersRow}>
        <label className={styles.filterField}>
          <span>Model</span>
          <select value={modelFilter} onChange={(event) => setModelFilter(event.target.value)}>
            {modelOptions.map((option) => (
              <option key={option} value={option}>{option === ALL ? 'All models' : option}</option>
            ))}
          </select>
        </label>
        <label className={styles.filterField}>
          <span>Source</span>
          <select value={sourceFilter} onChange={(event) => setSourceFilter(event.target.value)}>
            {sourceOptions.map((option) => (
              <option key={option} value={option}>{option === ALL ? 'All sources' : option}</option>
            ))}
          </select>
        </label>
        <label className={styles.filterField}>
          <span>Auth</span>
          <select value={authFilter} onChange={(event) => setAuthFilter(event.target.value)}>
            {authOptions.map((option) => (
              <option key={option} value={option}>{option === ALL ? 'All auth' : option}</option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className={styles.secondaryButton}
          onClick={() => {
            setModelFilter(ALL)
            setSourceFilter(ALL)
            setAuthFilter(ALL)
          }}
        >
          Clear filters
        </button>
        <button type="button" className={styles.secondaryButton} onClick={handleExportCsv} disabled={filteredItems.length === 0}>
          Export CSV
        </button>
        <button type="button" className={styles.secondaryButton} onClick={handleExportJson} disabled={filteredItems.length === 0}>
          Export JSON
        </button>
      </div>

      <div className={styles.tableScroller}>
        <div className={styles.table}>
          <div className={styles.eventsHeaderRow}>
            <span>Time</span>
            <span>API / model</span>
            <span>Source</span>
            <span>Auth</span>
            <span>Status</span>
            <span>Latency</span>
            <span>Input</span>
            <span>Output</span>
            <span>Reasoning</span>
            <span>Cached</span>
            <span>Total</span>
          </div>
          {filteredItems.map((event) => (
            <div key={`${event.timestamp}-${event.apiName}-${event.modelName}-${event.source}-${event.authIndex}`} className={styles.eventsDataRow}>
              <span>{new Date(event.timestamp).toLocaleString()}</span>
              <span>
                <strong>{event.apiName}</strong>
                <small className={styles.subtleBlock}>{event.modelName}</small>
              </span>
              <span>{event.source}</span>
              <span>{event.authIndex}</span>
              <span className={event.failed ? styles.statusFailed : styles.statusSuccess}>{event.failed ? 'Failed' : 'Success'}</span>
              <span>{formatNumber(event.latencyMs)} ms</span>
              <span>{formatNumber(event.inputTokens)}</span>
              <span>{formatNumber(event.outputTokens)}</span>
              <span>{formatNumber(event.reasoningTokens)}</span>
              <span>{formatNumber(event.cachedTokens)}</span>
              <span>{formatNumber(event.totalTokens)}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
