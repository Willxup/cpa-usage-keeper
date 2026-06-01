import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Select } from '@/components/ui/Select';
import { IconCheck, IconChevronDown } from '@/components/ui/icons';
import type { UsageEvent, UsageSourceFilterOption } from '@/lib/types';
import {
  calculateCacheRate,
  formatDurationMs,
  formatUsd,
  LATENCY_SOURCE_FIELD,
  normalizeAuthIndex,
} from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';

const ALL_FILTER = '__all__';

type SelectOption = { value: string; label: string };

export const REQUEST_EVENT_COLUMN_IDS = [
  'timestamp',
  'api_key',
  'source',
  'model',
  'reasoning_effort',
  'result',
  'ttft',
  'latency',
  'request_type',
  'endpoint',
  'input_tokens',
  'output_tokens',
  'reasoning_tokens',
  'cached_tokens',
  'cache_rate',
  'total_tokens',
  'total_cost',
] as const;

export type RequestEventColumnId = typeof REQUEST_EVENT_COLUMN_IDS[number];

const REQUEST_EVENT_COLUMN_ID_SET: ReadonlySet<string> = new Set(REQUEST_EVENT_COLUMN_IDS);

export const normalizeRequestEventVisibleColumnIds = (
  columnIds: readonly RequestEventColumnId[],
  availableColumnIds: readonly RequestEventColumnId[] = REQUEST_EVENT_COLUMN_IDS
): RequestEventColumnId[] => {
  const availableSet = new Set<RequestEventColumnId>(availableColumnIds);
  const seen = new Set<RequestEventColumnId>();
  const normalized = columnIds.filter((columnId) => {
    if (!REQUEST_EVENT_COLUMN_ID_SET.has(columnId) || !availableSet.has(columnId) || seen.has(columnId)) {
      return false;
    }
    seen.add(columnId);
    return true;
  });

  return normalized.length > 0 ? normalized : [...availableColumnIds];
};

export const toggleRequestEventColumnId = (
  columnIds: readonly RequestEventColumnId[],
  columnId: RequestEventColumnId,
  availableColumnIds: readonly RequestEventColumnId[] = REQUEST_EVENT_COLUMN_IDS
): RequestEventColumnId[] => {
  const normalized = normalizeRequestEventVisibleColumnIds(columnIds, availableColumnIds);
  if (!availableColumnIds.includes(columnId)) {
    return normalized;
  }
  if (normalized.includes(columnId)) {
    return normalized.length <= 1 ? normalized : normalized.filter((currentColumnId) => currentColumnId !== columnId);
  }
  return availableColumnIds.filter((currentColumnId) => normalized.includes(currentColumnId) || currentColumnId === columnId);
};

const appendSelectedOption = (
  options: SelectOption[],
  selectedValue: string,
  selectedLabel = selectedValue
) => {
  if (selectedValue === ALL_FILTER || options.some((option) => option.value === selectedValue)) {
    return options;
  }
  return [...options, { value: selectedValue, label: selectedLabel }];
};

type RequestEventRow = {
  id: string;
  timestamp: string;
  timestampMs: number;
  timestampLabel: string;
  apiKey: string;
  model: string;
  reasoningEffort: string;
  requestType: string;
  endpoint: string;
  sourceRaw: string;
  source: string;
  sourceType: string;
  authIndex: string;
  isDelete: boolean;
  failed: boolean;
  latencyMs: number | null;
  ttftMs: number | null;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cachedTokens: number;
  totalTokens: number;
  cacheRate: string;
  cost: number | null;
  costAvailable: boolean;
};

type RequestEventColumnDefinition = {
  id: RequestEventColumnId;
  label: string;
  header: ReactNode;
  renderCell: (row: RequestEventRow) => ReactNode;
};

