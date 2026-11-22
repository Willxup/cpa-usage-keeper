import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { Modal } from '@/components/ui/Modal'
import { Select } from '@/components/ui/Select'
import { fetchQuotaAutoRefreshSettings, updateQuotaAutoRefreshSettings } from '@/lib/api'
import type { QuotaAutoRefreshScheduleUnit, QuotaAutoRefreshSettings } from '@/lib/types'
import styles from './QuotaAutoRefreshSettings.module.scss'
import shared from '../CredentialSections.module.scss'
const AUTO_REFRESH_SCHEDULE_LIMITS: Record<QuotaAutoRefreshScheduleUnit, number> = { minute: 60, hour: 24, day: 30, week: 7 }
const AUTO_REFRESH_SCHEDULE_UNITS: QuotaAutoRefreshScheduleUnit[] = ['minute', 'hour', 'day', 'week']
const AUTO_REFRESH_WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const

type AutoRefreshSettingsFormSetters = {
  setEnabled: (enabled: boolean) => void
  setUnit: (unit: QuotaAutoRefreshScheduleUnit) => void
  setValue: (value: string) => void
}

type AutoRefreshSettingsBuildResult =
  | { settings: QuotaAutoRefreshSettings; errorKey?: never }
  | { settings?: never; errorKey: string }

function applyQuotaAutoRefreshSettingsForm(settings: QuotaAutoRefreshSettings, setters: AutoRefreshSettingsFormSetters) {
  setters.setEnabled(settings.enabled)
  if (settings.schedule) {
    setters.setUnit(settings.schedule.unit)
    setters.setValue(String(settings.schedule.value))
    return
  }
  setters.setUnit('minute')
  setters.setValue('')
}

export function buildQuotaAutoRefreshSettings({
  enabled,
  unit,
  value,
}: {
  enabled: boolean
  unit: QuotaAutoRefreshScheduleUnit
  value: string
}): AutoRefreshSettingsBuildResult {
  if (!enabled) {
    return { settings: { enabled: false, schedule: null } }
  }
  const trimmedValue = value.trim()
  if (trimmedValue === '') {
    return { errorKey: 'usage_stats.credentials_auto_refresh_validation_required' }
  }
  const numericValue = Number(trimmedValue)
  const max = AUTO_REFRESH_SCHEDULE_LIMITS[unit]
  if (!Number.isInteger(numericValue) || numericValue < 1 || numericValue > max) {
    return { errorKey: 'usage_stats.credentials_auto_refresh_validation_range' }
  }
  return {
    settings: {
      enabled: true,
      schedule: {
        unit,
        value: numericValue,
      },
    },
  }
}

export function isAutoRefreshSettingsSaveDisabled({
  loading,
  saving,
  loaded,
}: {
  loading: boolean
  saving: boolean
  loaded: boolean
}): boolean {
  return loading || saving || !loaded
}

export function isAutoRefreshSettingsControlDisabled({
  loading,
  saving,
  loaded,
}: {
  loading: boolean
  saving: boolean
  loaded: boolean
}): boolean {
  return loading || saving || !loaded
}

export function resolveQuotaAutoRefreshSettingsLoadFailure(nextError: unknown, fallbackMessage: string): { settings: QuotaAutoRefreshSettings; error: string; loaded: true } {
  return {
    settings: { enabled: false, schedule: null },
    error: nextError instanceof Error && nextError.message ? nextError.message : fallbackMessage,
    loaded: true,
  }
}

