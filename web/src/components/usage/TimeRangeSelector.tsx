import type { UsageTimeRange } from '../../lib/types'
import styles from '../../pages/usage/UsagePage.module.css'

interface TimeRangeSelectorProps {
  value: UsageTimeRange
  onChange: (value: UsageTimeRange) => void
}

const options: Array<{ value: UsageTimeRange; label: string }> = [
  { value: 'all', label: 'All data' },
  { value: 'today', label: 'Today' },
  { value: '7d', label: 'Last 7d' },
]

export function TimeRangeSelector({ value, onChange }: TimeRangeSelectorProps) {
  return (
    <label className={styles.timeRangeGroup}>
      <span className={styles.timeRangeLabel}>Range</span>
      <select value={value} onChange={(event) => onChange(event.target.value as UsageTimeRange)} className={styles.timeRangeSelect}>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}
