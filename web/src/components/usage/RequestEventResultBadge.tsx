import { useTranslation } from 'react-i18next'
import { IconScrollText } from '@/components/ui/icons'
import styles from './RequestEventResultBadge.module.scss'

type DataAttributes = { [key: `data-${string}`]: string }

interface RequestEventResultBadgeProps {
  failed: boolean
  loading?: boolean
  onOpen?: () => void
  dataAttributes?: DataAttributes
  size?: 'default' | 'compact'
}

export function RequestEventResultBadge({
  failed,
  loading = false,
  onOpen,
  dataAttributes,
  size = 'default',
}: RequestEventResultBadgeProps) {
  const { t } = useTranslation()
  const resultLabel = failed ? t('usage_stats.failure') : t('usage_stats.success')
  const resultLocale = t('usage_stats.success') === 'Success' ? 'en' : 'zh'
  const resultClassName = failed ? styles.requestEventsResultFailed : styles.requestEventsResultSuccess
  const badgeClassName = `${resultClassName} ${size === 'compact' ? styles.requestEventsResultCompact : ''}`.trim()

  if (!onOpen) {
    return (
      <span className={badgeClassName} data-result-locale={resultLocale} {...dataAttributes}>
        {resultLabel}
      </span>
    )
  }

  return (
    <button
      type="button"
      className={`${badgeClassName} ${styles.requestEventsResultLogButton}`.trim()}
      data-result-locale={resultLocale}
      onClick={(event) => {
        event.stopPropagation()
        onOpen()
      }}
      title={t('usage_stats.request_events_log_hint')}
      aria-label={loading
        ? t('usage_stats.request_events_log_loading_aria', { result: resultLabel })
        : t('usage_stats.request_events_log_open_aria', { result: resultLabel })}
      aria-busy={loading}
      disabled={loading}
      {...dataAttributes}
    >
      <span>{resultLabel}</span>
      <span className={styles.requestEventsResultLogIcon} aria-hidden="true">
        <IconScrollText size={9} />
      </span>
    </button>
  )
}