export interface RequestEventsDetailsCardProps {
  events: UsageEvent[];
  loading: boolean;
  page: number;
  pageSize: number;
  pageSizeOptions: readonly number[];
  totalCount: number;
  totalPages: number;
  modelOptions: string[];
  sourceOptions: UsageSourceFilterOption[];
  modelFilter: string;
  sourceFilter: string;
  resultFilter: string;
  initialVisibleColumnIds?: readonly RequestEventColumnId[];
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onModelFilterChange: (model: string) => void;
  onSourceFilterChange: (source: string) => void;
  onResultFilterChange: (result: string) => void;
}

const toNumber = (value: unknown): number => {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return 0;
  return parsed;
};

const formatRequestEventTimestamp = (timestamp: string): string => {
  const match = timestamp.match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})/);
  if (!match) return timestamp || '-';
  return `${match[1]}/${match[2]}/${match[3]} ${match[4]}:${match[5]}:${match[6]}`;
};

const formatCacheRate = (cachedTokens: number, inputTokens: number): string => {
  const rate = calculateCacheRate({ inputTokens, cachedTokens });
  return rate === null ? '-' : `${rate.toFixed(2)}%`;
};

const formatTTFTMs = (ttftMs: number | null): string => {
  if (ttftMs === null || ttftMs <= 0) {
    return '-';
  }
  return formatDurationMs(ttftMs);
};

const parseRequestEndpoint = (rawEndpoint: unknown): { requestType: string; endpoint: string } => {
  const raw = String(rawEndpoint ?? '').trim().replace(/\s+/g, ' ');
  if (!raw) {
    return { requestType: '-', endpoint: '-' };
  }
  const [first, ...rest] = raw.split(' ');
  const upperMethod = first.toUpperCase();
  const hasMethod = ['GET', 'POST'].includes(upperMethod);
  const requestType = upperMethod === 'POST' ? 'SSE' : upperMethod === 'GET' ? 'WS' : '-';
  const path = hasMethod ? rest.join(' ').trim() : raw;
  const normalizedPath = path.startsWith('/v1/') ? path.slice(3) : path === '/v1' ? '/' : path;
  return { requestType, endpoint: normalizedPath || '-' };
};

type RequestEventColumnOption = {
  id: RequestEventColumnId;
  label: string;
};

const COLUMN_DROPDOWN_VIEWPORT_MARGIN = 8;
const COLUMN_DROPDOWN_OFFSET = 6;
const COLUMN_DROPDOWN_MAX_HEIGHT = 300;
const COLUMN_DROPDOWN_MIN_WIDTH = 190;
const COLUMN_DROPDOWN_Z_INDEX = 2010;

const clampDropdownPosition = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

type RequestEventColumnMenuNavigationKey = 'ArrowDown' | 'ArrowUp' | 'Home' | 'End' | 'Tab' | 'Escape';

export const resolveRequestEventColumnMenuFocusIndex = (
  currentIndex: number,
  optionCount: number,
  key: RequestEventColumnMenuNavigationKey,
  shiftKey = false
): number | null => {
  if (optionCount <= 0 || key === 'Escape') {
    return null;
  }

  const safeCurrentIndex = currentIndex >= 0 && currentIndex < optionCount ? currentIndex : 0;
  if (key === 'Home') return 0;
  if (key === 'End') return optionCount - 1;
  if (key === 'ArrowDown') return (safeCurrentIndex + 1) % optionCount;
  if (key === 'ArrowUp') return (safeCurrentIndex - 1 + optionCount) % optionCount;
  if (key === 'Tab') {
    return shiftKey
      ? (safeCurrentIndex - 1 + optionCount) % optionCount
      : (safeCurrentIndex + 1) % optionCount;
  }

  return null;
};

