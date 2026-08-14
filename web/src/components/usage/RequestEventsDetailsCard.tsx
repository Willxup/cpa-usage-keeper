import React, {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { MainActionButton } from '@/components/ui/MainActionButton';
import { Modal } from '@/components/ui/Modal';
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip';
import { Select } from '@/components/ui/Select';
import { IconCheck, IconChevronDown, IconCopy, IconDownload, IconScrollText, IconSettings } from '@/components/ui/icons';
import type { UsageEvent, UsageEventRequestLogResponse, UsageSourceFilterOption } from '@/lib/types';
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment';
import {
  calculateCacheReadRate,
  formatDurationMs,
  formatUsd,
  LATENCY_SOURCE_FIELD,
  normalizeAuthIndex,
} from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';
import {
  REQUEST_EVENT_COLUMN_IDS,
  normalizeRequestEventColumnOrder,
  normalizeRequestEventVisibleColumnIds,
  type RequestEventColumnId,
} from './requestEventColumns';
import { RequestEventsColumnSettingsModal } from './RequestEventsColumnSettingsModal';

export {
  REQUEST_EVENT_COLUMN_IDS,
  normalizeRequestEventColumnOrder,
  normalizeRequestEventVisibleColumnIds,
  toggleRequestEventColumnId,
  type RequestEventColumnId,
} from './requestEventColumns';

const ALL_FILTER = '__all__';
const REQUEST_EVENT_VIRTUALIZATION_THRESHOLD = 50;
const REQUEST_EVENT_VIRTUAL_ROW_HEIGHT = 44;
const REQUEST_EVENT_VIRTUAL_OVERSCAN = 8;
const REQUEST_EVENT_VIRTUAL_INITIAL_VIEWPORT_HEIGHT = 760;
const REQUEST_EVENT_LOAD_MORE_THRESHOLD_PX = 1200;
const REQUEST_LOG_VIRTUAL_LINE_HEIGHT = 18;
const REQUEST_LOG_VIRTUAL_OVERSCAN = 8;
const REQUEST_LOG_VIRTUAL_PADDING_Y = 12;
const REQUEST_LOG_VIRTUAL_CHUNK_CHARS = 2048;
const REQUEST_LOG_VIRTUAL_BREAK_LOOKBACK = 256;
const REQUEST_LOG_GRAPHEME_CONTEXT_CHARS = 64;
const REQUEST_EVENT_CLIENT_IP_DISPLAY_LENGTH = 39;
const REQUEST_EVENT_X_FORWARDED_FOR_DISPLAY_LENGTH = 48;
const REQUEST_EVENT_USER_AGENT_DISPLAY_LENGTH = 48;
const REQUEST_EVENT_INTEGER_FORMATTER = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });
const REQUEST_LOG_GRAPHEME_SEGMENTER = typeof Intl !== 'undefined' && typeof Intl.Segmenter === 'function'
  ? new Intl.Segmenter(undefined, { granularity: 'grapheme' })
  : null;

type SelectOption = { value: string; label: string };

export type RequestEventExportFormat = 'csv' | 'json';

export const isRequestEventColumnSelectionControlled = (
  visibleColumnIds: readonly RequestEventColumnId[] | undefined,
  onVisibleColumnIdsChange: ((columnIds: RequestEventColumnId[]) => void) | undefined,
) => visibleColumnIds !== undefined && onVisibleColumnIdsChange !== undefined;

export const shouldCloseMenuOnFocusLeave = (
  container: { contains: (target: EventTarget) => boolean },
  nextFocus: EventTarget | null
): boolean => nextFocus === null || !container.contains(nextFocus);

export const shouldLoadMoreRequestEvents = ({
  scrollTop,
  clientHeight,
  scrollHeight,
  threshold = REQUEST_EVENT_LOAD_MORE_THRESHOLD_PX,
}: {
  scrollTop: number;
  clientHeight: number;
  scrollHeight: number;
  threshold?: number;
}): boolean => scrollHeight > 0 && scrollTop + clientHeight >= scrollHeight - Math.max(threshold, 0);

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
  latencyLabel: string;
  ttftMs: number | null;
  ttftLabel: string;
  speedTPS: number | null;
  speedLabel: string;
  clientIP: string;
  xForwardedFor: string;
  userAgent: string;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  totalTokens: number;
  inputTokensLabel: string;
  outputTokensLabel: string;
  reasoningTokensLabel: string;
  cacheReadTokensLabel: string;
  cacheCreationTokensLabel: string;
  totalTokensLabel: string;
  cacheReadRate: string;
  cost: number | null;
  costAvailable: boolean;
  costLabel: string;
};

