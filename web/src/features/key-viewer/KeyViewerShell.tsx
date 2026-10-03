import { useCallback, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { AuthSessionAPIKeySummary } from '@/lib/types';
import { appPath, logout } from '@/lib/api';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { KEY_VIEWER_PAGE_PATHS, READ_ONLY_PAGE_PATHS, type KeyViewerPage, type KeyViewerPath } from './navigation';
import styles from './KeyViewerShell.module.scss';
import { DashboardHeader } from '@/components/dashboard/DashboardHeader';
import { DashboardToolbar } from '@/components/dashboard/DashboardToolbar';

const KEY_VIEWER_PAGE_LABEL_KEYS: Record<KeyViewerPage, string> = {
  overview: 'usage_stats.tab_overview',
  realtime: 'usage_stats.tab_realtime',
  analysis: 'usage_stats.tab_analysis',
  ranking: 'usage_stats.tab_ranking',
  quota: 'key_quota.tab', events: 'usage_stats.tab_events', 'auth-files': 'usage_stats.tab_auth_files', 'ai-provider': 'usage_stats.tab_ai_provider',
};

interface KeyViewerShellProps {
  activePage: KeyViewerPage;
  apiKey?: AuthSessionAPIKeySummary;
  readOnly?: boolean;
  loading?: boolean;
  filters?: ReactNode[];
  onRefresh: () => void;
  refreshing?: boolean;
  refreshDisabled?: boolean;
  autoRefreshControl?: ReactNode;
  children: ReactNode;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyViewerShell({
  activePage,
  apiKey,
  readOnly = false,
  loading = false,
  filters,
  onRefresh,
  refreshing,
  refreshDisabled,
  autoRefreshControl,
  children,
  onNavigate,
  onAuthRequired,
}: KeyViewerShellProps) {
  const { t } = useTranslation();
  const [loggingOut, setLoggingOut] = useState(false);
  const identityLabel = readOnly ? t('read_only.title') : apiKey?.display_key || t('key_overview.identity_unknown');

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
        <DashboardHeader identity={identityLabel} onLogout={() => void handleLogout()} loggingOut={loggingOut} />

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

            <DashboardToolbar
              activeId={activePage}
              items={(Object.keys(KEY_VIEWER_PAGE_PATHS) as KeyViewerPage[]).filter((page) => (readOnly || !['events','auth-files','ai-provider'].includes(page)) && (page !== 'quota' || readOnly || apiKey?.quota_enabled === true)).map((page) => ({ id: page, label: t(KEY_VIEWER_PAGE_LABEL_KEYS[page]), href: appPath(readOnly ? READ_ONLY_PAGE_PATHS[page] : KEY_VIEWER_PAGE_PATHS[page]) }))}
              onNavigate={(page) => onNavigate(KEY_VIEWER_PAGE_PATHS[page])}
              filters={filters}
              onRefresh={onRefresh}
              refreshing={refreshing}
              refreshDisabled={refreshDisabled}
              autoRefreshControl={autoRefreshControl}
            />

            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