const resolveColumnDropdownStyle = (element: HTMLElement): CSSProperties => {
  const rect = element.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const availableWidth = Math.max(0, viewportWidth - COLUMN_DROPDOWN_VIEWPORT_MARGIN * 2);
  const width = Math.min(Math.max(rect.width, COLUMN_DROPDOWN_MIN_WIDTH), availableWidth);
  const left = clampDropdownPosition(
    rect.left - (width - rect.width) / 2,
    COLUMN_DROPDOWN_VIEWPORT_MARGIN,
    Math.max(COLUMN_DROPDOWN_VIEWPORT_MARGIN, viewportWidth - width - COLUMN_DROPDOWN_VIEWPORT_MARGIN)
  );
  const spaceBelow = viewportHeight - rect.bottom - COLUMN_DROPDOWN_VIEWPORT_MARGIN - COLUMN_DROPDOWN_OFFSET;
  const spaceAbove = rect.top - COLUMN_DROPDOWN_VIEWPORT_MARGIN - COLUMN_DROPDOWN_OFFSET;
  const direction = spaceBelow >= COLUMN_DROPDOWN_MAX_HEIGHT || spaceBelow >= spaceAbove ? 'down' : 'up';
  const maxHeight = Math.max(
    0,
    Math.min(COLUMN_DROPDOWN_MAX_HEIGHT, direction === 'down' ? spaceBelow : spaceAbove)
  );

  return direction === 'down'
    ? {
        position: 'fixed',
        top: rect.bottom + COLUMN_DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: COLUMN_DROPDOWN_Z_INDEX,
      }
    : {
        position: 'fixed',
        bottom: viewportHeight - rect.top + COLUMN_DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: COLUMN_DROPDOWN_Z_INDEX,
      };
};

function RequestEventsColumnSelector({
  label,
  summary,
  ariaLabel,
  options,
  selectedIds,
  onToggle,
}: {
  label: string;
  summary: string;
  ariaLabel: string;
  options: RequestEventColumnOption[];
  selectedIds: readonly RequestEventColumnId[];
  onToggle: (columnId: RequestEventColumnId) => void;
}) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const rafRef = useRef<number | null>(null);
  const [dropdownStyle, setDropdownStyle] = useState<CSSProperties | null>(null);
  const selectedIdSet = useMemo(() => new Set<RequestEventColumnId>(selectedIds), [selectedIds]);

  useEffect(() => {
    if (!open) return;
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [open]);

  useEffect(() => {
    if (!open || !dropdownStyle) return;
    const firstOption = dropdownRef.current?.querySelector<HTMLButtonElement>('button');
    firstOption?.focus();
  }, [dropdownStyle, open]);

  const updateDropdownStyle = useCallback(() => {
    if (!wrapRef.current) return;
    setDropdownStyle(resolveColumnDropdownStyle(wrapRef.current));
  }, []);

  const scheduleDropdownStyleUpdate = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
    }
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = null;
      updateDropdownStyle();
    });
  }, [updateDropdownStyle]);

  useLayoutEffect(() => {
    if (!open) {
      if (rafRef.current !== null && typeof window !== 'undefined') {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }

    updateDropdownStyle();
    window.addEventListener('resize', scheduleDropdownStyleUpdate);
    window.addEventListener('scroll', scheduleDropdownStyleUpdate, true);

    return () => {
      window.removeEventListener('resize', scheduleDropdownStyleUpdate);
      window.removeEventListener('scroll', scheduleDropdownStyleUpdate, true);
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [open, scheduleDropdownStyleUpdate, updateDropdownStyle]);

  const handleTriggerKeyDown = useCallback((event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    setOpen(true);
  }, []);

  const handleDropdownKeyDown = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
      return;
    }

    if (
      event.key !== 'ArrowDown' &&
      event.key !== 'ArrowUp' &&
      event.key !== 'Home' &&
      event.key !== 'End' &&
      event.key !== 'Tab'
    ) {
      return;
    }

    const optionButtons = Array.from(dropdownRef.current?.querySelectorAll<HTMLButtonElement>('button') ?? []);
    const currentIndex = optionButtons.findIndex((button) => button === document.activeElement);
    const nextIndex = resolveRequestEventColumnMenuFocusIndex(
      currentIndex,
      optionButtons.length,
      event.key,
      event.shiftKey
    );
    if (nextIndex === null) return;
    event.preventDefault();
    optionButtons[nextIndex]?.focus();
  }, []);

  const dropdown = open && dropdownStyle
    ? (
        <div
          ref={dropdownRef}
          className={styles.requestEventsColumnDropdown}
          role="menu"
          aria-label={ariaLabel}
          style={dropdownStyle}
          onKeyDown={handleDropdownKeyDown}
        >
          {options.map((option) => {
            const selected = selectedIdSet.has(option.id);
            return (
              <button
                key={option.id}
                type="button"
                role="menuitemcheckbox"
                aria-checked={selected}
                className={`${styles.requestEventsColumnOption} ${selected ? styles.requestEventsColumnOptionSelected : ''}`.trim()}
                onClick={() => onToggle(option.id)}
              >
                <span className={styles.requestEventsColumnOptionLabel}>{option.label}</span>
                {selected ? (
                  <span className={styles.requestEventsColumnCheck} aria-hidden="true">
                    <IconCheck size={12} />
                  </span>
                ) : (
                  <span className={styles.requestEventsColumnCheckPlaceholder} aria-hidden="true" />
                )}
              </button>
            );
          })}
        </div>
      )
    : null;

  return (
    <div className={styles.requestEventsPageSizeControl}>
      <span>{label}</span>
      <div className={styles.requestEventsColumnPicker} ref={wrapRef}>
        <button
          ref={triggerRef}
          type="button"
          className={styles.requestEventsColumnTrigger}
          aria-haspopup="menu"
          aria-expanded={open}
          aria-label={ariaLabel}
          onClick={() => setOpen((currentOpen) => !currentOpen)}
          onKeyDown={handleTriggerKeyDown}
        >
          <span>{summary}</span>
          <span className={styles.requestEventsColumnTriggerIcon} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>
      </div>
      {dropdown && (typeof document === 'undefined' ? dropdown : createPortal(dropdown, document.body))}
    </div>
  );
}

