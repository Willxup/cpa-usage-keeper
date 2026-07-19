import React, {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  Button as AntButton,
  Card,
  Checkbox,
  Dropdown,
  Empty,
  Form,
  Modal,
  Pagination,
  Select as AntSelect,
  Space,
  Table,
  Tag,
  Tooltip,
  type MenuProps,
  type TableColumnsType,
} from 'antd';
import { DownOutlined, DownloadOutlined, ReloadOutlined, TableOutlined } from '@ant-design/icons';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useTranslation } from 'react-i18next';
import { SectionHeader } from '@/components/layout';
import { IconCheck, IconChevronDown, IconCopy, IconScrollText } from '@/components/ui/icons';
import type { UsageEvent, UsageEventRequestLogResponse, UsageSourceFilterOption } from '@/lib/types';
import {
  calculateCacheReadRate,
  formatDurationMs,
  formatUsd,
  LATENCY_SOURCE_FIELD,
  normalizeAuthIndex,
} from '@/utils/usage';
import styles from './RequestEventsDetailsCard.module.scss';

const ALL_FILTER = '__all__';
const REQUEST_EVENT_ENTITY_FILTER_POPUP_WIDTH = 360;
const REQUEST_LOG_VIRTUAL_LINE_HEIGHT = 18;
const REQUEST_LOG_VIRTUAL_OVERSCAN = 8;
const REQUEST_LOG_VIRTUAL_PADDING_Y = 12;
const REQUEST_LOG_VIRTUAL_CHUNK_CHARS = 2048;
const REQUEST_LOG_VIRTUAL_BREAK_LOOKBACK = 256;
const REQUEST_LOG_GRAPHEME_CONTEXT_CHARS = 64;
const REQUEST_LOG_GRAPHEME_SEGMENTER = typeof Intl !== 'undefined' && typeof Intl.Segmenter === 'function'
  ? new Intl.Segmenter(undefined, { granularity: 'grapheme' })
  : null;

type SelectOption = { value: string; label: string };

export type RequestEventExportFormat = 'csv' | 'json';

export const REQUEST_EVENT_COLUMN_IDS = [
  'timestamp',
  'api_key',
  'source',
  'model',
  'model_alias',
  'reasoning_effort',
  'service_tier',
  'result',
  'request_type',
  'endpoint',
  'ttft',
  'latency',
  'speed',
  'input_tokens',
  'output_tokens',
  'reasoning_tokens',
  'cache_read_tokens',
  'cache_creation_tokens',
  'cache_read_rate',
  'total_tokens',
  'total_cost',
] as const;

export type RequestEventColumnId = typeof REQUEST_EVENT_COLUMN_IDS[number];

export const DEFAULT_REQUEST_EVENT_VISIBLE_COLUMN_IDS: RequestEventColumnId[] = [
  'timestamp',
  'source',
  'model',
  'result',
  'latency',
  'total_tokens',
  'total_cost',
];

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

export const isRequestEventColumnSelectionControlled = (
  visibleColumnIds: readonly RequestEventColumnId[] | undefined,
  onVisibleColumnIdsChange: ((columnIds: RequestEventColumnId[]) => void) | undefined,
) => visibleColumnIds !== undefined && onVisibleColumnIdsChange !== undefined;

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
  event: UsageEvent;
  id: string;
  requestId: string;
  timestamp: string;
  timestampMs: number;
  timestampLabel: string;
  apiKey: string;
  model: string;
  modelAlias: string;
  reasoningEffort: string;
  speedMode: string;
  speedModeRaw: string;
  responseSpeedMode: string;
  responseSpeedModeRaw: string;
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
  speedTPS: number | null;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  totalTokens: number;
  cacheReadRate: string;
  cost: number | null;
  costAvailable: boolean;
};

type RequestEventColumnDefinition = {
  id: RequestEventColumnId;
  label: string;
  title: ReactNode;
  className?: string;
  headerTitle?: string;
  onCell?: (row: RequestEventRow) => React.TdHTMLAttributes<HTMLTableCellElement>;
  render: (row: RequestEventRow) => ReactNode;
};

const REQUEST_LOG_SECTION_TITLE_KEYS: Record<string, string> = {
  'REQUEST INFO': 'usage_stats.request_events_log_section_request_info',
  HEADERS: 'usage_stats.request_events_log_section_headers',
  'API REQUEST': 'usage_stats.request_events_log_section_api_request',
  'API RESPONSE': 'usage_stats.request_events_log_section_api_response',
  'API RESPONSE ERROR': 'usage_stats.request_events_log_section_api_response_error',
  RESPONSE: 'usage_stats.request_events_log_section_response',
  'WEBSOCKET TIMELINE': 'usage_stats.request_events_log_section_websocket_timeline',
  'API WEBSOCKET TIMELINE': 'usage_stats.request_events_log_section_api_websocket_timeline',
  'RAW LOG': 'usage_stats.request_events_log_section_raw_log',
};

const formatRequestLogSectionTitle = (
  title: string,
  translate: (key: string) => string
) => {
  const normalizedTitle = title.trim().toUpperCase();
  const translationKey = REQUEST_LOG_SECTION_TITLE_KEYS[normalizedTitle];
  if (translationKey) {
    return translate(translationKey);
  }
  return title.trim() || translate('usage_stats.request_events_log_section');
};