type RequestEventColumnDefinition = {
  id: RequestEventColumnId;
  label: string;
  header: ReactNode;
  renderCell: (row: RequestEventRow) => ReactNode;
};

type RequestEventTableRowProps = {
  row: RequestEventRow;
  columns: readonly RequestEventColumnDefinition[];
  virtualIndex?: number;
  measureElement?: (node: HTMLTableRowElement | null) => void;
};

const RequestEventTableRow = React.memo(function RequestEventTableRow({
  row,
  columns,
  virtualIndex,
  measureElement,
}: RequestEventTableRowProps) {
  return (
    <tr
      ref={measureElement}
      data-index={virtualIndex}
      aria-rowindex={virtualIndex === undefined ? undefined : virtualIndex + 2}
    >
      {columns.map((column) => (
        <React.Fragment key={column.id}>{column.renderCell(row)}</React.Fragment>
      ))}
    </tr>
  );
});

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
  totalCount: number;
  modelOptions: string[];
  sourceOptions: UsageSourceFilterOption[];
  modelFilter: string;
  sourceFilter: string;
  resultFilter: string;
  exportingFormat?: RequestEventExportFormat | null;
  hasMore?: boolean;
  loadingMore?: boolean;
  autoLoadMore?: boolean;
  initialVisibleColumnIds?: readonly RequestEventColumnId[];
  initialColumnOrder?: readonly RequestEventColumnId[];
  visibleColumnIds?: readonly RequestEventColumnId[];
  columnOrder?: readonly RequestEventColumnId[];
  onModelFilterChange: (model: string) => void;
  onLoadMore?: () => void;
  onSourceFilterChange: (source: string) => void;
  onResultFilterChange: (result: string) => void;
  onExport?: (format: RequestEventExportFormat) => void;
  onVisibleColumnIdsChange?: (columnIds: RequestEventColumnId[]) => void;
  onColumnOrderChange?: (columnIds: RequestEventColumnId[]) => void;
  requestLogAccessEnabled?: boolean;
  onRequestLogOpen?: (event: UsageEvent) => void;
  requestLogLoadingEventId?: string | null;
  requestLogResponse?: UsageEventRequestLogResponse | null;
  requestLogError?: string;
  onRequestLogClose?: () => void;
  onRequestLogDownload?: (eventId: string) => void;
  requestLogDownloading?: boolean;
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

const truncateRequestEventMetadata = (value: string, maxLength: number): string => {
  const characters = Array.from(value);
  return characters.length <= maxLength
    ? value
    : `${characters.slice(0, maxLength).join('')}...`;
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
  useScrollBoundaryContainment(scrollerRef);
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
  const [open, setOpen] = useState(false);
  const disabled = !onExport || exportingFormat !== null;

  const handleSelect = (format: RequestEventExportFormat) => {
    setOpen(false);
    onExport?.(format);
  };

  const handleTriggerClick = () => {
    if (disabled) return;
    setOpen((currentOpen) => !currentOpen);
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      setOpen(false);
    }
  };

  const handleBlur = (event: React.FocusEvent<HTMLDivElement>) => {
    if (shouldCloseMenuOnFocusLeave({
      contains: (target) => target instanceof Node && event.currentTarget.contains(target),
    }, event.relatedTarget)) {
      setOpen(false);
    }
  };

  return (
    <div
      className={styles.requestEventsExportMenu}
      onMouseEnter={() => !disabled && setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onKeyDown={handleKeyDown}
      onBlur={handleBlur}
    >
      <MainActionButton
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={disabled}
        loading={exportingFormat !== null}
        onClick={handleTriggerClick}
      >
        <IconDownload size={12} aria-hidden="true" />
        <span>{label}</span>
        <IconChevronDown size={12} aria-hidden="true" />
      </MainActionButton>
      {open && !disabled && (
        <div className={styles.requestEventsExportDropdown} role="menu" aria-label={label}>
          <button type="button" role="menuitem" onClick={() => handleSelect('csv')}>
            {csvLabel}
          </button>
          <button type="button" role="menuitem" onClick={() => handleSelect('json')}>
            {jsonLabel}
          </button>
        </div>
      )}
    </div>
  );
}

