import type { CSSProperties, ReactNode } from 'react'
import { Card, Pagination, Select, Space, Tag, Typography } from 'antd'
import { SectionHeader } from '@/components/layout'
import styles from './CredentialSections.module.scss'
import { formatCompactNumber } from '@/utils/usage'

type CredentialSectionStyle = CSSProperties

interface CredentialSectionShellProps {
  title: string
  subtitle?: string
  countLabel: string
  titleExtra?: ReactNode
  actions?: ReactNode
  style?: CredentialSectionStyle
  children: ReactNode
}

export function CredentialSectionShell({ title, subtitle, countLabel, titleExtra, actions, style, children }: CredentialSectionShellProps) {
  return (
    <Card
      className={styles.credentialSectionCard}
      style={style}
      title={(
        <SectionHeader
          headingLevel={2}
          title={title}
          description={subtitle}
          meta={(
            <div className={styles.credentialSectionTitleMeta}>
              <Tag variant="filled" className={styles.credentialCountBadge}>{countLabel}</Tag>
              {titleExtra}
            </div>
          )}
          actions={actions ? <div className={styles.credentialSectionActions}>{actions}</div> : undefined}
        />
      )}
    >
      <div className={styles.credentialRows}>{children}</div>
    </Card>
  )
}

export function CredentialPriorityBadge({ children }: { children: ReactNode }) {
  return <Tag color="blue" variant="filled" className={styles.credentialPriorityBadge}>{children}</Tag>
}

export function MetricPill({ value }: { value: ReactNode }) {
  return (
    <span className={styles.credentialMetricValueCell}>{value}</span>
  )
}

export function RequestMetric({ total, success, failure }: { total: number; success: number; failure: number }) {
  return (
    <span className={styles.credentialRequestMetric}>
      <strong>{formatCredentialNumber(total)}</strong>
      <span className={styles.credentialRequestBreakdown}>
        (<span className={styles.credentialMetricValueSuccess}>{formatCredentialNumber(success)}</span>/<span className={styles.credentialMetricValueDanger}>{formatCredentialNumber(failure)}</span>)
      </span>
    </span>
  )
}

export function TonePercent({ value, tone }: { value: number | null; tone: 'success' | 'warning' | 'danger' | 'neutral' }) {
  return <span className={credentialToneClassName('credentialMetricValue', tone)}>{formatCredentialPercent(value)}</span>
}

export function successRateTone(value: number | null): 'success' | 'warning' | 'danger' | 'neutral' {
  if (value === null) {
    return 'neutral'
  }
  if (value >= 95) {
    return 'success'
  }
  if (value >= 80) {
    return 'warning'
  }
  return 'danger'
}

export function cacheReadRateTone(value: number | null): 'success' | 'warning' | 'danger' | 'neutral' {
  if (value === null) {
    return 'neutral'
  }
  if (value >= 50) {
    return 'success'
  }
  if (value >= 20) {
    return 'warning'
  }
  return 'neutral'
}

const CREDENTIAL_PAGE_SIZE_OPTIONS = [5, 10, 20, 50]

export function CredentialsPagination({
  leadingControls,
  page,
  total,
  totalPages,
  pageSize,
  sortValue,
  sortOptions,
  sortLabel,
  previousLabel,
  nextLabel,
  rowsPerPageLabel,
  onPageChange,
  onPageSizeChange,
  onSortChange,
}: {
  leadingControls?: ReactNode
  page: number
  total?: number
  totalPages: number
  pageSize: number
  sortValue?: string
  sortOptions?: Array<{ value: string; label: string }>
  sortLabel?: string
  previousLabel: string
  nextLabel: string
  rowsPerPageLabel: string
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onSortChange?: (sort: string) => void
}) {
  if (total === 0) {
    return null
  }

  const safeTotalPages = Math.max(totalPages, 1)
  const safePage = Math.min(Math.max(page, 1), safeTotalPages)
  const paginationTotal = Math.max(total ?? 0, (safeTotalPages - 1) * pageSize + 1)

  return (
    <div className={styles.credentialPagination}>
      <Space wrap size={[16, 12]} className={styles.credentialPaginationControls}>
        {leadingControls}
        {sortOptions && sortOptions.length > 0 && sortLabel && onSortChange && (
          <Space size={8} className={styles.credentialSortControl}>
            <Typography.Text type="secondary">{sortLabel}</Typography.Text>
            <Select
              size="small"
              value={sortValue}
              options={sortOptions}
              onChange={onSortChange}
              aria-label={sortLabel}
              popupMatchSelectWidth={false}
            />
          </Space>
        )}
        <Pagination
          current={safePage}
          total={paginationTotal}
          pageSize={pageSize}
          pageSizeOptions={CREDENTIAL_PAGE_SIZE_OPTIONS.map(String)}
          showSizeChanger
          showLessItems
          responsive
          size="small"
          locale={{
            items_per_page: rowsPerPageLabel,
            prev_page: previousLabel,
            next_page: nextLabel,
          }}
          showTotal={() => `${safePage} / ${safeTotalPages}`}
          onChange={(nextPage, nextPageSize) => {
            if (nextPageSize !== pageSize) {
              onPageSizeChange(nextPageSize)
              return
            }
            onPageChange(nextPage)
          }}
        />
      </Space>
    </div>
  )
}

export function formatCredentialNumber(value: number): string {
  return formatCompactNumber(value)
}

export function formatCredentialPercent(value: number | null): string {
  if (value === null) {
    return '—'
  }
  return `${value.toFixed(2)}%`
}

export function credentialToneClassName(prefix: string, tone: string): string {
  return styles[`${prefix}${capitalize(tone)}`] ?? ''
}

export function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1)
}