const isPreferredRequestLogChunkBreak = (character: string) =>
  character === ','
  || character === '}'
  || character === ']'
  || /\s/u.test(character);

const findPreferredRequestLogChunkEnd = (
  content: string,
  start: number,
  idealEnd: number,
) => {
  const minimumEnd = Math.max(
    start + Math.floor((idealEnd - start) * 0.75),
    idealEnd - REQUEST_LOG_VIRTUAL_BREAK_LOOKBACK,
  );
  for (let end = idealEnd; end > minimumEnd; end -= 1) {
    if (isPreferredRequestLogChunkBreak(content[end - 1] ?? '')) {
      return end;
    }
  }
  return idealEnd;
};

const fallbackRequestLogCodePointBoundary = (content: string, start: number, end: number) => {
  if (end <= start) return start;
  const previousCodeUnit = content.charCodeAt(end - 1);
  const nextCodeUnit = content.charCodeAt(end);
  const splitsSurrogatePair = previousCodeUnit >= 0xD800 && previousCodeUnit <= 0xDBFF
    && nextCodeUnit >= 0xDC00 && nextCodeUnit <= 0xDFFF;
  return splitsSurrogatePair ? end - 1 : end;
};

const findRequestLogGraphemeBoundary = (
  content: string,
  start: number,
  candidateEnd: number,
  lineEnd: number,
) => {
  if (candidateEnd >= lineEnd) return lineEnd;
  if (!REQUEST_LOG_GRAPHEME_SEGMENTER) {
    return fallbackRequestLogCodePointBoundary(content, start, candidateEnd);
  }

  // 只分割候选点附近的小窗口，避免对多 MiB ASCII 日志逐字执行字素分析。
  const contextStart = Math.max(start, candidateEnd - REQUEST_LOG_GRAPHEME_CONTEXT_CHARS);
  const contextEnd = Math.min(lineEnd, candidateEnd + REQUEST_LOG_GRAPHEME_CONTEXT_CHARS);
  let safeEnd = contextStart;
  for (const segment of REQUEST_LOG_GRAPHEME_SEGMENTER.segment(content.slice(contextStart, contextEnd))) {
    const boundary = contextStart + segment.index;
    if (boundary > candidateEnd) break;
    if (boundary > start) {
      safeEnd = boundary;
    }
  }
  if (safeEnd > start) return safeEnd;
  return fallbackRequestLogCodePointBoundary(content, start, candidateEnd);
};

