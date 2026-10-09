import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { BrandLink } from '@/components/BrandLink';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { IconExternalLink, IconMoreHorizontal, IconSun, IconMoon, IconMonitor } from '@/components/ui/icons';
import { Select } from '@/components/ui/Select';
import { isSupportedLanguage, persistLanguage } from '@/i18n';
import { useThemeStore } from '@/stores';
import styles from './DashboardHeader.module.scss';

interface DashboardHeaderProps {
  backToCPA?: string;
  identity?: string;
  onLogout: () => void;
  loggingOut?: boolean;
  onCheckUpdates?: () => void;
  checkingUpdates?: boolean;
  updateAvailable?: boolean;
}

export function DashboardHeader({ backToCPA, identity, onLogout, loggingOut = false, onCheckUpdates, checkingUpdates = false, updateAvailable = false }: DashboardHeaderProps) {
  const { t, i18n } = useTranslation();
  const theme = useThemeStore((state) => state.theme);
  const setTheme = useThemeStore((state) => state.setTheme);
  const [open, setOpen] = useState(false);
  const menuId = useId();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const language = isSupportedLanguage(i18n.language) ? i18n.language : 'en';
  const languages = [{ value: 'en', label: 'English' }, { value: 'zh', label: '简体中文' }, { value: 'zh-TW', label: '繁體中文' }];
  const themes = [{ value: 'white', label: t('usage_stats.theme_light') }, { value: 'dark', label: t('usage_stats.theme_dark') }, { value: 'auto', label: t('usage_stats.theme_auto') }];
  const selectedTheme = theme === 'light' ? 'white' : theme;
  const nextTheme = selectedTheme === 'white' ? 'dark' : selectedTheme === 'dark' ? 'auto' : 'white';
  const currentThemeLabel = themes.find((option) => option.value === selectedTheme)?.label;
  const nextThemeLabel = themes.find((option) => option.value === nextTheme)?.label;
  const ThemeIcon = selectedTheme === 'dark' ? IconMoon : selectedTheme === 'auto' ? IconMonitor : IconSun;

  useEffect(() => {
    if (!open) return;
    menuRef.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus();
    const pointer = (event: PointerEvent) => {
      if (!buttonRef.current?.contains(event.target as Node) && !menuRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { setOpen(false); buttonRef.current?.focus(); } };
    document.addEventListener('pointerdown', pointer); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('pointerdown', pointer); document.removeEventListener('keydown', key); };
  }, [open]);

  return <header className={styles.header} data-dashboard-header>
    <BrandLink className={styles.brand} />
    <div className={styles.actions}>
      {backToCPA && <a className={styles.back} href={backToCPA} target="_blank" rel="noreferrer" aria-label={t('usage_stats.back_to_cpa_aria')}><span className={styles.backFull}>{t('usage_stats.back_to_cpa')}</span><span className={styles.backShort}>CPA</span><IconExternalLink size={14} aria-hidden="true" /></a>}
      {identity && <span className={styles.identity} title={identity}>{identity}</span>}
      <Select
        value={language}
        options={languages}
        onChange={(next) => {
          if (isSupportedLanguage(next)) void i18n.changeLanguage(next).then(() => persistLanguage(next));
        }}
        ariaLabel={`${t('usage_stats.language_switch')}: ${languages.find((option) => option.value === language)?.label}`}
        className={styles.iconSelect}
        dropdownMinWidth={160}
        fullWidth={false}
        showChevron={false}
        renderValue={() => <span data-dashboard-language={language}>{language === 'zh' ? '中' : language === 'zh-TW' ? '繁' : 'EN'}</span>}
      />
      <button
        type="button"
        className={styles.themeToggle}
        aria-label={`${t('usage_stats.theme_switch')}: ${currentThemeLabel}`}
        title={`${currentThemeLabel} → ${nextThemeLabel}`}
        onClick={() => setTheme(nextTheme)}
      >
        <span data-dashboard-theme={selectedTheme}><ThemeIcon size={20} aria-hidden="true" /></span>
      </button>
      <button ref={buttonRef} type="button" className={styles.more} aria-label={t('usage_stats.more_actions')} aria-haspopup="menu" aria-controls={menuId} aria-expanded={open} onClick={() => setOpen((value) => !value)} onKeyDown={(event) => {
        if (event.key === 'ArrowDown') { event.preventDefault(); setOpen(true); }
      }}><IconMoreHorizontal size={20} aria-hidden="true" />{updateAvailable && <span className={styles.updateDot} />}</button>
      {open && <div ref={menuRef} className={styles.menu} id={menuId} role="menu" aria-label={t('usage_stats.more_actions')} onBlur={(event) => {
        // Safari touch clicks can blur before click with no related target; let the menu action finish.
        if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget) && !buttonRef.current?.contains(event.relatedTarget)) setOpen(false);
      }} onKeyDown={(event) => {
        const buttons = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []);
        const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === 'ArrowDown' ? (index + 1) % buttons.length : event.key === 'ArrowUp' ? (index + buttons.length - 1) % buttons.length : event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : -1;
        if (next >= 0) { event.preventDefault(); buttons[next]?.focus(); }
      }}>
        {identity && <div className={styles.menuIdentity}>{identity}</div>}
        {onCheckUpdates && <button type="button" role="menuitem" onClick={onCheckUpdates} disabled={checkingUpdates} aria-busy={checkingUpdates}>{checkingUpdates ? <LoadingSpinner size={14} /> : null}{t(checkingUpdates ? 'common.loading' : 'usage_stats.check_updates')}{updateAvailable && <span className={styles.updateDot} />}</button>}
        <button type="button" role="menuitem" disabled={loggingOut} aria-busy={loggingOut} onClick={() => {
          // 菜单项即将卸载，让退出确认框记录仍在页面上的焦点入口。
          buttonRef.current?.focus({ preventScroll: true });
          setOpen(false);
          onLogout();
        }}>{loggingOut ? <LoadingSpinner size={14} /> : null}{t(loggingOut ? 'common.loading' : 'common.logout')}</button>
      </div>}
    </div>
  </header>;
}
