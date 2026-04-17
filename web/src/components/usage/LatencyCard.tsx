import { formatNumber } from '../../lib/usage'
import styles from '../../pages/usage/UsagePage.module.css'

interface LatencyCardProps {
  averageLatencyMs: number
  maxLatencyMs: number
  sampleCount: number
}

export function LatencyCard({ averageLatencyMs, maxLatencyMs, sampleCount }: LatencyCardProps) {
  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>Latency</h2>
          <p className={styles.panelSubtitle}>Average and peak latency across currently visible request events.</p>
        </div>
        <span>{formatNumber(sampleCount)} samples</span>
      </div>
      <div className={styles.latencyGrid}>
        <article className={styles.latencyMetric}>
          <span className={styles.latencyLabel}>Average</span>
          <strong>{formatNumber(averageLatencyMs)} ms</strong>
        </article>
        <article className={styles.latencyMetric}>
          <span className={styles.latencyLabel}>Peak</span>
          <strong>{formatNumber(maxLatencyMs)} ms</strong>
        </article>
      </div>
    </section>
  )
}