export const splitRequestLogVirtualChunks = (
  content: string,
  maxChunkChars = REQUEST_LOG_VIRTUAL_CHUNK_CHARS,
): string[] => {
  if (content === '') return [''];
  const chunkSize = Math.max(2, Math.floor(maxChunkChars));
  const chunks: string[] = [];
  let lineStart = 0;

  while (lineStart <= content.length) {
    const newlineIndex = content.indexOf('\n', lineStart);
    const lineEnd = newlineIndex === -1 ? content.length : newlineIndex;
    if (lineStart === lineEnd) {
      chunks.push('');
    } else {
      let offset = lineStart;
      while (offset < lineEnd) {
        const idealEnd = Math.min(offset + chunkSize, lineEnd);
        const preferredEnd = idealEnd < lineEnd
          ? findPreferredRequestLogChunkEnd(content, offset, idealEnd)
          : lineEnd;
        const end = findRequestLogGraphemeBoundary(content, offset, preferredEnd, lineEnd);
        chunks.push(content.slice(offset, end));
        offset = end;
      }
    }
    if (newlineIndex === -1) break;
    lineStart = newlineIndex + 1;
  }

  return chunks;
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
  exportingFormat?: RequestEventExportFormat | null;
  initialVisibleColumnIds?: readonly RequestEventColumnId[];
  visibleColumnIds?: readonly RequestEventColumnId[];
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onModelFilterChange: (model: string) => void;
  onSourceFilterChange: (source: string) => void;
  onResultFilterChange: (result: string) => void;
  onExport?: (format: RequestEventExportFormat) => void;
  onVisibleColumnIdsChange?: (columnIds: RequestEventColumnId[]) => void;
  requestLogAccessEnabled?: boolean;
  onRequestLogOpen?: (event: UsageEvent) => void;
  requestLogLoadingEventId?: string | null;
  requestLogResponse?: UsageEventRequestLogResponse | null;
  requestLogError?: string;
  onRequestLogClose?: () => void;
  onRequestLogDownload?: (eventId: string) => void;
  requestLogDownloading?: boolean;
  onRefresh?: () => void;
  refreshing?: boolean;
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

const formatCacheReadRate = (cacheReadTokens: number, inputTokens: number): string => {
  const rate = calculateCacheReadRate({ inputTokens, cacheReadTokens });
  return rate === null ? '-' : `${rate.toFixed(2)}%`;
};

const formatTTFTMs = (ttftMs: number | null): string => {
  if (ttftMs === null || ttftMs <= 0) {
    return '-';
  }
  return formatDurationMs(ttftMs);
};

const formatSpeedTPS = (speedTPS: number | null): string => {
  if (speedTPS === null || speedTPS <= 0) {
    return '-';
  }
  return `${speedTPS.toFixed(1)} t/s`;
};

const REQUEST_SPEED_MODE_LABEL_KEYS: Record<string, string> = {
  auto: 'usage_stats.speed_mode_auto',
  default: 'usage_stats.speed_mode_standard',
  standard: 'usage_stats.speed_mode_standard',
  priority: 'usage_stats.speed_mode_fast',
  fast: 'usage_stats.speed_mode_fast',
  flex: 'usage_stats.speed_mode_flex',
};

const formatSpeedMode = (rawMode: unknown, t: (key: string) => string): string => {
  const value = String(rawMode ?? '').trim();
  if (!value) return '-';

  const labelKey = REQUEST_SPEED_MODE_LABEL_KEYS[value.toLowerCase()];
  return labelKey ? t(labelKey) : value;
};

const formatSpeedModeTooltipLine = (
  label: string,
  value: string,
  rawValue: string,
  t: (key: string, options: Record<string, string>) => string,
): string => t(
  rawValue === '-' ? 'usage_stats.speed_mode_tooltip_empty' : 'usage_stats.speed_mode_tooltip_value',
  { label, value, raw: rawValue },
);

const buildSpeedModeTooltipLines = (
  row: RequestEventRow,
  t: (key: string, options?: Record<string, string>) => string,
): string[] => [
  formatSpeedModeTooltipLine(t('usage_stats.speed_mode'), row.speedMode, row.speedModeRaw, t),
  formatSpeedModeTooltipLine(
    t('usage_stats.response_speed_mode'),
    row.responseSpeedMode,
    row.responseSpeedModeRaw,
    t,
  ),
];

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
  const selectedIdSet = useMemo(() => new Set<RequestEventColumnId>(selectedIds), [selectedIds]);
  const menuItems = useMemo<MenuProps['items']>(() => options.map((option) => ({
    key: option.id,
    label: (
      <Checkbox
        checked={selectedIdSet.has(option.id)}
        onChange={() => undefined}
        onClick={(event) => event.preventDefault()}
      >
        {option.label}
      </Checkbox>
    ),
  })), [options, selectedIdSet]);

  return (
    <Space size={8} className={styles.requestEventsColumnControl}>
      <span className={styles.requestEventsControlLabel}>{label}</span>
      <Dropdown
        trigger={['click']}
        menu={{
          items: menuItems,
          onClick: ({ key, domEvent }) => {
            domEvent.preventDefault();
            onToggle(key as RequestEventColumnId);
          },
        }}
      >
        <AntButton
          size="small"
          icon={<TableOutlined />}
          aria-label={ariaLabel}
        >
          {summary}
          <DownOutlined />
        </AntButton>
      </Dropdown>
    </Space>
  );
}

const copyRequestLogSectionContent = async (content: string) => {
  const clipboard = globalThis.navigator?.clipboard;
  if (clipboard) {
    try {
      await clipboard.writeText(content);
      return;
    } catch {
      // HTTP LAN pages may block the Clipboard API; fall through to textarea copy.
    }
  }

  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') {
    throw new Error('clipboard is not available');
  }
  const previouslyFocused = document.activeElement instanceof HTMLElement
    ? document.activeElement
    : null;
  const textarea = document.createElement('textarea');
  textarea.value = content;
  textarea.readOnly = true;
  textarea.tabIndex = -1;
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  textarea.style.pointerEvents = 'none';
  textarea.style.top = '0';
  textarea.style.left = '0';
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  try {
    if (!document.execCommand('copy')) {
      throw new Error('copy command failed');
    }
  } finally {
    textarea.remove();
    if (previouslyFocused?.isConnected) {
      previouslyFocused.focus();
    }
  }
};

