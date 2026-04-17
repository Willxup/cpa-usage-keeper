import { Area, AreaChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { formatNumber } from '../../lib/usage'
import type { TrendSeries } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface TrendChartCardProps {
  title: string
  periodLabel: string
  series: TrendSeries[]
  valueFormatter?: (value: number) => string
}

export function TrendChartCard({ title, periodLabel, series, valueFormatter = formatNumber }: TrendChartCardProps) {
  const labels = Array.from(new Set(series.flatMap((item) => item.data.map((point) => point.label)))).sort((left, right) => left.localeCompare(right))
  const chartData = labels.map((label) => {
    const row: Record<string, string | number> = { label }
    for (const item of series) {
      const point = item.data.find((entry) => entry.label === label)
      row[item.key] = point?.value ?? 0
    }
    return row
  })

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeader}>
        <div>
          <h2>{title}</h2>
          <p className={styles.panelSubtitle}>{periodLabel}</p>
        </div>
        <span className={styles.rowsCount}>{series.length} series</span>
      </div>
      {chartData.length > 0 ? (
        <>
          <div className={styles.chartLegend} aria-label="Chart legend">
            {series.map((item) => (
              <div key={item.key} className={styles.legendItem} title={item.label}>
                <span className={styles.legendDot} style={{ backgroundColor: item.color }} />
                <span className={styles.legendLabel}>{item.label}</span>
              </div>
            ))}
          </div>
          <div className={styles.chartWrap}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData}>
                <CartesianGrid stroke="var(--border-color)" strokeDasharray="3 3" />
                <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={24} />
                <YAxis tickLine={false} axisLine={false} tickFormatter={valueFormatter} width={72} />
                <Tooltip formatter={(value: number) => valueFormatter(value)} />
                <Legend />
                {series.map((item) => (
                  <Area
                    key={item.key}
                    type="monotone"
                    dataKey={item.key}
                    name={item.label}
                    stroke={item.color}
                    fill={item.color}
                    fillOpacity={0.14}
                    strokeWidth={2}
                  />
                ))}
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </>
      ) : (
        <div className={styles.emptyInline}>No data in selected range.</div>
      )}
    </section>
  )
}
