import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { AuthSessionAPIKeySummary } from '@/lib/types';
import type { Theme } from '@/types';
import { logout } from '@/lib/api';
import { BrandLink } from '@/components/BrandLink';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { MainActionButton } from '@/components/ui/MainActionButton';
import { useThemeStore } from '@/stores';
import { KEY_VIEWER_PAGE_PATHS, type KeyViewerPage, type KeyViewerPath } from './navigation';
import styles from './KeyViewerShell.module.scss';

const KEY_VIEWER_PAGE_LABEL_KEYS: Record<KeyViewerPage, string> = {
  overview: 'usage_stats.tab_overview',
  analysis: 'usage_stats.tab_analysis',
  ranking: 'usage_stats.tab_ranking',
};

const THEME_OPTIONS: ReadonlyArray<{ value: Theme; labelKey: string }> = [
  { value: 'white', labelKey: 'usage_stats.theme_light' },
  { value: 'dark', labelKey: 'usage_stats.theme_dark' },
  { value: 'auto', labelKey: 'usage_stats.theme_auto' },
];

interface KeyViewerShellProps {
  activePage: KeyViewerPage;
  apiKey?: AuthSessionAPIKeySummary;
  loading?: boolean;
  toolbar: ReactNode;
  children: ReactNode;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyViewerShell({
  activePage,
  apiKey,
  loading = false,
  toolbar,
  children,
  onNavigate,
  onAuthRequired,
}: KeyViewerShellProps) {
  const { t, i18n } = useTranslation();
  const theme = useThemeStore((state) => state.theme);
  const setTheme = useThemeStore((state) => state.setTheme);
  const [loggingOut, setLoggingOut] = useState(false);
  const themeOptions = useMemo(
    () => THEME_OPTIONS.map((option) => ({ ...option, label: t(option.labelKey) })),
    [t],
  );
  const identityLabel = apiKey?.display_key || t('key_overview.identity_unknown');

  const handleLogout = useCallback(async () => {
    setLoggingOut(true);
    try {
      await logout();
    } finally {
      onAuthRequired?.();
      setLoggingOut(false);
    }
  }, [onAuthRequired]);

  return (
    <div className={styles.pageShell} data-keeper-page="key-viewer">
      <div className={styles.pageFrame}>
        <header className={styles.topBar}>
          <div className={styles.brandBlock}>
            <BrandLink className={styles.eyebrow} />
          </div>
          <div className={styles.topBarActions}>
            <span className={styles.identityChip} title={identityLabel}>
              <span className={styles.identityDot} aria-hidden="true" />
              <span className={styles.identityText}>{identityLabel}</span>
            </span>
            <LanguageSwitcher />
            <div className={styles.themeSwitcher} role="tablist" aria-label={t('usage_stats.theme_switch')}>
              {themeOptions.map((option) => {
                const active = theme === option.value;
                return (
                  <button
                    key={option.value}
                    type="button"
                    role="tab"
                    aria-selected={active}
                    className={`${styles.themePill} ${active ? styles.themePillActive : ''}`.trim()}
                    onClick={() => setTheme(option.value)}
                  >
                    {option.label}
                  </button>
                );
              })}
            </div>
            <MainActionButton
              type="button"
              aria-label={t('common.logout')}
              onClick={() => void handleLogout()}
              disabled={loggingOut}
              loading={loggingOut}
            >
              {loggingOut ? t('common.loading') : t('common.logout')}
            </MainActionButton>
          </div>
        </header>

        <main className={styles.contentColumn}>
          <div className={styles.container}>
            {loading && (
              <div className={styles.loadingOverlay} aria-busy="true">
                <div className={styles.loadingOverlayContent}>
                  <LoadingSpinner size={28} className={styles.loadingOverlaySpinner} />
                  <span className={styles.loadingOverlayText}>{t('common.loading')}</span>
                </div>
              </div>
            )}

            <div className={styles.toolbarRow}>
              <div
                className={`${styles.tabBar} ${styles.tabBarConnected}`.trim()}
                role="tablist"
                aria-label={t('key_overview.tabs_aria_label')}
                lang={i18n.resolvedLanguage || i18n.language}
              >
                {(Object.keys(KEY_VIEWER_PAGE_PATHS) as KeyViewerPage[]).map((page) => {
                  const active = page === activePage;
                  return (
                    <button
                      key={page}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      className={`${styles.tabPill} ${active ? styles.tabPillActive : ''}`.trim()}
                      onClick={() => onNavigate(KEY_VIEWER_PAGE_PATHS[page])}
                    >
                      {t(KEY_VIEWER_PAGE_LABEL_KEYS[page])}
                    </button>
                  );
                })}
              </div>
              <div className={styles.toolbarActionsRight}>{toolbar}</div>
            </div>

            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