function RequestLogSectionDisclosure({
  title,
  content,
  defaultOpen,
}: {
  title: string;
  content: string;
  defaultOpen: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(defaultOpen);
  const [hasOpened, setHasOpened] = useState(defaultOpen);
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const panelId = useId();
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const copyResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const chunks = useMemo(
    () => hasOpened ? splitRequestLogVirtualChunks(content) : [],
    [content, hasOpened],
  );
  // TanStack Virtual 依赖内部可变测量状态，不参与 React Compiler 自动记忆化。
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: hasOpened ? chunks.length : 0,
    getScrollElement: () => scrollerRef.current,
    estimateSize: () => REQUEST_LOG_VIRTUAL_LINE_HEIGHT,
    overscan: REQUEST_LOG_VIRTUAL_OVERSCAN,
    paddingStart: REQUEST_LOG_VIRTUAL_PADDING_Y,
    paddingEnd: REQUEST_LOG_VIRTUAL_PADDING_Y,
    initialRect: { width: 0, height: 360 },
  });
  const virtualItems = rowVirtualizer.getVirtualItems();
  const handleToggle = useCallback(() => {
    const nextOpen = !open;
    if (nextOpen) {
      setHasOpened(true);
    }
    setOpen(nextOpen);
  }, [open]);
  const handleCopy = useCallback(async () => {
    try {
      await copyRequestLogSectionContent(content);
      setCopyState('copied');
    } catch {
      setCopyState('failed');
    }
    if (copyResetTimerRef.current) {
      clearTimeout(copyResetTimerRef.current);
    }
    copyResetTimerRef.current = setTimeout(() => setCopyState('idle'), 1600);
  }, [content]);

  useEffect(() => () => {
    if (copyResetTimerRef.current) {
      clearTimeout(copyResetTimerRef.current);
    }
  }, []);

  const copyLabel = copyState === 'copied'
    ? t('usage_stats.request_events_log_copied_section', { section: title })
    : copyState === 'failed'
      ? t('usage_stats.request_events_log_copy_failed_section', { section: title })
      : t('usage_stats.request_events_log_copy_section', { section: title });

  return (
    <section
      className={`${styles.requestEventsLogSection} ${open ? styles.requestEventsLogSectionOpen : ''}`.trim()}
    >
      <div className={styles.requestEventsLogSectionHeader}>
        <button
          type="button"
          className={styles.requestEventsLogSectionTrigger}
          aria-expanded={open}
          aria-controls={panelId}
          onClick={handleToggle}
        >
          <span className={styles.requestEventsLogSectionTitle}>{title}</span>
          <span className={styles.requestEventsLogSectionChevron} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>
        <button
          type="button"
          className={`${styles.requestEventsLogSectionCopyButton} ${copyState === 'copied' ? styles.requestEventsLogSectionCopyButtonCopied : ''} ${copyState === 'failed' ? styles.requestEventsLogSectionCopyButtonFailed : ''}`.trim()}
          onClick={() => void handleCopy()}
          aria-label={copyLabel}
          title={copyLabel}
        >
          {copyState === 'copied' ? <IconCheck size={14} /> : <IconCopy size={14} />}
        </button>
      </div>
      <div
        id={panelId}
        className={styles.requestEventsLogSectionPanel}
        aria-hidden={!open}
      >
        <div className={styles.requestEventsLogSectionPanelInner} ref={scrollerRef}>
          {hasOpened ? (
            <div
              className={styles.requestEventsLogVirtualSpacer}
              style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
            >
              {virtualItems.map((virtualItem) => (
                <pre
                  key={virtualItem.key}
                  ref={rowVirtualizer.measureElement}
                  data-index={virtualItem.index}
                  className={styles.requestEventsLogVirtualLine}
                  style={{ transform: `translateY(${virtualItem.start}px)` }}
                >
                  {chunks[virtualItem.index] || ' '}
                </pre>
              ))}
            </div>
          ) : null}
        </div>
      </div>
    </section>
  );
}

