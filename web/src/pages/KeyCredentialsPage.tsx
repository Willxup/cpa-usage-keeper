import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, appPath, fetchKeyCredential, fetchKeyStatus } from '@/lib/api';
import type { AuthSessionAPIKeySummary } from '@/lib/types';
import {
  CredentialDetailDrawer,
  CredentialProviderFilterBar,
  CredentialsSection,
  type CredentialDetailSelection,
} from '@/components/usage';
import {
  buildCredentialRows,
  selectQuotaEligibleAuthIndexes,
  updateCredentialDetailStats,
} from '@/components/usage/credentials/credentialViewModels';
import { buildCredentialProviderSearch } from '@/components/usage/credentials/credentialProviderFilters';
import { CREDENTIAL_PAGES_REFRESH_INTERVAL_MS, useCredentialPages } from '@/components/usage/credentials/useCredentialPages';
import { useQuotaCache } from '@/components/usage/credentials/useQuotaCache';
import { useQuotaRefreshTasks } from '@/components/usage/credentials/useQuotaRefreshTasks';
import { buildCredentialQuotaStateMap } from '@/components/usage/credentials/useCredentialsTabData';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import { cpamcEmbedSearch } from '@/embed/cpamcEmbed';
import shellStyles from '@/features/key-viewer/KeyViewerShell.module.scss';
import usageStyles from '@/pages/UsagePage.module.scss';

const isPageVisible = () => typeof document === 'undefined' || document.visibilityState !== 'hidden';

export interface KeyCredentialsPageProps {
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyCredentialsPage({ apiKey, onNavigate, onAuthRequired }: KeyCredentialsPageProps) {
  const [pageVisible, setPageVisible] = useState(isPageVisible);
  const [timeZone, setTimeZone] = useState<string | undefined>();
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const [detailSelection, setDetailSelection] = useState<CredentialDetailSelection | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const fallbackFocusRef = useRef<HTMLInputElement | null>(null);
  const detailRequestRef = useRef<{ id: string; controller: AbortController } | null>(null);

  useEffect(() => {
    const syncVisibility = () => setPageVisible(isPageVisible());
    syncVisibility();
    if (typeof document === 'undefined') return undefined;
    document.addEventListener('visibilitychange', syncVisibility);
    return () => document.removeEventListener('visibilitychange', syncVisibility);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void fetchKeyStatus(controller.signal)
      .then((res) => {
        if (!controller.signal.aborted && res.timezone) {
          setTimeZone(res.timezone);
        }
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof ApiError && error.status === 401) {
          onAuthRequired?.();
        }
      });
    return () => controller.abort();
  }, [onAuthRequired]);

  const credentialPages = useCredentialPages({
    viewer: 'key',
    enabledCredentials: pageVisible,
    enabledAiProviders: false,
    onAuthRequired,
  });

  useEffect(() => {
    const search = buildCredentialProviderSearch(cpamcEmbedSearch(), credentialPages.credentialProviderFilter);
    const href = appPath('/key-credentials') + search + window.location.hash;
    if (href !== window.location.pathname + window.location.search + window.location.hash) {
      window.history.replaceState(null, '', href);
    }
  }, [credentialPages.credentialProviderFilter]);

  const currentAuthIndexes = useMemo(
    () => selectQuotaEligibleAuthIndexes(credentialPages.credentialIdentities),
    [credentialPages.credentialIdentities],
  );

  const {
    quotaResponseByAuthIndex,
    quotaStateByAuthIndex,
    applyRefreshUpdates,
    refreshQuotaCache,
  } = useQuotaCache({
    viewer: 'key',
    enabled: pageVisible,
    authIndexes: currentAuthIndexes,
    onAuthRequired,
  });

  const quotaRefreshTasks = useQuotaRefreshTasks({
    viewer: 'key',
    enabled: pageVisible,
    currentAuthIndexes,
    quotaStateByAuthIndex,
    applyRefreshUpdates,
    onAuthRequired,
  });

  const quotaResponsesByAuthIndex = useMemo(
    () => new Map(Object.entries(quotaResponseByAuthIndex)),
    [quotaResponseByAuthIndex],
  );
  const quotaStates = useMemo(
    () => buildCredentialQuotaStateMap(quotaStateByAuthIndex),
    [quotaStateByAuthIndex],
  );
  const credentialRows = useMemo(
    () => buildCredentialRows(credentialPages.credentialIdentities, quotaResponsesByAuthIndex, quotaStates),
    [credentialPages.credentialIdentities, quotaResponsesByAuthIndex, quotaStates],
  );

