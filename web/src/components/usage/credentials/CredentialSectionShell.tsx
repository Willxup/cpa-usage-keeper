import type { CSSProperties, ReactNode } from 'react'
import styles from './CredentialSections.module.scss'
import { Select } from '@/components/ui/Select'
import { formatCompactNumber } from '@/utils/usage'
import { CREDENTIAL_PAGE_SIZE_OPTIONS } from './credentialViewModels'

type CredentialSectionStyle = CSSProperties

interface CredentialSectionShellProps {
  title: string
  subtitle: string
  countLabel: string
  titleExtra?: ReactNode
  actions?: ReactNode
  style?: CredentialSectionStyle
  children: ReactNode
}

interface CredentialRowShellProps {
  icon?: ReactNode
  title: ReactNode
  subtitle?: ReactNode
  badges: ReactNode
  metrics: ReactNode
  side: ReactNode
  rowClassName?: string
}

interface CredentialTableHeaderProps {
  nameLabel: string
  totalRequestsLabel: string
  successRateLabel: string
  totalTokensLabel: string
  cacheReadRateLabel: string
  sideLabel: string
  rowClassName?: string
}

export function CredentialSectionShell({ title, subtitle, countLabel, titleExtra, actions, style, children }: CredentialSectionShellProps) {
  return (
    <section className={`${styles.credentialSectionCard} keeper-card-surface`} style={style}>
      <div className={styles.credentialSectionHeader}>
        <div className={styles.credentialSectionTitleBlock}>
          <div className={`${styles.credentialSectionTitleRow} keeper-card-title-track`}>
            <h3 className={`${styles.credentialSectionTitle} keeper-card-title`}>{title}</h3>
            <span className={styles.credentialCountBadge}>{countLabel}</span>
            {titleExtra}
          </div>
          <p className={`${styles.credentialSectionSubtitle} keeper-card-subtitle`}>{subtitle}</p>
        </div>
        {actions && <div className={styles.credentialSectionActions}>{actions}</div>}
      </div>
      <div className={styles.credentialRows}>{children}</div>
    </section>
  )
}

export function CredentialRowShell({ icon, title, subtitle, badges, metrics, side, rowClassName }: CredentialRowShellProps) {
  // 统一三段式行结构：左侧身份信息、中间指标、右侧 quota/状态区域。
  return (
    <article className={`${styles.credentialRow} ${rowClassName ?? ''}`.trim()}>
      <div className={styles.credentialIdentityBlock}>
        {icon}
        <div className={styles.credentialIdentityContent}>
          <div className={styles.credentialNameRow}>
            <div className={styles.credentialNameMain}>
              <span className={styles.credentialDisplayName}>{title}</span>
            </div>
            {badges && <div className={styles.credentialBadges}>{badges}</div>}
          </div>
          {subtitle && <span className={styles.credentialIdentityText}>{subtitle}</span>}
        </div>
      </div>
      <div className={styles.credentialMetricGroup}>{metrics}</div>
      <div className={styles.credentialSidePanel}>{side}</div>
    </article>
  )
}

export function CredentialTableHeader({ nameLabel, totalRequestsLabel, successRateLabel, totalTokensLabel, cacheReadRateLabel, sideLabel, rowClassName }: CredentialTableHeaderProps) {
  return (
    <div className={`${styles.credentialTableHeader} ${rowClassName ?? ''}`.trim()}>
      <span className={styles.credentialTableHeaderName}>{nameLabel}</span>
      <div className={styles.credentialMetricHeaderGroup}>
        <span className={styles.credentialMetricHeaderCell}>{totalRequestsLabel}</span>
        <span className={styles.credentialMetricHeaderCell}>{successRateLabel}</span>
        <span className={styles.credentialMetricHeaderCell}>{totalTokensLabel}</span>
        <span className={styles.credentialMetricHeaderCell}>{cacheReadRateLabel}</span>
      </div>
      <span className={styles.credentialTableHeaderSide}>{sideLabel}</span>
    </div>
  )
}

export function CredentialBadge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'success' | 'warning' | 'danger' }) {
  return <span className={`${styles.credentialBadge} ${styles[`credentialBadge${capitalize(tone)}`]}`.trim()}>{children}</span>
}

export function CredentialPriorityBadge({ children }: { children: ReactNode }) {
  return <span className={styles.credentialPriorityBadge}>{children}</span>
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

const CREDENTIAL_PAGE_SIZE_SELECT_OPTIONS = CREDENTIAL_PAGE_SIZE_OPTIONS.map((option) => ({
  value: String(option),
  label: String(option),
}))

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

  const selectedSortLabel = sortOptions?.find((option) => option.value === sortValue)?.label

  return (
    <div className={styles.credentialPagination}>
      <div className={styles.credentialPaginationControls}>
        {leadingControls}
        {sortOptions && sortOptions.length > 0 && sortLabel && onSortChange && (
          <div className={styles.credentialPageSizeControl}>
            <span>{sortLabel}</span>
            <div className={styles.credentialPaginationSortControl}>
              <span
                className={styles.credentialPaginationSortSizer}
                data-credential-pagination-sort-sizer="true"
                aria-hidden="true"
              >
                {sortOptions.map((option) => <span key={option.value}>{option.label}</span>)}
              </span>
              <Select
                value={sortValue ?? ''}
                options={sortOptions}
                onChange={onSortChange}
                className={`${styles.credentialPaginationSelect} ${styles.credentialPaginationSortSelect}`}
                dropdownClassName={styles.credentialPaginationDropdown}
                ariaLabel={selectedSortLabel ? `${sortLabel}: ${selectedSortLabel}` : sortLabel}
                fullWidth
                dropdownMinWidth={180}
              />
            </div>
          </div>
        )}
        <div className={styles.credentialPageSizeControl}>
          <span>{rowsPerPageLabel}</span>
          <Select
            value={String(pageSize)}
            options={CREDENTIAL_PAGE_SIZE_SELECT_OPTIONS}
            onChange={(value) => onPageSizeChange(Number(value))}
            className={`${styles.credentialPaginationSelect} ${styles.credentialPaginationPageSizeSelect}`}
            dropdownClassName={styles.credentialPaginationDropdown}
            ariaLabel={`${rowsPerPageLabel}: ${pageSize}`}
            fullWidth={false}
            dropdownMinWidth={72}
          />
        </div>
        <button type="button" onClick={() => onPageChange(page - 1)} disabled={page <= 1}>{previousLabel}</button>
        <span className={styles.credentialPaginationPage}>{page} / {totalPages}</span>
        <button type="button" onClick={() => onPageChange(page + 1)} disabled={page >= totalPages}>{nextLabel}</button>
      </div>
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
