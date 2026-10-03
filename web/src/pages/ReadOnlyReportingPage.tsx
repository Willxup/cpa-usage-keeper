import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { useTranslation } from 'react-i18next';
import {
  ApiError,
  exportUsageEvents,
  fetchReadOnlyReport,
  fetchUsageEvents,
  type UsageEventsExportFormat,
  type UsageIdentityPageSort,
} from '@/lib/api';
import type {
  UsageCustomRange,
  UsageTimeRange,
  UsageEventsResponse,
  UsageIdentitiesPageResponse,
  UsageQuotaCacheResponse,
  UsageSourceFilterOption,
} from '@/lib/types';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import { RequestEventsDetailsCard } from '@/components/usage/RequestEventsDetailsCard';
import { TimeRangeControl } from '@/components/usage/TimeRangeControl';
import { Select } from '@/components/ui/Select';
import { AuthFileCredentialsSection } from '@/components/usage/credentials/AuthFileCredentialsSection';
import { AiProviderCredentialsSection } from '@/components/usage/credentials/AiProviderCredentialsSection';
import { CredentialDetailDrawer } from '@/components/usage/credentials/CredentialDetailDrawer';
import {
  buildAuthFileCredentialRows,
  buildAiProviderCredentialRows,
  type CredentialDetailSelection,
} from '@/components/usage/credentials/credentialViewModels';
import { startQuotaPolling } from '@/lib/quotaPolling';
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery';

interface Props {
  page: 'events' | 'auth-files' | 'ai-provider';
  selectedApiKeyId?: string;
  keyFilter?: ReactNode;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired: () => void;
}
const ALL = '__all__';
const noop = async () => undefined;

