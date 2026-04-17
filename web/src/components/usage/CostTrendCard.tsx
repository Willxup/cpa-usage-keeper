import { formatNumber } from '../../lib/usage'
import type { TrendPoint } from '../../lib/types'
import { TrendChartCard } from './TrendChartCard'

interface CostTrendCardProps {
  data: TrendPoint[]
}

export function CostTrendCard({ data }: CostTrendCardProps) {
  return (
    <TrendChartCard
      title="Cost Trend"
      periodLabel="Estimated cost per hour using backend-saved model pricing."
      series={[{ key: 'cost', label: 'Cost', color: '#f59e0b', data }]}
      valueFormatter={(value) => `$${formatNumber(Number(value.toFixed(4)))}`}
    />
  )
}