function RequestEventsTitle({ title, subtitle, eyebrow, totalLabel }: { title: string; subtitle: string; eyebrow: string; totalLabel: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <span className={styles.sectionEyebrow}>{eyebrow}</span>
      <div className={styles.requestEventsTitleRow}>
        <h3 className={styles.sectionTitle}>{title}</h3>
        <span className={styles.requestEventsCountBadge}>{totalLabel}</span>
      </div>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export function RequestEventsDetailsCard({
  events,
  loading,
  page,
  pageSize,
  pageSizeOptions,
  totalCount,
  totalPages,
  modelOptions: backendModelOptions,
  sourceOptions: backendSourceOptions,
  modelFilter,
  sourceFilter,
  resultFilter,
  initialVisibleColumnIds,
  onPageChange,
  onPageSizeChange,
  onModelFilterChange,
  onSourceFilterChange,
  onResultFilterChange,
}: RequestEventsDetailsCardProps) {
  const { t } = useTranslation();
  const latencyHint = t('usage_stats.latency_unit_hint', {
    field: LATENCY_SOURCE_FIELD,
    unit: t('usage_stats.duration_unit_ms'),
  });
  const ttftHint = t('usage_stats.ttft_hint');

  const rows = useMemo<RequestEventRow[]>(() => {
    return events.map((event, index) => {
      const timestamp = event.timestamp;
      const timestampMs = Date.parse(timestamp);
      const sourceRaw = String(event.source_raw ?? '').trim() || String(event.source ?? '').trim();
      const authIndexRaw = event.auth_index as unknown;
      const authIndex =
        authIndexRaw === null || authIndexRaw === undefined || authIndexRaw === ''
          ? '-'
          : normalizeAuthIndex(authIndexRaw) || '-';
      const source = String(event.source ?? '').trim() || '-';
      const sourceType = String(event.source_type ?? '').trim();
      const apiKey = String(event.api_key ?? '').trim() || '-';
      const model = String(event.model ?? '').trim() || '-';
      const reasoningEffort = String(event.reasoning_effort ?? '').trim() || '-';
      const endpointFields = parseRequestEndpoint(event.endpoint);
      const inputTokens = Math.max(toNumber(event.tokens?.input_tokens), 0);
      const outputTokens = Math.max(toNumber(event.tokens?.output_tokens), 0);
      const reasoningTokens = Math.max(toNumber(event.tokens?.reasoning_tokens), 0);
      const cachedTokens = Math.max(toNumber(event.tokens?.cached_tokens), 0);
      const totalTokens = Math.max(toNumber(event.tokens?.total_tokens), 0);
      const latencyMs = Number.isFinite(event.latency_ms) ? event.latency_ms : null;
      const ttftMs = Number.isFinite(event.ttft_ms) ? event.ttft_ms as number : null;
      // 费用由后端按当前价格配置运行时计算，前端只负责展示可用/不可用状态。
      const costAvailable = event.cost_available === true;
      const cost = costAvailable ? Math.max(toNumber(event.cost_usd), 0) : null;

      return {
        id: event.id ? String(event.id) : `${timestamp}-${model}-${sourceRaw || source}-${authIndex}-${index}`,
        timestamp,
        timestampMs: Number.isNaN(timestampMs) ? 0 : timestampMs,
        timestampLabel: formatRequestEventTimestamp(timestamp),
        apiKey,
        model,
        reasoningEffort,
        requestType: endpointFields.requestType,
        endpoint: endpointFields.endpoint,
        sourceRaw: sourceRaw || '-',
        source,
        sourceType,
        authIndex,
        isDelete: event.isDelete === true,
        failed: event.failed === true,
        latencyMs,
        ttftMs,
        inputTokens,
        outputTokens,
        reasoningTokens,
        cachedTokens,
        totalTokens,
        cacheRate: formatCacheRate(cachedTokens, inputTokens),
        cost,
        costAvailable,
      };
    });
  }, [events]);

  const hasLatencyData = useMemo(() => rows.some((row) => row.latencyMs !== null), [rows]);
  const [visibleColumnIds, setVisibleColumnIds] = useState<RequestEventColumnId[]>(() => (
    normalizeRequestEventVisibleColumnIds(initialVisibleColumnIds ?? REQUEST_EVENT_COLUMN_IDS)
  ));

  const availableColumnIds = useMemo(
    () => REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'latency' || hasLatencyData),
    [hasLatencyData]
  );
  const effectiveVisibleColumnIds = useMemo(
    () => normalizeRequestEventVisibleColumnIds(visibleColumnIds, availableColumnIds),
    [availableColumnIds, visibleColumnIds]
  );
  const effectiveVisibleColumnIdSet = useMemo(
    () => new Set<RequestEventColumnId>(effectiveVisibleColumnIds),
    [effectiveVisibleColumnIds]
  );
  const handleColumnToggle = useCallback((columnId: RequestEventColumnId) => {
    setVisibleColumnIds((currentColumnIds) => toggleRequestEventColumnId(currentColumnIds, columnId, availableColumnIds));
  }, [availableColumnIds]);

  const modelOptions = useMemo(() => {
    const options = [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...backendModelOptions.map((model) => ({ value: model, label: model })),
    ];
    return appendSelectedOption(options, modelFilter);
  }, [backendModelOptions, modelFilter, t]);

  const sourceOptions = useMemo(() => {
    const options = [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...backendSourceOptions.map((source) => ({ value: source.value, label: source.displayName || source.label || source.value })),
    ];
    const selectedSource = backendSourceOptions.find((source) => source.value === sourceFilter);
    const selectedLabel = selectedSource?.displayName || selectedSource?.label;
    return appendSelectedOption(options, sourceFilter, selectedLabel || sourceFilter);
  }, [backendSourceOptions, sourceFilter, t]);

  const resultOptions = useMemo(
    () => [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      { value: 'success', label: t('usage_stats.success') },
      { value: 'failed', label: t('usage_stats.failure') },
    ],
    [t]
  );

  const modelOptionSet = useMemo(
    () => new Set(modelOptions.map((option) => option.value)),
    [modelOptions]
  );
  const sourceOptionSet = useMemo(
    () => new Set(sourceOptions.map((option) => option.value)),
    [sourceOptions]
  );
  const resultOptionSet = useMemo(
    () => new Set(resultOptions.map((option) => option.value)),
    [resultOptions]
  );

  const effectiveModelFilter = modelOptionSet.has(modelFilter) ? modelFilter : ALL_FILTER;
  const effectiveSourceFilter = sourceOptionSet.has(sourceFilter) ? sourceFilter : ALL_FILTER;
  const effectiveResultFilter = resultOptionSet.has(resultFilter) ? resultFilter : ALL_FILTER;

  const columnDefinitions = useMemo<RequestEventColumnDefinition[]>(() => {
    const definitions: RequestEventColumnDefinition[] = [
      {
        id: 'timestamp',
        label: t('usage_stats.request_events_timestamp'),
        header: <th>{t('usage_stats.request_events_timestamp')}</th>,
        renderCell: (row) => (
          <td title={row.timestamp} className={styles.requestEventsTimestamp}>
            {row.timestampLabel}
          </td>
        ),
      },
      {
        id: 'api_key',
        label: t('usage_stats.api_key_filter'),
        header: <th>{t('usage_stats.api_key_filter')}</th>,
        renderCell: (row) => <td className={styles.requestEventsAPIKeyCell} title={row.apiKey}>{row.apiKey}</td>,
      },
      {
        id: 'source',
        label: t('usage_stats.request_events_source'),
        header: <th>{t('usage_stats.request_events_source')}</th>,
        renderCell: (row) => (
          <td className={styles.requestEventsSourceCell} title={row.source}>
            <span className={styles.requestEventsSourceStack}>
              <span className={styles.requestEventsSourceValue}>{row.source}</span>
              {(row.isDelete || row.sourceType) && (
                <span className={styles.requestEventsSourceTags}>
                  {row.sourceType && (
                    <span className={styles.credentialType}>{row.sourceType}</span>
                  )}
                  {row.isDelete && (
                    <span className={styles.requestEventsDeletedTag}>{t('usage_stats.deleted')}</span>
                  )}
                </span>
              )}
            </span>
          </td>
        ),
      },
      {
        id: 'model',
        label: t('usage_stats.model_name'),
        header: <th>{t('usage_stats.model_name')}</th>,
        renderCell: (row) => <td className={styles.modelCell}>{row.model}</td>,
      },
      {
        id: 'reasoning_effort',
        label: t('usage_stats.reasoning_effort'),
        header: <th title={t('usage_stats.reasoning_effort_hint')}>{t('usage_stats.reasoning_effort')}</th>,
        renderCell: (row) => <td>{row.reasoningEffort}</td>,
      },
      {
        id: 'result',
        label: t('usage_stats.request_events_result'),
        header: <th>{t('usage_stats.request_events_result')}</th>,
        renderCell: (row) => (
          <td>
            <span
              className={
                row.failed
                  ? styles.requestEventsResultFailed
                  : styles.requestEventsResultSuccess
              }
            >
              {row.failed ? t('usage_stats.failure') : t('usage_stats.success')}
            </span>
          </td>
        ),
      },
      {
        id: 'ttft',
        label: t('usage_stats.ttft'),
        header: <th title={ttftHint}>{t('usage_stats.ttft')}</th>,
        renderCell: (row) => <td className={styles.durationCell}>{formatTTFTMs(row.ttftMs)}</td>,
      },
      {
        id: 'latency',
        label: t('usage_stats.latency'),
        header: <th title={latencyHint}>{t('usage_stats.latency')}</th>,
        renderCell: (row) => <td className={styles.durationCell}>{formatDurationMs(row.latencyMs)}</td>,
      },
      {
        id: 'request_type',
        label: t('usage_stats.request_type'),
        header: <th>{t('usage_stats.request_type')}</th>,
        renderCell: (row) => <td>{row.requestType}</td>,
      },
      {
        id: 'endpoint',
        label: t('usage_stats.request_endpoint'),
        header: <th>{t('usage_stats.request_endpoint')}</th>,
        renderCell: (row) => <td className={styles.requestEventsEndpointCell} title={row.endpoint}>{row.endpoint}</td>,
      },
      {
        id: 'input_tokens',
        label: t('usage_stats.input_tokens'),
        header: <th>{t('usage_stats.input_tokens')}</th>,
        renderCell: (row) => <td>{row.inputTokens.toLocaleString()}</td>,
      },
      {
        id: 'output_tokens',
        label: t('usage_stats.output_tokens'),
        header: <th>{t('usage_stats.output_tokens')}</th>,
        renderCell: (row) => <td>{row.outputTokens.toLocaleString()}</td>,
      },
      {
        id: 'reasoning_tokens',
        label: t('usage_stats.reasoning_tokens'),
        header: <th className={styles.requestEventsReasoningHeader}>{t('usage_stats.reasoning_tokens')}</th>,
        renderCell: (row) => <td>{row.reasoningTokens.toLocaleString()}</td>,
      },
      {
        id: 'cached_tokens',
        label: t('usage_stats.cached_tokens'),
        header: <th>{t('usage_stats.cached_tokens')}</th>,
        renderCell: (row) => <td>{row.cachedTokens.toLocaleString()}</td>,
      },
      {
        id: 'cache_rate',
        label: t('usage_stats.cache_rate'),
        header: <th>{t('usage_stats.cache_rate')}</th>,
        renderCell: (row) => <td>{row.cacheRate}</td>,
      },
      {
        id: 'total_tokens',
        label: t('usage_stats.total_tokens'),
        header: <th>{t('usage_stats.total_tokens')}</th>,
        renderCell: (row) => <td>{row.totalTokens.toLocaleString()}</td>,
      },
      {
        id: 'total_cost',
        label: t('usage_stats.total_cost'),
        header: <th>{t('usage_stats.total_cost')}</th>,
        renderCell: (row) => (
          <td title={row.costAvailable ? undefined : t('usage_stats.cost_need_price')}>
            {row.costAvailable && row.cost !== null ? formatUsd(row.cost) : '-'}
          </td>
        ),
      },
    ];

    return definitions.filter((definition) => definition.id !== 'latency' || hasLatencyData);
  }, [hasLatencyData, latencyHint, t, ttftHint]);

  const visibleColumns = useMemo(
    () => columnDefinitions.filter((definition) => effectiveVisibleColumnIdSet.has(definition.id)),
    [columnDefinitions, effectiveVisibleColumnIdSet]
  );
  const columnOptions = useMemo(
    () => columnDefinitions.map((definition) => ({ id: definition.id, label: definition.label })),
    [columnDefinitions]
  );
  const visibleColumnSummary = effectiveVisibleColumnIds.length === availableColumnIds.length
    ? t('usage_stats.request_events_columns_all')
    : t('usage_stats.request_events_columns_count', {
        selected: effectiveVisibleColumnIds.length,
        total: availableColumnIds.length,
      });

  const hasActiveFilters =
    modelFilter !== ALL_FILTER ||
    sourceFilter !== ALL_FILTER ||
    resultFilter !== ALL_FILTER;

  const computedTotalPages = pageSize > 0 ? Math.ceil(totalCount / pageSize) : 0;
  const safeTotalPages = Math.max(totalPages, computedTotalPages, rows.length > 0 ? 1 : 0);
  const safePage = safeTotalPages > 0 ? Math.min(Math.max(page, 1), safeTotalPages) : 0;
  const pageLabel = safeTotalPages > 0 ? `${safePage} / ${safeTotalPages}` : t('usage_stats.request_events_page_empty');

  const handleClearFilters = () => {
    onModelFilterChange(ALL_FILTER);
    onSourceFilterChange(ALL_FILTER);
    onResultFilterChange(ALL_FILTER);
  };

  return (
    <Card
      className={styles.requestEventsCard}
      title={
        <RequestEventsTitle
          eyebrow={t('usage_stats.request_events_eyebrow')}
          title={t('usage_stats.request_events_title')}
          subtitle={t('usage_stats.request_events_subtitle')}
          totalLabel={t('usage_stats.request_events_total_count', { count: totalCount })}
        />
      }
      extra={
        <div className={styles.requestEventsActions}>
          <Button
            variant="ghost"
            size="sm"
            className={styles.usagePillAction}
            onClick={handleClearFilters}
            disabled={!hasActiveFilters}
          >
            {t('usage_stats.clear_filters')}
          </Button>
        </div>
      }
    >
      <div className={styles.requestEventsToolbar}>
        <div className={styles.requestEventsFiltersGroup}>
          <label className={styles.requestEventsFilterItem}>
            <span className={styles.requestEventsFilterLabel}>
              {t('usage_stats.request_events_filter_model')}
            </span>
            <Select
              value={effectiveModelFilter}
              options={modelOptions}
              onChange={onModelFilterChange}
              className={`${styles.requestEventsSelect} ${styles.usagePillControl}`}
              ariaLabel={t('usage_stats.request_events_filter_model')}
              fullWidth={false}
            />
          </label>
          <label className={styles.requestEventsFilterItem}>
            <span className={styles.requestEventsFilterLabel}>
              {t('usage_stats.request_events_filter_source')}
            </span>
            <Select
              value={effectiveSourceFilter}
              options={sourceOptions}
              onChange={onSourceFilterChange}
              className={`${styles.requestEventsSelect} ${styles.usagePillControl}`}
              ariaLabel={t('usage_stats.request_events_filter_source')}
              fullWidth={false}
            />
          </label>
          <label className={styles.requestEventsFilterItem}>
            <span className={styles.requestEventsFilterLabel}>
              {t('usage_stats.request_events_filter_result')}
            </span>
            <Select
              value={effectiveResultFilter}
              options={resultOptions}
              onChange={onResultFilterChange}
              className={`${styles.requestEventsResultSelect} ${styles.usagePillControl}`}
              ariaLabel={t('usage_stats.request_events_filter_result')}
              fullWidth={false}
            />
          </label>
        </div>
      </div>

      {loading && rows.length === 0 ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : rows.length === 0 ? (
        <EmptyState
          title={t('usage_stats.request_events_empty_title')}
          description={t('usage_stats.request_events_empty_desc')}
        />
      ) : (
        <>
          <div className={styles.requestEventsTableWrapper}>
            <table className={styles.table}>
              <thead>
                <tr>
                  {visibleColumns.map((column) => (
                    <React.Fragment key={column.id}>{column.header}</React.Fragment>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.id}>
                    {visibleColumns.map((column) => (
                      <React.Fragment key={column.id}>{column.renderCell(row)}</React.Fragment>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className={styles.requestEventsPaginationFooter}>
            <div className={styles.requestEventsPaginationControls}>
              <RequestEventsColumnSelector
                label={t('usage_stats.request_events_columns')}
                summary={visibleColumnSummary}
                ariaLabel={t('usage_stats.request_events_columns')}
                options={columnOptions}
                selectedIds={effectiveVisibleColumnIds}
                onToggle={handleColumnToggle}
              />
              <label className={styles.requestEventsPageSizeControl}>
                <span>{t('usage_stats.request_events_rows_per_page')}</span>
                <select value={pageSize} onChange={(event) => onPageSizeChange(Number(event.target.value))} disabled={loading}>
                  {pageSizeOptions.map((option) => <option key={option} value={option}>{option}</option>)}
                </select>
              </label>
              <button type="button" className={styles.requestEventsPagerButton} onClick={() => onPageChange(page - 1)} disabled={loading || safePage <= 1}>
                {t('usage_stats.request_events_previous_page')}
              </button>
              <span className={styles.requestEventsPaginationPage}>{pageLabel}</span>
              <button type="button" className={styles.requestEventsPagerButton} onClick={() => onPageChange(page + 1)} disabled={loading || safeTotalPages === 0 || safePage >= safeTotalPages}>
                {t('usage_stats.request_events_next_page')}
              </button>
            </div>
          </div>
        </>
      )}
    </Card>
  );
}