function RequestEventsExportMenu({
  label,
  csvLabel,
  jsonLabel,
  exportingFormat,
  onExport,
}: {
  label: string;
  csvLabel: string;
  jsonLabel: string;
  exportingFormat: RequestEventExportFormat | null;
  onExport?: (format: RequestEventExportFormat) => void;
}) {
  const disabled = !onExport || exportingFormat !== null;
  const menuItems = useMemo<MenuProps['items']>(() => [
    { key: 'csv', label: csvLabel },
    { key: 'json', label: jsonLabel },
  ], [csvLabel, jsonLabel]);

  return (
    <Dropdown
      trigger={['click']}
      disabled={disabled}
      menu={{
        items: menuItems,
        onClick: ({ key }) => onExport?.(key as RequestEventExportFormat),
      }}
    >
      <AntButton
        size="small"
        icon={<DownloadOutlined />}
        loading={exportingFormat !== null}
        disabled={disabled}
      >
        {label}
        <DownOutlined />
      </AntButton>
    </Dropdown>
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
  exportingFormat = null,
  initialVisibleColumnIds,
  visibleColumnIds,
  onPageChange,
  onPageSizeChange,
  onModelFilterChange,
  onSourceFilterChange,
  onResultFilterChange,
  onExport,
  onVisibleColumnIdsChange,
  requestLogAccessEnabled = false,
  onRequestLogOpen,
  requestLogLoadingEventId = null,
  requestLogResponse = null,
  requestLogError = '',
  onRequestLogClose,
  onRequestLogDownload,
  requestLogDownloading = false,
  onRefresh,
  refreshing = false,
}: RequestEventsDetailsCardProps) {
  const { t } = useTranslation();
  const resultLocale = t('usage_stats.success') === 'Success' ? 'en' : 'zh';
  const latencyHint = t('usage_stats.latency_unit_hint', {
    field: LATENCY_SOURCE_FIELD,
    unit: t('usage_stats.duration_unit_ms'),
  });
  const ttftHint = t('usage_stats.ttft_hint');
  const speedHint = t('usage_stats.speed_hint');

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
      const modelValue = String(event.model ?? '').trim();
      const model = modelValue || '-';
      const modelAliasValue = String(event.model_alias ?? '').trim();
      const modelAlias = modelAliasValue && modelAliasValue !== modelValue ? modelAliasValue : '-';
      const reasoningEffort = String(event.reasoning_effort ?? '').trim() || '-';
      const speedModeRaw = String(event.service_tier ?? '').trim() || '-';
      const responseSpeedModeRaw = String(event.response_service_tier ?? '').trim() || '-';
      const speedMode = formatSpeedMode(speedModeRaw, t);
      const responseSpeedMode = formatSpeedMode(responseSpeedModeRaw, t);
      const endpointFields = parseRequestEndpoint(event.endpoint);
      const inputTokens = Math.max(toNumber(event.tokens?.input_tokens), 0);
      const outputTokens = Math.max(toNumber(event.tokens?.output_tokens), 0);
      const reasoningTokens = Math.max(toNumber(event.tokens?.reasoning_tokens), 0);
      const cacheReadTokens = Math.max(toNumber(event.tokens?.cache_read_tokens), 0);
      const cacheCreationTokens = Math.max(toNumber(event.tokens?.cache_creation_tokens), 0);
      const totalTokens = Math.max(toNumber(event.tokens?.total_tokens), 0);
      const latencyMs = Number.isFinite(event.latency_ms) ? event.latency_ms : null;
      const ttftMs = Number.isFinite(event.ttft_ms) ? event.ttft_ms as number : null;
      const speedTPS = Number.isFinite(event.speed_tps) ? event.speed_tps as number : null;
      // 费用由后端按当前价格配置运行时计算，前端只负责展示可用/不可用状态。
      const costAvailable = event.cost_available === true;
      const cost = costAvailable ? Math.max(toNumber(event.cost_usd), 0) : null;

      return {
        event,
        id: event.id ? String(event.id) : `${timestamp}-${model}-${sourceRaw || source}-${authIndex}-${index}`,
        requestId: String(event.request_id ?? '').trim(),
        timestamp,
        timestampMs: Number.isNaN(timestampMs) ? 0 : timestampMs,
        timestampLabel: formatRequestEventTimestamp(timestamp),
        apiKey,
        model,
        modelAlias,
        reasoningEffort,
        speedMode,
        speedModeRaw,
        responseSpeedMode,
        responseSpeedModeRaw,
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
        speedTPS,
        inputTokens,
        outputTokens,
        reasoningTokens,
        cacheReadTokens,
        cacheCreationTokens,
        totalTokens,
        cacheReadRate: formatCacheReadRate(cacheReadTokens, inputTokens),
        cost,
        costAvailable,
      };
    });
  }, [events, t]);

  const [internalVisibleColumnIds, setInternalVisibleColumnIds] = useState<RequestEventColumnId[]>(() => (
    normalizeRequestEventVisibleColumnIds(initialVisibleColumnIds ?? visibleColumnIds ?? DEFAULT_REQUEST_EVENT_VISIBLE_COLUMN_IDS)
  ));
  const isColumnSelectionControlled = isRequestEventColumnSelectionControlled(visibleColumnIds, onVisibleColumnIdsChange);
  const selectedVisibleColumnIds = isColumnSelectionControlled && visibleColumnIds !== undefined
    ? visibleColumnIds
    : internalVisibleColumnIds;

  const effectiveVisibleColumnIds = useMemo(
    () => normalizeRequestEventVisibleColumnIds(selectedVisibleColumnIds),
    [selectedVisibleColumnIds]
  );
  const effectiveVisibleColumnIdSet = useMemo(
    () => new Set<RequestEventColumnId>(effectiveVisibleColumnIds),
    [effectiveVisibleColumnIds]
  );
  const handleColumnToggle = useCallback((columnId: RequestEventColumnId) => {
    const nextColumnIds = toggleRequestEventColumnId(selectedVisibleColumnIds, columnId);
    if (!isColumnSelectionControlled) {
      setInternalVisibleColumnIds(nextColumnIds);
    }
    onVisibleColumnIdsChange?.(nextColumnIds);
  }, [isColumnSelectionControlled, onVisibleColumnIdsChange, selectedVisibleColumnIds]);
  const requestLogOpen = typeof document !== 'undefined' && Boolean(requestLogResponse || requestLogError || requestLogLoadingEventId);
  const requestLogTooLarge = requestLogResponse?.too_large === true || (requestLogResponse?.previewable === false && requestLogResponse?.downloadable === true);
  const requestLogTitle = requestLogTooLarge ? t('usage_stats.request_events_log_too_large_title') : t('usage_stats.request_events_log_title');
  const requestLogSections = requestLogResponse?.sections ?? [];
  const requestLogDownloadable = Boolean(requestLogResponse?.downloadable && String(requestLogResponse?.event_id ?? '').trim() && onRequestLogDownload);
  const handleRequestLogDownloadAction = useCallback(() => {
    const eventId = String(requestLogResponse?.event_id ?? '').trim();
    if (eventId && onRequestLogDownload) {
      onRequestLogDownload(eventId);
    }
  }, [onRequestLogDownload, requestLogResponse?.event_id]);

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
        title: t('usage_stats.request_events_timestamp'),
        className: styles.requestEventsNoWrapCell,
        onCell: (row) => ({ title: row.timestamp }),
        render: (row) => row.timestampLabel,
      },
      {
        id: 'api_key',
        label: t('usage_stats.api_key_filter'),
        title: t('usage_stats.api_key_filter'),
        className: styles.requestEventsAPIKeyCell,
        onCell: (row) => ({ title: row.apiKey }),
        render: (row) => row.apiKey,
      },
      {
        id: 'source',
        label: t('usage_stats.request_events_source'),
        title: t('usage_stats.request_events_source'),
        className: styles.requestEventsSourceCell,
        onCell: (row) => ({ title: row.source }),
        render: (row) => {
          const hasDistinctSourceType = Boolean(
            row.sourceType && row.sourceType.toLocaleLowerCase() !== row.source.toLocaleLowerCase(),
          );

          return (
            <span className={styles.requestEventsSourceStack}>
              <span className={styles.requestEventsSourceValue}>{row.source}</span>
              {(row.isDelete || hasDistinctSourceType) && (
                <span className={styles.requestEventsSourceTags}>
                  {hasDistinctSourceType && <Tag>{row.sourceType}</Tag>}
                  {row.isDelete && <Tag color="error">{t('usage_stats.deleted')}</Tag>}
                </span>
              )}
            </span>
          );
        },
      },
      {
        id: 'model',
        label: t('usage_stats.model_name'),
        title: t('usage_stats.model_name'),
        className: styles.modelCell,
        render: (row) => row.model,
      },
      {
        id: 'model_alias',
        label: t('usage_stats.model_alias'),
        title: t('usage_stats.model_alias'),
        className: `${styles.modelCell} ${styles.requestEventsNoWrapCell}`,
        onCell: (row) => ({ title: row.modelAlias }),
        render: (row) => row.modelAlias,
      },
      {
        id: 'reasoning_effort',
        label: t('usage_stats.reasoning_effort'),
        title: t('usage_stats.reasoning_effort'),
        className: styles.requestEventsNoWrapCell,
        headerTitle: t('usage_stats.reasoning_effort_hint'),
        render: (row) => row.reasoningEffort,
      },
      {
        id: 'service_tier',
        label: t('usage_stats.speed_mode'),
        title: t('usage_stats.speed_mode'),
        className: `${styles.requestEventsNoWrapCell} ${styles.requestEventsSpeedModeCell}`,
        render: (row) => {
          const tooltipLines = buildSpeedModeTooltipLines(row, t);
          return (
            <Tooltip
              title={(
                <div className={styles.requestEventsSpeedModeTooltipContent}>
                  {tooltipLines.map((line) => <span key={line}>{line}</span>)}
                </div>
              )}
              trigger={['hover', 'focus', 'click']}
              placement="top"
            >
              <span
                className={styles.requestEventsSpeedModeValue}
                tabIndex={0}
              >
                {`${row.speedMode} / ${row.responseSpeedMode}`}
              </span>
            </Tooltip>
          );
        },
      },
      {
        id: 'result',
        label: t('usage_stats.request_events_result'),
        title: t('usage_stats.request_events_result'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => {
          const resultLabel = row.failed ? t('usage_stats.failure') : t('usage_stats.success');
          const loading = requestLogLoadingEventId === row.id;
          const resultClassName = row.failed ? styles.requestEventsResultFailed : styles.requestEventsResultSuccess;
          const canOpenLog = Boolean(requestLogAccessEnabled && row.requestId && onRequestLogOpen);
          return canOpenLog ? (
            <button
              type="button"
              className={`${resultClassName} ${styles.requestEventsResultLogButton}`.trim()}
              data-result-locale={resultLocale}
              onClick={() => {
                onRequestLogOpen?.(row.event);
              }}
              title={t('usage_stats.request_events_log_hint')}
              aria-label={loading ? t('usage_stats.request_events_log_loading_aria', { result: resultLabel }) : t('usage_stats.request_events_log_open_aria', { result: resultLabel })}
              aria-busy={loading}
              disabled={loading}
            >
              <span>{resultLabel}</span>
              <span className={styles.requestEventsResultLogIcon} aria-hidden="true">
                <IconScrollText size={9} />
              </span>
            </button>
          ) : (
            <span className={resultClassName} data-result-locale={resultLocale}>{resultLabel}</span>
          );
        },
      },
      {
        id: 'request_type',
        label: t('usage_stats.request_type'),
        title: t('usage_stats.request_type'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.requestType,
      },
      {
        id: 'endpoint',
        label: t('usage_stats.request_endpoint'),
        title: t('usage_stats.request_endpoint'),
        className: styles.requestEventsNoWrapCell,
        onCell: (row) => ({ title: row.endpoint }),
        render: (row) => row.endpoint,
      },
      {
        id: 'ttft',
        label: t('usage_stats.ttft'),
        title: t('usage_stats.ttft'),
        className: styles.requestEventsNoWrapCell,
        headerTitle: ttftHint,
        render: (row) => formatTTFTMs(row.ttftMs),
      },
      {
        id: 'latency',
        label: t('usage_stats.latency'),
        title: t('usage_stats.latency'),
        className: styles.requestEventsNoWrapCell,
        headerTitle: latencyHint,
        render: (row) => formatDurationMs(row.latencyMs),
      },
      {
        id: 'speed',
        label: t('usage_stats.speed'),
        title: t('usage_stats.speed'),
        className: styles.requestEventsNoWrapCell,
        headerTitle: speedHint,
        render: (row) => formatSpeedTPS(row.speedTPS),
      },
      {
        id: 'input_tokens',
        label: t('usage_stats.input_tokens'),
        title: t('usage_stats.input_tokens'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.inputTokens.toLocaleString(),
      },
      {
        id: 'output_tokens',
        label: t('usage_stats.output_tokens'),
        title: t('usage_stats.output_tokens'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.outputTokens.toLocaleString(),
      },
      {
        id: 'reasoning_tokens',
        label: t('usage_stats.reasoning_tokens'),
        title: t('usage_stats.reasoning_tokens'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.reasoningTokens.toLocaleString(),
      },
      {
        id: 'cache_read_tokens',
        label: t('usage_stats.cache_read_tokens'),
        title: t('usage_stats.cache_read_tokens'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.cacheReadTokens.toLocaleString(),
      },
      {
        id: 'cache_creation_tokens',
        label: t('usage_stats.cache_creation_tokens'),
        title: t('usage_stats.cache_creation_tokens'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.cacheCreationTokens.toLocaleString(),
      },
      {
        id: 'cache_read_rate',
        label: t('usage_stats.cache_rate'),
        title: t('usage_stats.cache_rate'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.cacheReadRate,
      },
      {
        id: 'total_tokens',
        label: t('usage_stats.total_tokens'),
        title: t('usage_stats.total_tokens'),
        className: styles.requestEventsNoWrapCell,
        render: (row) => row.totalTokens.toLocaleString(),
      },
      {
        id: 'total_cost',
        label: t('usage_stats.total_cost'),
        title: t('usage_stats.total_cost'),
        className: styles.requestEventsNoWrapCell,
        onCell: (row) => ({ title: row.costAvailable ? undefined : t('usage_stats.cost_need_price') }),
        render: (row) => row.costAvailable && row.cost !== null ? formatUsd(row.cost) : '-',
      },
    ];

    return definitions;
  }, [
    latencyHint,
    onRequestLogOpen,
    requestLogAccessEnabled,
    requestLogLoadingEventId,
    resultLocale,
    speedHint,
    t,
    ttftHint,
  ]);

  const visibleColumns = useMemo(
    () => columnDefinitions.filter((definition) => effectiveVisibleColumnIdSet.has(definition.id)),
    [columnDefinitions, effectiveVisibleColumnIdSet]
  );
  const tableColumns = useMemo<TableColumnsType<RequestEventRow>>(
    () => visibleColumns.map((definition) => ({
      key: definition.id,
      title: definition.title,
      className: definition.className,
      onHeaderCell: () => ({
        className: definition.className,
        title: definition.headerTitle,
      }),
      onCell: (row) => definition.onCell?.(row) ?? {},
      render: (_value, row) => definition.render(row),
    })),
    [visibleColumns]
  );
  const columnOptions = useMemo(
    () => columnDefinitions.map((definition) => ({ id: definition.id, label: definition.label })),
    [columnDefinitions]
  );
  const visibleColumnSummary = effectiveVisibleColumnIds.length === REQUEST_EVENT_COLUMN_IDS.length
    ? t('usage_stats.request_events_columns_all')
    : t('usage_stats.request_events_columns_count', {
        selected: effectiveVisibleColumnIds.length,
        total: REQUEST_EVENT_COLUMN_IDS.length,
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
    <>
      <Card
        variant="outlined"
        className={styles.requestEventsCard}
        title={
          <SectionHeader
            headingLevel={2}
            title={t('usage_stats.request_events_title')}
            meta={(
              <span className={styles.requestEventsCountBadge}>
                {t('usage_stats.request_events_total_count', { count: totalCount })}
              </span>
            )}
            actions={(
              <div className={styles.requestEventsActions}>
                {onRefresh && (
                  <AntButton
                    size="small"
                    icon={<ReloadOutlined />}
                    loading={refreshing}
                    onClick={onRefresh}
                  >
                    {t('usage_stats.refresh')}
                  </AntButton>
                )}
                <RequestEventsColumnSelector
                  label={t('usage_stats.request_events_columns')}
                  summary={visibleColumnSummary}
                  ariaLabel={t('usage_stats.request_events_columns')}
                  options={columnOptions}
                  selectedIds={effectiveVisibleColumnIds}
                  onToggle={handleColumnToggle}
                />
                <RequestEventsExportMenu
                  label={t('usage_stats.export')}
                  csvLabel={t('usage_stats.export_csv')}
                  jsonLabel={t('usage_stats.export_json')}
                  exportingFormat={exportingFormat}
                  onExport={onExport}
                />
              </div>
            )}
          />
        }
      >
        <div className={styles.requestEventsToolbar}>
          <Form layout="inline" className={styles.requestEventsFiltersForm}>
            <Space wrap align="center" size={[12, 12]} className={styles.requestEventsFiltersGroup}>
              <Form.Item label={t('usage_stats.request_events_filter_model')}>
                <AntSelect
                  className={styles.requestEventsEntityFilter}
                  value={effectiveModelFilter}
                  options={modelOptions}
                  onChange={onModelFilterChange}
                  aria-label={t('usage_stats.request_events_filter_model')}
                  showSearch
                  optionFilterProp="label"
                  popupMatchSelectWidth={REQUEST_EVENT_ENTITY_FILTER_POPUP_WIDTH}
                />
              </Form.Item>
              <Form.Item label={t('usage_stats.request_events_filter_source')}>
                <AntSelect
                  className={styles.requestEventsEntityFilter}
                  value={effectiveSourceFilter}
                  options={sourceOptions}
                  onChange={onSourceFilterChange}
                  aria-label={t('usage_stats.request_events_filter_source')}
                  showSearch
                  optionFilterProp="label"
                  popupMatchSelectWidth={REQUEST_EVENT_ENTITY_FILTER_POPUP_WIDTH}
                />
              </Form.Item>
              <Form.Item label={t('usage_stats.request_events_filter_result')}>
                <AntSelect
                  className={styles.requestEventsResultFilter}
                  value={effectiveResultFilter}
                  options={resultOptions}
                  onChange={onResultFilterChange}
                  aria-label={t('usage_stats.request_events_filter_result')}
                />
              </Form.Item>
              <Form.Item>
                <AntButton
                  type="link"
                  onClick={handleClearFilters}
                  disabled={!hasActiveFilters}
                >
                  {t('usage_stats.clear_filters')}
                </AntButton>
              </Form.Item>
            </Space>
          </Form>
        </div>

        {loading && rows.length === 0 ? (
          <div className={styles.hint}>{t('common.loading')}</div>
        ) : rows.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={(
              <Space orientation="vertical" size={0}>
                <strong>{t('usage_stats.request_events_empty_title')}</strong>
                <span>{t('usage_stats.request_events_empty_desc')}</span>
              </Space>
            )}
          />
        ) : (
          <>
            <div className={styles.requestEventsTableWrapper}>
              <Table<RequestEventRow>
                className={styles.requestEventsTable}
                columns={tableColumns}
                dataSource={rows}
                rowKey="id"
                pagination={false}
                loading={loading}
                size="small"
                scroll={{ x: 'max-content', y: 'clamp(520px, 68vh, 760px)' }}
              />
            </div>

            <div className={styles.requestEventsPaginationFooter}>
              <Space size={[16, 12]} className={styles.requestEventsPaginationControls}>
                <Pagination
                  current={safePage || 1}
                  total={Math.max(totalCount, safeTotalPages > 0 ? (safeTotalPages - 1) * pageSize + 1 : 0)}
                  pageSize={pageSize}
                  pageSizeOptions={pageSizeOptions.map(String)}
                  showSizeChanger
                  showLessItems
                  responsive
                  size="small"
                  disabled={loading}
                  showTotal={() => pageLabel}
                  onChange={(nextPage, nextPageSize) => {
                    if (nextPageSize !== pageSize) {
                      onPageSizeChange(nextPageSize);
                      return;
                    }
                    onPageChange(nextPage);
                  }}
                />
              </Space>
            </div>
          </>
        )}
      </Card>
      <Modal
        open={requestLogOpen}
        title={requestLogTitle}
        onCancel={onRequestLogClose ?? (() => undefined)}
        width={requestLogTooLarge ? 360 : 920}
        className={requestLogTooLarge ? styles.requestEventsLargeLogModal : undefined}
        destroyOnHidden
        centered
        focusable={{ focusTriggerAfterClose: true }}
        footer={
          requestLogTooLarge ? (
            <>
              <AntButton size="small" onClick={onRequestLogClose ?? (() => undefined)}>
                {t('common.cancel')}
              </AntButton>
              <AntButton type="primary" size="small" onClick={handleRequestLogDownloadAction} loading={requestLogDownloading} disabled={!requestLogDownloadable}>
                {requestLogDownloading ? t('common.loading') : t('usage_stats.request_events_log_download')}
              </AntButton>
            </>
          ) : requestLogDownloadable ? (
            <AntButton size="small" onClick={handleRequestLogDownloadAction} loading={requestLogDownloading}>
              {requestLogDownloading ? t('common.loading') : t('usage_stats.request_events_log_download')}
            </AntButton>
          ) : null
        }
      >
        <div className={styles.requestEventsLogViewer}>
          {requestLogLoadingEventId && !requestLogResponse && !requestLogError ? (
            <div className={styles.hint} role="status" aria-live="polite">{t('common.loading')}</div>
          ) : requestLogError ? (
            <div className={styles.errorBox} role="status" aria-live="polite">{requestLogError}</div>
          ) : requestLogTooLarge ? (
            <div className={styles.requestEventsLargeLogPrompt} role="status" aria-live="polite">{t('usage_stats.request_events_log_too_large')}</div>
          ) : requestLogResponse ? (
            <>
              {requestLogSections.length > 0 ? (
                <div className={styles.requestEventsLogSections}>
                  {requestLogSections.map((section, index) => (
                    <RequestLogSectionDisclosure
                      key={`${requestLogResponse.event_id}-${section.title}-${index}`}
                      title={formatRequestLogSectionTitle(section.title, t)}
                      content={section.content}
                      defaultOpen={index === 0}
                    />
                  ))}
                </div>
              ) : (
                <div className={styles.hint}>{t('usage_stats.request_events_log_empty')}</div>
              )}
            </>
          ) : null}
        </div>
      </Modal>
    </>
  );
}
