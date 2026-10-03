import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchReadOnlyKeys } from '@/lib/api';
import { Select } from '@/components/ui/Select';
import { appPath } from '@/lib/api';
import { stripAppBasePath } from '@/lib/usageNavigation';
import {
  getReadOnlyPage,
  KEY_VIEWER_PAGE_PATHS,
  READ_ONLY_PAGE_PATHS,
  type KeyViewerPath,
  type KeyViewerPage,
} from '@/features/key-viewer/navigation';
import { KeyOverviewPage } from './KeyOverviewPage';
import { KeyAnalysisPage } from './KeyAnalysisPage';
import { KeyQuotaPage } from './KeyQuotaPage';
import { KeyRankingPage } from './KeyRankingPage';
import { ReadOnlyReportingPage } from './ReadOnlyReportingPage';

export function ReadOnlyPage({
  onAuthRequired,
}: {
  onAuthRequired: () => void;
}) {
  const [page, setPage] = useState<KeyViewerPage>(
    () =>
      getReadOnlyPage(
        stripAppBasePath(window.location.pathname, window.__APP_BASE_PATH__) ??
          '/',
      ) ?? 'overview',
  );
  const navigate = (path: KeyViewerPath) => {
    const next = (Object.keys(KEY_VIEWER_PAGE_PATHS) as KeyViewerPage[]).find(
      (item) => KEY_VIEWER_PAGE_PATHS[item] === path,
    );
    if (!next) return;
    window.history.replaceState(null, '', appPath(READ_ONLY_PAGE_PATHS[next]));
    setPage(next);
  };
  const { t } = useTranslation();
  const [selectedApiKeyId, setSelectedApiKeyId] = useState('');
  const [keys, setKeys] = useState<{ id: string; label: string }[]>([]);
  const [keysLoading, setKeysLoading] = useState(true);
  const [keysError, setKeysError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    fetchReadOnlyKeys(controller.signal).then(
      (result) => {
        setKeys(result.keys);
        setKeysLoading(false);
      },
      (error) => {
        if (controller.signal.aborted) return;
        if (error instanceof ApiError && error.status === 401) onAuthRequired();
        setKeysError('Unable to load API keys. Reload to retry.');
        setKeysLoading(false);
      },
    );
    return () => controller.abort();
  }, [onAuthRequired]);
  const options = [
    { value: '', label: t('usage_stats.api_key_filter_all') },
    ...keys.map((key) => ({ value: key.id, label: key.label })),
  ];
  const keyFilter = (
    <>
      <Select
        value={selectedApiKeyId}
        options={options}
        onChange={setSelectedApiKeyId}
        disabled={keysLoading || Boolean(keysError)}
        ariaLabel={`${t('usage_stats.api_key_filter')}: ${options.find((option) => option.value === selectedApiKeyId)?.label ?? ''}`}
        fullWidth={false}
        dropdownMinWidth={180}
        renderValue={(option) => (
          <>
            <span data-dashboard-filter-caption>
              {t('usage_stats.api_key_filter')}
            </span>
            <span data-dashboard-filter-value>{option?.label}</span>
          </>
        )}
      />
      {keysError && <span role="alert">{keysError}</span>}
    </>
  );
  const shared = {
    readOnly: true,
    selectedApiKeyId,
    keyFilter,
    onNavigate: navigate,
    onAuthRequired,
  };
  if (page === 'ranking') return <KeyRankingPage {...shared} />;
  if (page === 'events' || page === 'auth-files' || page === 'ai-provider')
    return (
      <ReadOnlyReportingPage
        key={page + selectedApiKeyId}
        {...shared}
        page={page}
      />
    );
  if (page === 'analysis')
    return <KeyAnalysisPage key={selectedApiKeyId} {...shared} />;
  if (page === 'quota') return <KeyQuotaPage {...shared} />;
  return (
    <KeyOverviewPage
      key={selectedApiKeyId}
      {...shared}
      page={page === 'realtime' ? 'realtime' : 'overview'}
    />
  );
}