export function RequestEventsDetailsCard({
  events,
  loading,
  totalCount,
  modelOptions: backendModelOptions,
  sourceOptions: backendSourceOptions,
  modelFilter,
  sourceFilter,
  resultFilter,
  exportingFormat = null,
  hasMore = false,
  loadingMore = false,
  autoLoadMore = true,
  initialVisibleColumnIds,
  initialColumnOrder,
  visibleColumnIds,
  columnOrder,
  onModelFilterChange,
  onSourceFilterChange,
  onLoadMore,
  onResultFilterChange,
  onExport,
  onVisibleColumnIdsChange,
  onColumnOrderChange,
  requestLogAccessEnabled = false,
  onRequestLogOpen,
  requestLogLoadingEventId = null,
  requestLogResponse = null,
  requestLogError = '',
  onRequestLogClose,
  onRequestLogDownload,
  requestLogDownloading = false,
}: RequestEventsDetailsCardProps) {
  const { t } = useTranslation();
  const {
    tooltip: requestEventsTooltip,
    showOnMouseEnter: handleRequestEventsTooltipMouseEnter,
    hideOnMouseLeave: handleRequestEventsTooltipMouseLeave,
    showOnFocus: handleRequestEventsTooltipFocus,
    hideOnBlur: handleRequestEventsTooltipBlur,
  } = usePortalTooltip();
  const [columnSettingsOpen, setColumnSettingsOpen] = useState(false);
  const [columnSettingsSession, setColumnSettingsSession] = useState(0);
  const requestEventsTableWrapperRef = useRef<HTMLDivElement | null>(null);
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
      const clientIP = String(event.client_ip ?? '').trim() || '-';
      const xForwardedFor = String(event.x_forwarded_for ?? '').trim() || '-';
      const userAgent = String(event.user_agent ?? '').trim() || '-';
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
        latencyLabel: formatDurationMs(latencyMs),
        ttftMs,
        ttftLabel: formatTTFTMs(ttftMs),
        speedTPS,
        speedLabel: formatSpeedTPS(speedTPS),
        clientIP,
        xForwardedFor,
        userAgent,
        inputTokens,
        outputTokens,
        reasoningTokens,
        cacheReadTokens,
        cacheCreationTokens,
        totalTokens,
        inputTokensLabel: REQUEST_EVENT_INTEGER_FORMATTER.format(inputTokens),
        outputTokensLabel: REQUEST_EVENT_INTEGER_FORMATTER.format(outputTokens),
        reasoningTokensLabel: REQUEST_EVENT_INTEGER_FORMATTER.format(reasoningTokens),
        cacheReadTokensLabel: REQUEST_EVENT_INTEGER_FORMATTER.format(cacheReadTokens),
        cacheCreationTokensLabel: REQUEST_EVENT_INTEGER_FORMATTER.format(cacheCreationTokens),
        totalTokensLabel: REQUEST_EVENT_INTEGER_FORMATTER.format(totalTokens),
        cacheReadRate: formatCacheReadRate(cacheReadTokens, inputTokens),
        cost,
        costAvailable,
        costLabel: costAvailable && cost !== null ? formatUsd(cost) : '-',
      };
    });
  }, [events, t]);
  const virtualizeRows = rows.length > REQUEST_EVENT_VIRTUALIZATION_THRESHOLD;
  // TanStack Virtual 依赖内部可变测量状态，不参与 React Compiler 自动记忆化。
  // eslint-disable-next-line react-hooks/incompatible-library
  const eventRowVirtualizer = useVirtualizer({
    count: virtualizeRows ? rows.length : 0,
    getScrollElement: () => requestEventsTableWrapperRef.current,
    estimateSize: () => REQUEST_EVENT_VIRTUAL_ROW_HEIGHT,
    overscan: REQUEST_EVENT_VIRTUAL_OVERSCAN,
    getItemKey: (index) => rows[index]?.id ?? index,
    initialRect: { width: 0, height: REQUEST_EVENT_VIRTUAL_INITIAL_VIEWPORT_HEIGHT },
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualRows = eventRowVirtualizer.getVirtualItems();
  const virtualPaddingTop = virtualRows.length > 0 ? virtualRows[0].start : 0;
  const virtualPaddingBottom = virtualRows.length > 0
    ? Math.max(eventRowVirtualizer.getTotalSize() - virtualRows[virtualRows.length - 1].end, 0)
    : 0;
  const handleTableScroll = useCallback((event: React.UIEvent<HTMLDivElement>) => {
    if (!autoLoadMore || !hasMore || loading || loadingMore || !onLoadMore) return;
    const scroller = event.currentTarget;
    if (shouldLoadMoreRequestEvents(scroller)) {
      onLoadMore();
    }
  }, [autoLoadMore, hasMore, loading, loadingMore, onLoadMore]);
  useEffect(() => {
    const scroller = requestEventsTableWrapperRef.current;
    if (!scroller || !autoLoadMore || !hasMore || loading || loadingMore || !onLoadMore) return;
    if (shouldLoadMoreRequestEvents(scroller)) {
      onLoadMore();
    }
  }, [autoLoadMore, hasMore, loading, loadingMore, onLoadMore, rows.length]);
  useScrollBoundaryContainment(requestEventsTableWrapperRef, rows.length > 0);

  const [internalVisibleColumnIds, setInternalVisibleColumnIds] = useState<RequestEventColumnId[]>(() => (
    normalizeRequestEventVisibleColumnIds(initialVisibleColumnIds ?? visibleColumnIds ?? REQUEST_EVENT_COLUMN_IDS)
  ));
  const [internalColumnOrder, setInternalColumnOrder] = useState<RequestEventColumnId[]>(() => (
    normalizeRequestEventColumnOrder(initialColumnOrder ?? columnOrder ?? REQUEST_EVENT_COLUMN_IDS)
  ));
  const isColumnSelectionControlled = isRequestEventColumnSelectionControlled(visibleColumnIds, onVisibleColumnIdsChange);
  const isColumnOrderControlled = columnOrder !== undefined && onColumnOrderChange !== undefined;
  const selectedVisibleColumnIds = isColumnSelectionControlled && visibleColumnIds !== undefined
    ? visibleColumnIds
    : internalVisibleColumnIds;
  const selectedColumnOrder = isColumnOrderControlled && columnOrder !== undefined
    ? columnOrder
    : internalColumnOrder;

  const effectiveVisibleColumnIds = useMemo(
    () => normalizeRequestEventVisibleColumnIds(selectedVisibleColumnIds),
    [selectedVisibleColumnIds]
  );
  const effectiveVisibleColumnIdSet = useMemo(
    () => new Set<RequestEventColumnId>(effectiveVisibleColumnIds),
    [effectiveVisibleColumnIds]
  );
  useLayoutEffect(() => {
    if (virtualizeRows) {
      eventRowVirtualizer.measure();
    }
  }, [effectiveVisibleColumnIds, eventRowVirtualizer, virtualizeRows]);
  const effectiveColumnOrder = useMemo(
    () => normalizeRequestEventColumnOrder(selectedColumnOrder),
    [selectedColumnOrder]
  );
  const handleColumnSettingsApply = useCallback((
    nextVisibleColumnIds: RequestEventColumnId[],
    nextColumnOrder: RequestEventColumnId[],
  ) => {
    if (!isColumnSelectionControlled) {
      setInternalVisibleColumnIds(nextVisibleColumnIds);
    }
    if (!isColumnOrderControlled) {
      setInternalColumnOrder(nextColumnOrder);
    }
    onVisibleColumnIdsChange?.(nextVisibleColumnIds);
    onColumnOrderChange?.(nextColumnOrder);
  }, [isColumnOrderControlled, isColumnSelectionControlled, onColumnOrderChange, onVisibleColumnIdsChange]);
  const requestLogOpen = Boolean(requestLogResponse || requestLogError || requestLogLoadingEventId);
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

  const renderClientMetadataCell = useCallback((value: string, maxLength: number) => {
    const hasValue = value !== '-';
    const tooltipLines = [value];
    return (
      <td
        className={`${styles.requestEventsNoWrapCell} ${styles.requestEventsSpeedModeCell}`}
        tabIndex={hasValue ? 0 : undefined}
        aria-label={hasValue ? value : undefined}
        onMouseEnter={hasValue
          ? (event) => handleRequestEventsTooltipMouseEnter(tooltipLines, event.currentTarget)
          : undefined}
        onMouseLeave={hasValue
          ? (event) => handleRequestEventsTooltipMouseLeave(event.currentTarget)
          : undefined}
        onFocus={hasValue
          ? (event) => handleRequestEventsTooltipFocus(tooltipLines, event.currentTarget)
          : undefined}
        onBlur={hasValue
          ? (event) => handleRequestEventsTooltipBlur(event.currentTarget)
          : undefined}
      >
        {truncateRequestEventMetadata(value, maxLength)}
      </td>
    );
  }, [
    handleRequestEventsTooltipBlur,
    handleRequestEventsTooltipFocus,
    handleRequestEventsTooltipMouseEnter,
    handleRequestEventsTooltipMouseLeave,
  ]);

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
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.request_events_timestamp')}</th>,
        renderCell: (row) => (
          <td title={row.timestamp} className={styles.requestEventsNoWrapCell}>
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
        id: 'model_alias',
        label: t('usage_stats.model_alias'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.model_alias')}</th>,
        renderCell: (row) => <td className={styles.modelCell} title={row.modelAlias}>{row.modelAlias}</td>,
      },
      {
        id: 'reasoning_effort',
        label: t('usage_stats.reasoning_effort'),
        header: <th className={styles.requestEventsNoWrapCell} title={t('usage_stats.reasoning_effort_hint')}>{t('usage_stats.reasoning_effort')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.reasoningEffort}</td>,
      },
      {
        id: 'service_tier',
        label: t('usage_stats.speed_mode'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.speed_mode')}</th>,
        renderCell: (row) => {
          const tooltipLines = buildSpeedModeTooltipLines(row, t);
          return (
            <td
              className={`${styles.requestEventsNoWrapCell} ${styles.requestEventsSpeedModeCell}`}
              tabIndex={0}
              aria-label={tooltipLines.join('; ')}
              onMouseEnter={(event) => handleRequestEventsTooltipMouseEnter(tooltipLines, event.currentTarget)}
              onMouseLeave={(event) => handleRequestEventsTooltipMouseLeave(event.currentTarget)}
              onFocus={(event) => handleRequestEventsTooltipFocus(tooltipLines, event.currentTarget)}
              onBlur={(event) => handleRequestEventsTooltipBlur(event.currentTarget)}
            >
              {`${row.speedMode} / ${row.responseSpeedMode}`}
            </td>
          );
        },
      },
      {
        id: 'result',
        label: t('usage_stats.request_events_result'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.request_events_result')}</th>,
        renderCell: (row) => {
          const resultLabel = row.failed ? t('usage_stats.failure') : t('usage_stats.success');
          const loading = requestLogLoadingEventId === row.id;
          const resultClassName = row.failed ? styles.requestEventsResultFailed : styles.requestEventsResultSuccess;
          const canOpenLog = Boolean(requestLogAccessEnabled && row.requestId && onRequestLogOpen);
          return (
            <td className={styles.requestEventsNoWrapCell}>
              {canOpenLog ? (
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
              )}
            </td>
          );
        },
      },
      {
        id: 'request_type',
        label: t('usage_stats.request_type'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.request_type')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.requestType}</td>,
      },
      {
        id: 'endpoint',
        label: t('usage_stats.request_endpoint'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.request_endpoint')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell} title={row.endpoint}>{row.endpoint}</td>,
      },
      {
        id: 'ttft',
        label: t('usage_stats.ttft'),
        header: <th className={styles.requestEventsNoWrapCell} title={ttftHint}>{t('usage_stats.ttft')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.ttftLabel}</td>,
      },
      {
        id: 'latency',
        label: t('usage_stats.latency'),
        header: <th className={styles.requestEventsNoWrapCell} title={latencyHint}>{t('usage_stats.latency')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.latencyLabel}</td>,
      },
      {
        id: 'speed',
        label: t('usage_stats.speed'),
        header: <th className={styles.requestEventsNoWrapCell} title={speedHint}>{t('usage_stats.speed')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.speedLabel}</td>,
      },
      {
        id: 'client_ip',
        label: t('usage_stats.client_ip'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.client_ip')}</th>,
        renderCell: (row) => renderClientMetadataCell(
          row.clientIP,
          REQUEST_EVENT_CLIENT_IP_DISPLAY_LENGTH,
        ),
      },
      {
        id: 'x_forwarded_for',
        label: t('usage_stats.x_forwarded_for'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.x_forwarded_for')}</th>,
        renderCell: (row) => renderClientMetadataCell(
          row.xForwardedFor,
          REQUEST_EVENT_X_FORWARDED_FOR_DISPLAY_LENGTH,
        ),
      },
      {
        id: 'user_agent',
        label: t('usage_stats.user_agent'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.user_agent')}</th>,
        renderCell: (row) => renderClientMetadataCell(
          row.userAgent,
          REQUEST_EVENT_USER_AGENT_DISPLAY_LENGTH,
        ),
      },
      {
        id: 'input_tokens',
        label: t('usage_stats.input_tokens'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.input_tokens')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.inputTokensLabel}</td>,
      },
      {
        id: 'output_tokens',
        label: t('usage_stats.output_tokens'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.output_tokens')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.outputTokensLabel}</td>,
      },
      {
        id: 'reasoning_tokens',
        label: t('usage_stats.reasoning_tokens'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.reasoning_tokens')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.reasoningTokensLabel}</td>,
      },
      {
        id: 'cache_read_tokens',
        label: t('usage_stats.cache_read_tokens'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.cache_read_tokens')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.cacheReadTokensLabel}</td>,
      },
      {
        id: 'cache_creation_tokens',
        label: t('usage_stats.cache_creation_tokens'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.cache_creation_tokens')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.cacheCreationTokensLabel}</td>,
      },
      {
        id: 'cache_read_rate',
        label: t('usage_stats.cache_rate'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.cache_rate')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.cacheReadRate}</td>,
      },
      {
        id: 'total_tokens',
        label: t('usage_stats.total_tokens'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.total_tokens')}</th>,
        renderCell: (row) => <td className={styles.requestEventsNoWrapCell}>{row.totalTokensLabel}</td>,
      },
      {
        id: 'total_cost',
        label: t('usage_stats.total_cost'),
        header: <th className={styles.requestEventsNoWrapCell}>{t('usage_stats.total_cost')}</th>,
        renderCell: (row) => (
          <td className={styles.requestEventsNoWrapCell} title={row.costAvailable ? undefined : t('usage_stats.cost_need_price')}>
            {row.costLabel}
          </td>
        ),
      },
    ];

    return definitions;
  }, [
    handleRequestEventsTooltipBlur,
    handleRequestEventsTooltipFocus,
    handleRequestEventsTooltipMouseEnter,
    handleRequestEventsTooltipMouseLeave,
    latencyHint,
    onRequestLogOpen,
    requestLogAccessEnabled,
    requestLogLoadingEventId,
    renderClientMetadataCell,
    resultLocale,
    speedHint,
    t,
    ttftHint,
  ]);

  const visibleColumns = useMemo(() => {
    const definitionsById = new Map(columnDefinitions.map((definition) => [definition.id, definition]));
    return effectiveColumnOrder.flatMap((columnId) => {
      const definition = definitionsById.get(columnId);
      return definition && effectiveVisibleColumnIdSet.has(columnId) ? [definition] : [];
    });
  }, [columnDefinitions, effectiveColumnOrder, effectiveVisibleColumnIdSet]);
  const columnOptions = useMemo(
    () => columnDefinitions.map((definition) => ({ id: definition.id, label: definition.label })),
    [columnDefinitions]
  );

  const hasActiveFilters =
    modelFilter !== ALL_FILTER ||
    sourceFilter !== ALL_FILTER ||
    resultFilter !== ALL_FILTER;


  const handleClearFilters = () => {
    onModelFilterChange(ALL_FILTER);
    onSourceFilterChange(ALL_FILTER);
    onResultFilterChange(ALL_FILTER);
  };

  return (
    <>
      <Card
        className={styles.requestEventsCard}
        variant="flush"
        title={t('usage_stats.request_events_title')}
        subtitle={t('usage_stats.request_events_subtitle')}
        titleMeta={
          <span className={styles.requestEventsCountBadge}>
            {t('usage_stats.request_events_total_count', { count: totalCount })}
          </span>
        }
        extra={
          <div className={styles.requestEventsActions}>
            <MainActionButton
              type="button"
              data-request-events-column-settings-trigger="true"
              aria-label={t('usage_stats.request_events_columns')}
              onClick={() => {
                // 新会话重新挂载草稿状态，取消或关闭后不会复用上一次未提交修改。
                setColumnSettingsSession((currentSession) => currentSession + 1);
                setColumnSettingsOpen(true);
              }}
            >
              <IconSettings size={12} aria-hidden="true" />
              <span>{t('usage_stats.request_events_columns')}</span>
            </MainActionButton>
            <RequestEventsExportMenu
              label={t('usage_stats.export')}
              csvLabel={t('usage_stats.export_csv')}
              jsonLabel={t('usage_stats.export_json')}
              exportingFormat={exportingFormat}
              onExport={onExport}
            />
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
            <div className={styles.requestEventsFilterActionSlot}>
              <Button
                variant="ghost"
                size="sm"
                appearance="action"
                onClick={handleClearFilters}
                disabled={!hasActiveFilters}
              >
                {t('usage_stats.clear_filters')}
              </Button>
            </div>
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
            <div ref={requestEventsTableWrapperRef} className={styles.requestEventsTableWrapper} data-virtualized={virtualizeRows} data-loaded-row-count={rows.length} onScroll={handleTableScroll}>
              <table className={styles.table} aria-rowcount={totalCount + 1}>
                <thead>
                  <tr>
                    {visibleColumns.map((column) => (
                      <React.Fragment key={column.id}>{column.header}</React.Fragment>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {virtualizeRows ? (
                    <>
                      {virtualPaddingTop > 0 && (
                        <tr
                          className={styles.requestEventsVirtualSpacerRow}
                          style={{ height: `${virtualPaddingTop}px` }}
                          aria-hidden="true"
                        >
                          <td colSpan={visibleColumns.length} />
                        </tr>
                      )}
                      {virtualRows.map((virtualRow) => {
                        const row = rows[virtualRow.index];
                        return (
                          <RequestEventTableRow
                            key={virtualRow.key}
                            row={row}
                            columns={visibleColumns}
                            virtualIndex={virtualRow.index}
                            measureElement={eventRowVirtualizer.measureElement}
                          />
                        );
                      })}
                      {virtualPaddingBottom > 0 && (
                        <tr
                          className={styles.requestEventsVirtualSpacerRow}
                          style={{ height: `${virtualPaddingBottom}px` }}
                          aria-hidden="true"
                        >
                          <td colSpan={visibleColumns.length} />
                        </tr>
                      )}
                    </>
                  ) : rows.map((row) => (
                    <RequestEventTableRow key={row.id} row={row} columns={visibleColumns} />
                  ))}
                </tbody>
              </table>
            </div>

            <div className={styles.requestEventsPaginationFooter}>
              <div className={styles.requestEventsPaginationControls}>
                <>
                  <span className={styles.requestEventsPaginationPage} aria-live="polite">
                    {t('usage_stats.request_events_loaded_count', { loaded: rows.length, total: totalCount })}
                  </span>
                  {hasMore && (
                    <button
                      type="button"
                      className={styles.requestEventsPagerButton}
                      onClick={onLoadMore}
                      disabled={loading || loadingMore}
                    >
                      {loadingMore ? t('common.loading') : t('usage_stats.request_events_load_more')}
                    </button>
                  )}
                </>
              </div>
            </div>
          </>
        )}
      </Card>
      <RequestEventsColumnSettingsModal
        key={columnSettingsSession}
        open={columnSettingsOpen}
        options={columnOptions}
        visibleColumnIds={effectiveVisibleColumnIds}
        columnOrder={effectiveColumnOrder}
        onApply={handleColumnSettingsApply}
        onClose={() => setColumnSettingsOpen(false)}
      />
      <PortalTooltip tooltip={requestEventsTooltip} />
      <Modal
        open={requestLogOpen}
        title={requestLogTitle}
        onClose={onRequestLogClose ?? (() => undefined)}
        width={requestLogTooLarge ? 360 : 920}
        className={requestLogTooLarge ? styles.requestEventsLargeLogModal : undefined}
        footer={
          requestLogTooLarge ? (
            <>
              <Button variant="secondary" size="sm" appearance="action" onClick={onRequestLogClose ?? (() => undefined)}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" size="sm" appearance="action" onClick={handleRequestLogDownloadAction} loading={requestLogDownloading} disabled={!requestLogDownloadable}>
                {requestLogDownloading ? t('common.loading') : t('usage_stats.request_events_log_download')}
              </Button>
            </>
          ) : requestLogDownloadable ? (
            <Button variant="secondary" size="sm" appearance="action" onClick={handleRequestLogDownloadAction} loading={requestLogDownloading}>
              {requestLogDownloading ? t('common.loading') : t('usage_stats.request_events_log_download')}
            </Button>
          ) : undefined
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