export function QuotaAutoRefreshSettingsModal({ open: settingsModalOpen, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [settingsError, setSettingsError] = useState('')
  const [settingsEnabled, setSettingsEnabled] = useState(false)
  const [scheduleUnit, setScheduleUnit] = useState<QuotaAutoRefreshScheduleUnit>('minute')
  const [scheduleValue, setScheduleValue] = useState('')
  const scheduleMax = AUTO_REFRESH_SCHEDULE_LIMITS[scheduleUnit]

  useEffect(() => {
    if (!settingsModalOpen) {
      setSettingsError('')
      setSettingsLoaded(false)
      return
    }
    const controller = new AbortController()
    setSettingsLoading(true)
    setSettingsLoaded(false)
    setSettingsError('')
    void fetchQuotaAutoRefreshSettings(controller.signal)
      .then((settings) => {
        if (controller.signal.aborted) return
        applyQuotaAutoRefreshSettingsForm(settings, {
          setEnabled: setSettingsEnabled,
          setUnit: setScheduleUnit,
          setValue: setScheduleValue,
        })
        setSettingsLoaded(true)
      })
      .catch((nextError: unknown) => {
        if (controller.signal.aborted) return
        const fallback = resolveQuotaAutoRefreshSettingsLoadFailure(nextError, t('usage_stats.credentials_auto_refresh_load_failed'))
        applyQuotaAutoRefreshSettingsForm(fallback.settings, {
          setEnabled: setSettingsEnabled,
          setUnit: setScheduleUnit,
          setValue: setScheduleValue,
        })
        setSettingsLoaded(fallback.loaded)
        setSettingsError(fallback.error)
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setSettingsLoading(false)
        }
      })
    return () => controller.abort()
  }, [settingsModalOpen, t])

  const handleSaveAutoRefreshSettings = async () => {
    if (isAutoRefreshSettingsSaveDisabled({ loading: settingsLoading, saving: settingsSaving, loaded: settingsLoaded })) {
      return
    }
    const nextSettings = buildQuotaAutoRefreshSettings({
      enabled: settingsEnabled,
      unit: scheduleUnit,
      value: scheduleValue,
    })
    const errorKey = 'errorKey' in nextSettings ? nextSettings.errorKey : ''
    if (errorKey) {
      setSettingsError(t(errorKey, { max: String(scheduleMax) }))
      return
    }
    const settingsToSave = nextSettings.settings
    if (!settingsToSave) {
      return
    }
    setSettingsSaving(true)
    setSettingsError('')
    try {
      const saved = await updateQuotaAutoRefreshSettings(settingsToSave)
      applyQuotaAutoRefreshSettingsForm(saved, {
        setEnabled: setSettingsEnabled,
        setUnit: setScheduleUnit,
        setValue: setScheduleValue,
      })
    } catch (nextError) {
      setSettingsError(nextError instanceof Error ? nextError.message : t('usage_stats.credentials_auto_refresh_save_failed'))
    } finally {
      setSettingsSaving(false)
    }
  }

  return <QuotaAutoRefreshSettingsForm open={settingsModalOpen} enabled={settingsEnabled} unit={scheduleUnit} value={scheduleValue}
    loading={settingsLoading} saving={settingsSaving} loaded={settingsLoaded} error={settingsError}
    onClose={() => { if (!settingsSaving) onClose() }} onEnabledChange={setSettingsEnabled}
    onUnitChange={(unit) => { setScheduleUnit(unit); setScheduleValue('') }} onValueChange={setScheduleValue}
    onSave={handleSaveAutoRefreshSettings} />
}