export function ReadOnlyReportingPage({
  page,
  selectedApiKeyId,
  keyFilter,
  onNavigate,
  onAuthRequired,
}: Props) {
  const { t } = useTranslation();
  const [range, setRange] = useState<UsageTimeRange>('today');
  const [custom, setCustom] = useState<UsageCustomRange>();
  const [model, setModel] = useState(ALL),
    [source, setSource] = useState(ALL),
    [result, setResult] = useState(ALL);
  const [events, setEvents] = useState<UsageEventsResponse | null>(null),
    [options, setOptions] = useState<{
      models: string[];
      sources: UsageSourceFilterOption[];
    }>({ models: [], sources: [] });
  const [accounts, setAccounts] = useState<UsageIdentitiesPageResponse | null>(
      null,
    ),
    [cache, setCache] = useState<UsageQuotaCacheResponse>({ items: [] });
  const [number, setNumber] = useState(1),
    [size, setSize] = useState(10),
    [active, setActive] = useState(true),
    [sort, setSort] = useState<UsageIdentityPageSort>('priority');
  const [detail, setDetail] = useState<CredentialDetailSelection | null>(null);
  const [provider, setProvider] = useState('');
  const moreController = useRef<AbortController | null>(null);
  const [loading, setLoading] = useState(false),
    [error, setError] = useState(''),
    [revision, setRevision] = useState(0),
    [exporting, setExporting] = useState<UsageEventsExportFormat | null>(null);
  const [nextCursor, setNextCursor] = useState<string>(),
    [loadingMore, setLoadingMore] = useState(false),
    [autoLoadMore, setAutoLoadMore] = useState(true);
  const query = useMemo(
    () =>
      buildUsageRangeQuery({
        range,
        customUnit: custom?.unit,
        customStart: custom?.start,
        customEnd: custom?.end,
      }),
    [range, custom],
  );
  const eventOptions = useMemo(
    () => ({
      readOnly: true,
      apiKeyId: selectedApiKeyId,
      model: model === ALL ? undefined : model,
      source: source === ALL ? undefined : source,
      result: result === ALL ? undefined : result,
      pageSize: 50,
      cursorMode: true,
    }),
    [selectedApiKeyId, model, source, result],
  );
  const fail = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && err.status === 401) onAuthRequired();
      setError(err instanceof Error ? err.message : 'Unable to load report');
    },
    [onAuthRequired],
  );

  useEffect(() => {
    moreController.current?.abort();
    moreController.current = null;
    setLoadingMore(false);
    const controller = new AbortController();
    setLoading(true);
    setError('');
    setEvents(null);
    setAccounts(null);
    setNextCursor(undefined);
    setAutoLoadMore(true);
    const load = async () => {
      try {
        if (page === 'events') {
          const params = new URLSearchParams();
          for (const [key, value] of Object.entries(query))
            if (value !== undefined) params.set(key, String(value));
          if (selectedApiKeyId) params.set('api_key_id', selectedApiKeyId);
          const [data, filters] = await Promise.all([
            fetchUsageEvents(query, controller.signal, eventOptions),
            fetchReadOnlyReport<typeof options>(
              'events/filters',
              params,
              controller.signal,
            ),
          ]);
          if (controller.signal.aborted) return;
          setEvents(data);
          setOptions(filters);
          setNextCursor(data.has_more ? data.next_cursor : undefined);
        } else {
          const params = new URLSearchParams({
            auth_type: page === 'auth-files' ? '1' : '2',
            page: String(number),
            page_size: String(size),
            active_only: String(active),
            sort,
          });
          if (provider) params.append('type', provider);
          const data = await fetchReadOnlyReport<UsageIdentitiesPageResponse>(
            'accounts',
            params,
            controller.signal,
          );
          if (controller.signal.aborted) return;
          setAccounts(data);
        }
      } catch (err) {
        if (!controller.signal.aborted) fail(err);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    };
    void load();
    return () => {
      controller.abort();
      moreController.current?.abort();
    };
  }, [
    page,
    query,
    eventOptions,
    selectedApiKeyId,
    number,
    size,
    active,
    sort,
    provider,
    revision,
    fail,
  ]);

  useEffect(() => {
    if (page !== 'auth-files') return;
    return startQuotaPolling(async (signal) => {
      try {
        const quota = await fetchReadOnlyReport<UsageQuotaCacheResponse>(
          'accounts/quota-cache',
          undefined,
          signal,
        );
        if (!signal.aborted) setCache(quota);
      } catch (err) {
        if (!signal.aborted) {
          if (err instanceof ApiError && err.status === 401) onAuthRequired();
          else setError('Cached quota is unavailable.');
        }
      }
    });
  }, [page, onAuthRequired]);

  const loadMore = async () => {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    const controller = new AbortController();
    moreController.current = controller;
    try {
      const data = await fetchUsageEvents(query, controller.signal, {
        ...eventOptions,
        cursor: nextCursor,
      });
      if (controller.signal.aborted) return;
      setEvents((previous) =>
        previous
          ? { ...data, events: [...previous.events, ...data.events] }
          : data,
      );
      setNextCursor(data.has_more ? data.next_cursor : undefined);
      setAutoLoadMore(true);
    } catch (err) {
      if (!controller.signal.aborted) {
        fail(err);
        setAutoLoadMore(false);
      }
    } finally {
      if (moreController.current === controller) {
        setLoadingMore(false);
        moreController.current = null;
      }
    }
  };
  const doExport = async (format: UsageEventsExportFormat) => {
    setExporting(format);
    setError('');
    try {
      const file = await exportUsageEvents(query, format, eventOptions);
      const href = URL.createObjectURL(file.blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = file.filename;
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(href), 1000);
    } catch (err) {
      fail(err);
    } finally {
      setExporting(null);
    }
  };
  const quotaMap = useMemo(
    () =>
      new Map(
        cache.items
          .filter((item) => item.quota)
          .map((item) => [item.auth_index, item.quota!]),
      ),
    [cache],
  );
  const authRows = useMemo(
    () => buildAuthFileCredentialRows(accounts?.identities ?? [], quotaMap),
    [accounts, quotaMap],
  );
  const providerRows = useMemo(
    () => buildAiProviderCredentialRows(accounts?.identities ?? []),
    [accounts],
  );
  const pagination = {
    reportingOnly: true,
    total: accounts?.total_count ?? 0,
    page: number,
    totalPages: accounts?.total_pages ?? 1,
    pageSize: size,
    activeOnly: active,
    sort,
    loading,
    onPageChange: setNumber,
    onPageSizeChange: (value: number) => {
      setSize(value);
      setNumber(1);
    },
    onActiveOnlyChange: (value: boolean) => {
      setActive(value);
      setNumber(1);
    },
    onSortChange: (value: UsageIdentityPageSort) => {
      setSort(value);
      setNumber(1);
    },
  };
  return (
    <KeyViewerShell
      activePage={page}
      readOnly
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
      onRefresh={() => setRevision((value) => value + 1)}
      refreshing={loading}
      filters={
        page === 'events'
          ? [
              keyFilter,
              <TimeRangeControl
                key="range"
                value={range}
                customRange={custom}
                ariaLabel={t('usage_stats.range_filter')}
                labelInsideTrigger
                onChange={(value, next) => {
                  setRange(value);
                  setCustom(next);
                }}
              />,
            ]
          : [
              <Select
                key="provider"
                value={provider}
                options={[
                  { value: '', label: t('usage_stats.api_key_filter_all') },
                  ...(accounts?.type_counts ?? []).map((item) => ({
                    value: item.type,
                    label: item.type,
                  })),
                ]}
                ariaLabel="Provider"
                onChange={(value) => {
                  setProvider(value);
                  setNumber(1);
                }}
              />,
            ]
      }
    >
      {error && <p role="alert">{error}</p>}
      {page === 'events' && (
        <RequestEventsDetailsCard
          events={events?.events ?? []}
          loading={loading}
          totalCount={events?.total_count ?? 0}
          modelOptions={options.models}
          sourceOptions={options.sources}
          modelFilter={model}
          sourceFilter={source}
          resultFilter={result}
          onModelFilterChange={setModel}
          onSourceFilterChange={setSource}
          onResultFilterChange={setResult}
          onExport={(format) => void doExport(format)}
          exportingFormat={exporting}
          hasMore={Boolean(nextCursor)}
          loadingMore={loadingMore}
          autoLoadMore={autoLoadMore}
          onLoadMore={() => void loadMore()}
          requestLogAccessEnabled={false}
        />
      )}
      {page === 'auth-files' && (
        <AuthFileCredentialsSection
          {...pagination}
          rows={authRows}
          quotaRefreshing={false}
          quotaRefreshError=""
          quotaInspectionStatus={null}
          quotaInspectionLoading={false}
          quotaInspectionStarting={false}
          quotaInspectionError=""
          onRefreshQuota={noop}
          onRefreshQuotaForAuthIndex={noop}
          onResetQuotaForAuthIndex={noop}
          onRefreshInspectionStatus={noop}
          onStartInspection={noop}
          onOpenDetails={(row) => setDetail({ kind: 'auth-file', row })}
        />
      )}
      {page === 'ai-provider' && (
        <AiProviderCredentialsSection
          {...pagination}
          rows={providerRows}
          onOpenDetails={(row) => setDetail({ kind: 'ai-provider', row })}
        />
      )}
      <CredentialDetailDrawer
        readOnly
        open={Boolean(detail)}
        selection={detail}
        onAuthRequired={onAuthRequired}
        onClose={() => setDetail(null)}
      />
    </KeyViewerShell>
  );
}
