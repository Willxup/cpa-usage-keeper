import React, { useCallback, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import styles from './FailureDetailsModal.module.scss'

export type FailureDetailsData = {
  requestId?: string
  failureStatusCode: number | null
  failureCode: string
  failureMessage: string
  failureBody: string
  source?: string
  authIndex?: string
}

type Props = {
  data: FailureDetailsData
  onClose: () => void
}

export function FailureDetailsModal({ data, onClose }: Props) {
  const { t } = useTranslation()

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    },
    [onClose],
  )

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  return createPortal(
    <div className={styles.overlay} role="presentation" onClick={onClose}>
      <section
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby="failure-details-title"
        onClick={(e) => e.stopPropagation()}
      >
        <header className={styles.header}>
          <h2 id="failure-details-title" className={styles.title}>
            {t('usage_stats.request_events_failure_details_title')}
          </h2>
          <button type="button" className={styles.closeButton} onClick={onClose}>
            {t('common.close')}
          </button>
        </header>
        <div className={styles.content}>
          {data.requestId && (
            <div className={styles.field}>
              <div className={styles.label}>
                {t('usage_stats.request_events_failure_request_id')}
              </div>
              <div className={styles.value}>{data.requestId}</div>
            </div>
          )}
          <div className={styles.field}>
            <div className={styles.label}>
              {t('usage_stats.request_events_failure_status_code')}
            </div>
            <div className={styles.value}>
              {data.failureStatusCode != null ? (
                <span className={styles.statusBadge}>{data.failureStatusCode}</span>
              ) : (
                '-'
              )}
            </div>
          </div>
          <div className={styles.field}>
            <div className={styles.label}>
              {t('usage_stats.request_events_failure_code')}
            </div>
            <div className={styles.value}>{data.failureCode || '-'}</div>
          </div>
          <div className={styles.field}>
            <div className={styles.label}>
              {t('usage_stats.request_events_failure_message')}
            </div>
            <div className={styles.value}>{data.failureMessage || '-'}</div>
          </div>
          <div className={styles.field}>
            <div className={styles.label}>
              {t('usage_stats.request_events_failure_body')}
            </div>
            <pre className={styles.bodyBox}>{data.failureBody || '-'}</pre>
          </div>
          {(data.source || data.authIndex) && (
            <div className={styles.field}>
              <div className={styles.label}>
                {t('usage_stats.request_events_failure_source_auth')}
              </div>
              <div className={styles.value}>
                {data.source || '-'} / {data.authIndex || '-'}
              </div>
            </div>
          )}
        </div>
      </section>
    </div>,
    document.body,
  )
}