export function QuotaAutoRefreshSettingsForm({
  open,
  enabled,
  unit,
  value,
  loading,
  saving,
  loaded,
  error,
  onClose,
  onEnabledChange,
  onUnitChange,
  onValueChange,
  onSave,
}: {
  open: boolean
  enabled: boolean
  unit: QuotaAutoRefreshScheduleUnit
  value: string
  loading: boolean
  saving: boolean
  loaded: boolean
  error: string
  onClose: () => void
  onEnabledChange: (enabled: boolean) => void
  onUnitChange: (unit: QuotaAutoRefreshScheduleUnit) => void
  onValueChange: (value: string) => void
  onSave: () => Promise<void>
}) {
  const { t } = useTranslation()
  const scheduleMax = AUTO_REFRESH_SCHEDULE_LIMITS[unit]
  const controlsDisabled = isAutoRefreshSettingsControlDisabled({ loading, saving, loaded })
  const scheduleControlsDisabled = controlsDisabled || !enabled
  const saveDisabled = isAutoRefreshSettingsSaveDisabled({ loading, saving, loaded })
  const scheduleAreaClassName = `${styles.credentialAutoRefreshScheduleArea} ${enabled ? styles.credentialAutoRefreshScheduleAreaActive : ''}`.trim()

  return (
    <Modal
      open={open}
      title={t('usage_stats.credentials_auto_refresh_settings')}
      onClose={saving ? () => undefined : onClose}
      width={620}
      className={styles.credentialAutoRefreshSettingsModal}
      closeDisabled={saving}
    >
      <div className={styles.credentialAutoRefreshSettingsPanel}>
        <div className={styles.credentialAutoRefreshSettingsRow}>
          <label className={`${shared.credentialActiveOnlySwitch} ${controlsDisabled ? shared.credentialActiveOnlySwitchDisabled : ''}`.trim()}>
            <span className={shared.credentialActiveOnlyLabel}>{t('usage_stats.credentials_auto_refresh_enabled')}</span>
            <input type="checkbox" checked={enabled} onChange={(event) => onEnabledChange(event.target.checked)} disabled={controlsDisabled} />
            <span className={shared.credentialActiveOnlyTrack} aria-hidden="true">
              <span className={shared.credentialActiveOnlyThumb} />
            </span>
          </label>
          <button
            type="button"
            className={styles.credentialAutoRefreshSaveButton}
            onClick={() => void onSave()}
            disabled={saveDisabled}
            aria-busy={saving}
            aria-label={saving ? t('usage_stats.credentials_auto_refresh_saving') : t('usage_stats.credentials_auto_refresh_save')}
            title={saving ? t('usage_stats.credentials_auto_refresh_saving') : t('usage_stats.credentials_auto_refresh_save')}
          >
            {saving ? <LoadingSpinner size={12} /> : t('usage_stats.credentials_auto_refresh_save')}
          </button>
        </div>
        <div className={scheduleAreaClassName} aria-hidden={!enabled}>
          <div className={styles.credentialAutoRefreshScheduleGrid}>
            <div className={styles.credentialAutoRefreshUnitSwitcher} role="group" aria-label={t('usage_stats.credentials_auto_refresh_unit')}>
              {AUTO_REFRESH_SCHEDULE_UNITS.map((nextUnit) => (
                <button
                  key={nextUnit}
                  type="button"
                  className={nextUnit === unit ? styles.credentialAutoRefreshUnitButtonActive : undefined}
                  onClick={() => onUnitChange(nextUnit)}
                  aria-pressed={nextUnit === unit}
                  disabled={scheduleControlsDisabled}
                >
                  {t(`usage_stats.credentials_auto_refresh_unit_${nextUnit}`)}
                </button>
              ))}
            </div>
            {unit === 'week' ? (
              <label className={styles.credentialAutoRefreshIntervalField}>
                <span className={styles.credentialAutoRefreshIntervalLabel}>{t('usage_stats.credentials_auto_refresh_weekday')}</span>
                <Select
                  value={value}
                  options={[
                    { value: '', label: t('usage_stats.credentials_auto_refresh_select') },
                    ...AUTO_REFRESH_WEEKDAYS.map((weekday) => ({ value: String(weekday), label: t(`usage_stats.credentials_auto_refresh_weekday_${weekday}`) })),
                  ]}
                  onChange={onValueChange}
                  disabled={scheduleControlsDisabled}
                  ariaLabel={`${t('usage_stats.credentials_auto_refresh_weekday')}: ${t(value ? `usage_stats.credentials_auto_refresh_weekday_${value}` : 'usage_stats.credentials_auto_refresh_select')}`}
                  className={styles.credentialAutoRefreshWeekdaySelect}
                />
              </label>
            ) : (
              <label className={styles.credentialAutoRefreshIntervalField}>
                <span className={styles.credentialAutoRefreshIntervalLabel}>{t('usage_stats.credentials_auto_refresh_value')}</span>
                <input
                  type="number"
                  min={1}
                  max={scheduleMax}
                  step={1}
                  value={value}
                  onChange={(event) => onValueChange(event.target.value)}
                  disabled={scheduleControlsDisabled}
                />
                <span className={styles.credentialAutoRefreshUnitSuffix}>{t(`usage_stats.credentials_auto_refresh_unit_${unit}`)}</span>
              </label>
            )}
          </div>
          <div className={styles.credentialAutoRefreshScheduleTip}>
            {t(`usage_stats.credentials_auto_refresh_tip_${unit}`)}
          </div>
        </div>
        {loading && <div className={shared.credentialEmptyState}>{t('common.loading')}</div>}
        {error && <div className={shared.credentialInlineError} role="alert">{error}</div>}
      </div>
    </Modal>
  )
}