  const detailId = detailSelection?.row.identity.id;
  const refreshCredentialDetail = useCallback(async () => {
    if (!detailOpen || !detailId) return;
    detailRequestRef.current?.controller.abort();
    const request = { id: detailId, controller: new AbortController() };
    detailRequestRef.current = request;
    try {
      const updated = await fetchKeyCredential(request.id, request.controller.signal);
      if (detailRequestRef.current !== request) return;
      setDetailSelection((current) => current?.row.identity.id === request.id ? updateCredentialDetailStats(current, updated) : current);
    } catch (error) {
      if (detailRequestRef.current !== request) return;
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    } finally {
      if (detailRequestRef.current === request) {
        detailRequestRef.current = null;
      }
    }
  }, [detailId, detailOpen, onAuthRequired]);

  useEffect(() => {
    if (!detailOpen) return;
    void refreshCredentialDetail();
    const interval = window.setInterval(() => {
      void refreshCredentialDetail();
    }, CREDENTIAL_PAGES_REFRESH_INTERVAL_MS);
    return () => {
      window.clearInterval(interval);
      detailRequestRef.current?.controller.abort();
      detailRequestRef.current = null;
    };
  }, [detailOpen, refreshCredentialDetail]);

  const currentDetailSelection = useMemo<CredentialDetailSelection | null>(() => {
    if (!detailSelection) return null;
    const id = detailSelection.row.identity.id;
    const row = credentialRows.find((item) => item.identity.id === id);
    return row
      ? updateCredentialDetailStats({ kind: 'credential', row }, { ...detailSelection.row.identity, priority: row.identity.priority })
      : detailSelection;
  }, [credentialRows, detailSelection]);

  const handleManualRefresh = useCallback(async () => {
    if (manualRefreshLoading) return;
    setManualRefreshLoading(true);
    try {
      await Promise.all([credentialPages.refresh(), refreshQuotaCache(), refreshCredentialDetail()]);
    } finally {
      setManualRefreshLoading(false);
    }
  }, [credentialPages, manualRefreshLoading, refreshCredentialDetail, refreshQuotaCache]);

  return (
    <KeyViewerShell
      activePage="credentials"
      apiKey={apiKey}
      onRefresh={() => void handleManualRefresh()}
      refreshing={manualRefreshLoading}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
    >
      {credentialPages.error && (
        <div className={shellStyles.errorBox} role="alert">{credentialPages.error}</div>
      )}

      <CredentialProviderFilterBar
        scope="credentials"
        typeCounts={credentialPages.credentialTypeCounts}
        value={credentialPages.credentialProviderFilter}
        onChange={credentialPages.setCredentialProviderFilter}
      />

      <div className={usageStyles.credentialsSections}>
        <CredentialsSection
          rows={credentialRows}
          timeZone={timeZone}
          total={credentialPages.credentialTotal}
          page={credentialPages.credentialPage}
          totalPages={credentialPages.credentialTotalPages}
          pageSize={credentialPages.credentialPageSize}
          activeOnly={credentialPages.credentialActiveOnly}
          sort={credentialPages.credentialSort}
          loading={credentialPages.loading}
          viewMode={credentialPages.credentialViewMode}
          onViewModeChange={credentialPages.setCredentialViewMode}
          readOnly
          quotaRefreshing={quotaRefreshTasks.quotaRefreshing}
          quotaRefreshError={quotaRefreshTasks.quotaRefreshError}
          onPageChange={credentialPages.setCredentialPage}
          onPageSizeChange={credentialPages.setCredentialPageSize}
          onActiveOnlyChange={credentialPages.setCredentialActiveOnly}
          onSortChange={credentialPages.setCredentialSort}
          onRefreshQuota={quotaRefreshTasks.refreshQuotaForCurrentCredentialPage}
          onRefreshQuotaForAuthIndex={quotaRefreshTasks.refreshQuotaForAuthIndex}
          editFallbackRef={fallbackFocusRef}
          onOpenDetails={(row) => {
            setDetailSelection({ kind: 'credential', row });
            setDetailOpen(true);
          }}
        />
      </div>

      <CredentialDetailDrawer
        open={detailOpen}
        viewer="key"
        selection={currentDetailSelection}
        timeZone={timeZone}
        fallbackFocusRef={fallbackFocusRef}
        onAuthRequired={onAuthRequired}
        requestLogAccessEnabled={false}
        onClose={() => setDetailOpen(false)}
      />
    </KeyViewerShell>
  );
}
