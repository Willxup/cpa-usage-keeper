import { useTranslation } from 'react-i18next'
import { AppFooter } from '@/components/AppFooter'
import { BrandLink } from '@/components/BrandLink'
import { Button } from '@/components/ui/Button'
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher'
import { isCPAMCEmbed } from '@/embed/cpamcEmbed'
import type { StartupStatus } from '@/lib/startup'
import { useThemeStore } from '@/stores'
import type { Theme } from '@/types'
import styles from './StartupPage.module.scss'

const THEMES: ReadonlyArray<{ value: Theme; key: string }> = [
  { value: 'white', key: 'usage_stats.theme_light' },
  { value: 'dark', key: 'usage_stats.theme_dark' },
  { value: 'auto', key: 'usage_stats.theme_auto' },
]

interface StartupPageProps {
  status: StartupStatus | null
  connectionIssue: boolean
  onRetry: () => void
}

// 未登录的启动页只展示本地安全文案和后端确认的处理数；总量未知时不显示比例。
export function StartupPage({ status, connectionIssue, onRetry }: StartupPageProps) {
  const { t, i18n } = useTranslation()
  const theme = useThemeStore((state) => state.theme)
  const setTheme = useThemeStore((state) => state.setTheme)
  const phase = status?.phase ?? 'opening'
  const progress = status?.progress
  const number = new Intl.NumberFormat(i18n.language)
  const processed = progress ? number.format(progress.processed_count) : ''
  const total = progress?.total_count === null || progress?.total_count === undefined
    ? null : number.format(progress.total_count)
  const percent = progress && progress.total_count !== null && progress.total_count > 0 &&
    progress.processed_count <= progress.total_count
    ? Math.floor((progress.processed_count / progress.total_count) * 100) : null

  return (
    <div className="app-frame" data-embed={isCPAMCEmbed() ? 'cpamc' : undefined}>
      <main className="app-main">
        <section className={styles.shell} data-keeper-page="startup" aria-label={t('startup.region')}>
          <div className={styles.frame}>
            <header className={styles.header}>
              <BrandLink className={styles.brand} />
              <div className={styles.controls}>
                <LanguageSwitcher />
                <div className={styles.themes} role="group" aria-label={t('usage_stats.theme_switch')}>
                  {THEMES.map(({ value, key }) => (
                    <button key={value} type="button" className={styles.themeButton}
                      aria-pressed={theme === value} onClick={() => setTheme(value)}>{t(key)}</button>
                  ))}
                </div>
              </div>
            </header>
            <div className={styles.card}>
              <span className={styles.eyebrow}>{t('startup.region')}</span>
              <div className={styles.phase} data-phase={phase} aria-live="polite">
                <span className={styles.indicator} aria-hidden="true" />
                <span>{t(`startup.phase_${phase}`)}</span>
              </div>
              <h1>{t(`startup.title_${phase}`)}</h1>
              <p className={styles.description}>{t(`startup.description_${phase}`)}</p>
              {progress && phase === 'migrating' && (
                <div className={styles.progress} aria-live="polite">
                  <span>{total === null
                    ? t('startup.progress_unknown', { processed })
                    : t('startup.progress_known', { processed, total })}</span>
                  {percent !== null && <>
                    <div className={styles.track} role="progressbar" aria-valuemin={0} aria-valuemax={100}
                      aria-valuenow={percent} aria-label={t('startup.progress_label')}>
                      <span style={{ width: `${percent}%` }} />
                    </div>
                    <span>{percent}%</span>
                  </>}
                </div>
              )}
              {phase === 'failed' && <p className={styles.nextStep}>{t('startup.failed_next_step')}</p>}
              {connectionIssue && <p className={styles.connection} role="alert">{t('startup.connection_issue')}</p>}
              {(phase === 'failed' || connectionIssue) && <Button type="button" variant="secondary" onClick={onRetry}>
                {t('startup.check_again')}
              </Button>}
            </div>
          </div>
        </section>
      </main>
      <AppFooter loadVersion={false} />
    </div>
  )
}
