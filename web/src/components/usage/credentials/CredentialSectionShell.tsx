import type { CSSProperties, ReactNode } from 'react'
import styles from './CredentialSections.module.scss'
import { Select } from '@/components/ui/Select'
import { CREDENTIAL_PAGE_SIZE_OPTIONS } from './credentialViewModels'
import type { CredentialViewMode } from './credentialListPreferences'

type CredentialSectionStyle = CSSProperties

interface CredentialSectionShellProps {
  title: string
  viewMode: CredentialViewMode
  contentClassName: string
  titleHelp?: ReactNode
  titleExtra?: ReactNode
  actions?: ReactNode
  style?: CredentialSectionStyle
  children: ReactNode
}

export function CredentialSectionShell({ title, viewMode, contentClassName, titleHelp, titleExtra, actions, style, children }: CredentialSectionShellProps) {
  const heading = <h3 className={`${styles.credentialSectionTitle} keeper-card-title`}>{title}</h3>
  return (
    <section className={`${styles.credentialSectionCard} keeper-card-surface`} data-view-mode={viewMode} style={style}>
      <div className={styles.credentialSectionHeader}>
        <div className={styles.credentialSectionTitleBlock}>
          <div className={`${styles.credentialSectionTitleRow} keeper-card-title-track`}>
            {titleHelp ? <div className={styles.credentialSectionHeading}>{heading}{titleHelp}</div> : heading}
            {titleExtra}
          </div>
        </div>
        {actions && <div className={styles.credentialSectionActions}>{actions}</div>}
      </div>
      <div className={`${styles.credentialRows} ${contentClassName}`} data-view-mode={viewMode}>{children}</div>
    </section>
  )
}

export function CredentialPriorityBadge({ children }: { children: ReactNode }) {
  return <span className={styles.credentialPriorityBadge}>{children}</span>
}

const CREDENTIAL_PAGE_SIZE_SELECT_OPTIONS = CREDENTIAL_PAGE_SIZE_OPTIONS.map((option) => ({
  value: String(option),
  label: String(option),
}))

export function CredentialsPagination({
  className,
  controlsClassName,
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
  className?: string
  controlsClassName?: string
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
    <div className={`${styles.credentialPagination} ${className ?? ''}`.trim()} onFocusCapture={(event) => {
      // 局部滚动条的边缘按钮需完整可见；Select 的 portal 焦点不属于此滚动容器。
      const bar = event.currentTarget
      if (bar.contains(event.target) && bar.scrollWidth > bar.clientWidth) {
        event.target.scrollIntoView({ block: 'nearest', inline: 'nearest' })
      }
    }}>
      <div className={`${styles.credentialPaginationControls} ${controlsClassName ?? ''}`.trim()}>
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
