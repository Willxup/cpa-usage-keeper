import { useEffect, useId, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import type { CredentialViewMode } from './credentialListPreferences'
import type { QuotaUsageMode } from './quota/CredentialQuotaPanel'
import styles from './CredentialViewControls.module.scss'

export function IconChoice({ label, selected, onClick, children }: { label: string; selected: boolean; onClick: () => void; children: ReactNode }) {
  const id = useId()
  const pointerFocus = useRef(false)
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip()
  useEffect(() => {
    if (!tooltip) return
    const handleKey = (event: KeyboardEvent) => { if (event.key === 'Escape') dismiss() }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [tooltip, dismiss])
  return <>
    <button type="button" aria-label={label} aria-pressed={selected} aria-describedby={tooltip ? id : undefined}
      onPointerEnter={(event) => { if (event.pointerType !== 'touch') showOnMouseEnter([label], event.currentTarget) }}
      onPointerLeave={(event) => hideOnMouseLeave(event.currentTarget)}
      onPointerDown={() => { pointerFocus.current = true; dismiss() }}
      onFocus={(event) => { if (!pointerFocus.current) showOnFocus([label], event.currentTarget) }}
      onBlur={(event) => { pointerFocus.current = false; hideOnBlur(event.currentTarget) }}
      onClick={() => { pointerFocus.current = false; dismiss(); onClick() }}>
      {children}
    </button>
    <PortalTooltip tooltip={tooltip} id={id} />
  </>
}

const iconProps = { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true as const }

export function CredentialViewModeSwitch({ mode, onChange }: { mode: CredentialViewMode; onChange: (mode: CredentialViewMode) => void }) {
  const { t } = useTranslation()
  return <div className={styles.segment} role="group" aria-label={t('usage_stats.credentials_view_layout')}>
    <IconChoice label={t('usage_stats.credentials_view_cards')} selected={mode === 'cards'} onClick={() => onChange('cards')}>
      <svg {...iconProps}><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></svg>
    </IconChoice>
    <IconChoice label={t('usage_stats.credentials_view_list')} selected={mode === 'list'} onClick={() => onChange('list')}>
      <svg {...iconProps}><path d="M9 5h12M9 12h12M9 19h12" /><path d="M3 5h.01M3 12h.01M3 19h.01" strokeWidth="3" /></svg>
    </IconChoice>
  </div>
}

export function QuotaUsageModeSwitch({ mode, onChange }: { mode: QuotaUsageMode; onChange: (mode: QuotaUsageMode) => void }) {
  const { t } = useTranslation()
  return <div className={styles.segment} role="group" aria-label={t('usage_stats.credentials_quota_usage_mode_aria')}>
    <IconChoice label={t('usage_stats.credentials_quota_usage_current_full')} selected={mode === 'current'} onClick={() => onChange('current')}>
      <svg {...iconProps}><path d="M4.9 19.1a10 10 0 1 1 14.2 0Z" /><path d="M12 4v2M4 12h2M18 12h2M6.35 6.35l1.4 1.4M17.65 6.35l-1.4 1.4M12 14l3-5" /><circle cx="12" cy="14" r="1" /></svg>
    </IconChoice>
    <IconChoice label={t('usage_stats.credentials_quota_usage_estimated_full')} selected={mode === 'estimated'} onClick={() => onChange('estimated')}>
      <svg {...iconProps}><rect x="5" y="2" width="14" height="20" rx="2" /><path d="M8 6h8M8 11h1M12 11h1M16 11h.01M8 15h1M12 15h1M8 19h1M12 19h1M16 15v4" /></svg>
    </IconChoice>
  </div>
}
