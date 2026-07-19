import React from 'react';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  DEFAULT_REQUEST_EVENT_VISIBLE_COLUMN_IDS,
  REQUEST_EVENT_COLUMN_IDS,
  RequestEventsDetailsCard,
  isRequestEventColumnSelectionControlled,
  toggleRequestEventColumnId,
  type RequestEventColumnId,
} from './RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const requestEventsSource = readFileSync(new URL('./RequestEventsDetailsCard.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const requestLogModalSource = requestEventsSource.slice(
  requestEventsSource.indexOf('<Modal\n        open={requestLogOpen}'),
  requestEventsSource.indexOf('\n      </Modal>', requestEventsSource.indexOf('<Modal\n        open={requestLogOpen}')),
);

const events: UsageEvent[] = [
  {
    id: '101',
    timestamp: '2026-04-23T02:00:00.000Z',
    api_key: 'Production Key',
    model: 'claude-sonnet',
    reasoning_effort: 'medium',
    service_tier: 'auto',
    response_service_tier: 'priority',
    endpoint: 'POST /v1/messages',
    source: 'Provider A',
    source_raw: 'source-a',
    source_type: 'openai',
    auth_index: '1',
    failed: false,
    latency_ms: 120,
    ttft_ms: 45,
    speed_tps: 30,
    tokens: {
      input_tokens: 100,
      output_tokens: 60,
      reasoning_tokens: 20,
      cache_read_tokens: 20,
      cache_creation_tokens: 0,
      total_tokens: 200,
    },
    cost_usd: 0.1234,
    cost_available: true,
    pricing_style: 'claude',
  },
];

const renderCard = (
  props: Partial<React.ComponentProps<typeof RequestEventsDetailsCard>> = {},
  diagnosticColumns = true,
) =>
  renderToStaticMarkup(
    <RequestEventsDetailsCard
      events={events}
      loading={false}
      page={1}
      pageSize={20}
      pageSizeOptions={[20, 50, 100, 500, 1000]}
      totalCount={120}
      totalPages={6}
      modelOptions={['claude-sonnet', 'claude-opus']}
      sourceOptions={[{ value: 'source-a', label: 'Provider A' }, { value: 'source-b', label: 'Provider B' }]}
      modelFilter="__all__"
      sourceFilter="__all__"
      resultFilter="__all__"
      onPageChange={() => undefined}
      onPageSizeChange={() => undefined}
      onModelFilterChange={() => undefined}
      onSourceFilterChange={() => undefined}
      onResultFilterChange={() => undefined}
      {...(diagnosticColumns && props.initialVisibleColumnIds === undefined && props.visibleColumnIds === undefined
        ? { initialVisibleColumnIds: REQUEST_EVENT_COLUMN_IDS }
        : {})}
      {...props}
    />,
  );

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;
const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();
const extractTableHeaders = (html: string) => (
  Array.from(html.matchAll(/<th\b[^>]*>(.*?)<\/th>/gs), (match) => textFromMarkup(match[1]))
);
const extractFirstTableRowCells = (html: string) => {
  const row = html.match(/<tr\b[^>]*data-row-key="[^"]+"[^>]*>(.*?)<\/tr>/s)?.[1] ?? '';
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]));
};
const cellForHeader = (html: string, header: string) => {
  const headers = extractTableHeaders(html);
  return extractFirstTableRowCells(html)[headers.indexOf(header)];
};

describe('RequestEventsDetailsCard pagination', () => {
  it('renders the title without the Event Stream eyebrow', () => {
    const html = renderCard();

    expect(html).toContain('Request Event Log');
    expect(html).not.toContain('Event Stream');
  });

  it('uses the operational monitoring columns when no view preference is supplied', () => {
    const html = renderCard({}, false);

    expect(DEFAULT_REQUEST_EVENT_VISIBLE_COLUMN_IDS).toEqual([
      'timestamp',
      'source',
      'model',
      'result',
      'latency',
      'total_tokens',
      'total_cost',
    ]);
    expect(extractTableHeaders(html)).toEqual([
      'Timestamp',
      'Source',
      'Model',
      'Result',
      'Latency',
      'Total Tokens',
      'Total Cost',
    ]);
  });

  it('renders total events, current page, page size options, and disabled page buttons', () => {
    const html = renderCard();

    const headers = extractTableHeaders(html);
    expect(html).toContain('120 total events');
    expect(headers).toEqual(expect.arrayContaining([
      'Timestamp', 'API Key', 'Source', 'Model', 'Effort', 'Speed Mode', 'Result', 'Type',
      'Endpoint', 'TTFT', 'Latency', 'Speed', 'Input',
    ]));
    expect(html).not.toContain('Reasoning Level');
    expect(headers.indexOf('Timestamp')).toBeLessThan(headers.indexOf('API Key'));
    expect(headers.indexOf('API Key')).toBeLessThan(headers.indexOf('Source'));
    expect(headers.indexOf('Source')).toBeLessThan(headers.indexOf('Model'));
    expect(headers.indexOf('Model')).toBeLessThan(headers.indexOf('Effort'));
    expect(headers.indexOf('Effort')).toBeLessThan(headers.indexOf('Speed Mode'));
    expect(headers.indexOf('Speed Mode')).toBeLessThan(headers.indexOf('Result'));
    expect(headers.indexOf('Result')).toBeLessThan(headers.indexOf('Type'));
    expect(headers.indexOf('Type')).toBeLessThan(headers.indexOf('Endpoint'));
    expect(headers.indexOf('Endpoint')).toBeLessThan(headers.indexOf('TTFT'));
    expect(headers.indexOf('TTFT')).toBeLessThan(headers.indexOf('Latency'));
    expect(headers.indexOf('Latency')).toBeLessThan(headers.indexOf('Speed'));
    expect(headers.indexOf('Speed')).toBeLessThan(headers.indexOf('Input'));
    expect(cellForHeader(html, 'API Key')).toBe('Production Key');
    expect(cellForHeader(html, 'Effort')).toBe('medium');
    expect(cellForHeader(html, 'Speed Mode')).toBe('Auto / Fast');
    expect(cellForHeader(html, 'Type')).toBe('SSE');
    expect(cellForHeader(html, 'Endpoint')).toBe('/messages');
    expect(cellForHeader(html, 'TTFT')).toBe('45ms');
    expect(cellForHeader(html, 'Latency')).toBe('120ms');
    expect(cellForHeader(html, 'Speed')).toBe('30.0 t/s');
    expect(html).toContain('1 / 6');
    expect(html).toContain('ant-pagination');
    expect(html).toContain('20 / page');
    expect(html).toContain('title="Previous Page"');
    expect(html).toContain('title="Next Page"');
    expect(html).toContain('disabled');
    expect(requestEventsSource).toContain('pageSizeOptions={pageSizeOptions.map(String)}');
    expect(requestEventsSource).toContain('showSizeChanger');
  });

  it('maps request speed mode values before the independently mapped response mode', () => {
    const html = renderCard({
      visibleColumnIds: ['reasoning_effort', 'service_tier', 'result'],
      events: [
        { ...events[0], id: 'auto', service_tier: 'auto', response_service_tier: 'priority' },
        { ...events[0], id: 'default', service_tier: 'default', response_service_tier: 'priority' },
        { ...events[0], id: 'standard', service_tier: 'standard', response_service_tier: 'priority' },
        { ...events[0], id: 'priority', service_tier: 'priority', response_service_tier: 'default' },
        { ...events[0], id: 'fast', service_tier: 'fast', response_service_tier: 'default' },
        { ...events[0], id: 'flex', service_tier: 'flex', response_service_tier: 'default' },
        { ...events[0], id: 'empty', service_tier: '', response_service_tier: 'priority' },
        { ...events[0], id: 'unknown', service_tier: 'batch', response_service_tier: 'default' },
      ],
    });

    expect(countOccurrences(html, 'Auto / Fast')).toBe(1);
    expect(countOccurrences(html, 'Standard / Fast')).toBe(2);
    expect(countOccurrences(html, 'Fast / Standard')).toBe(2);
    expect(countOccurrences(html, 'Flex / Standard')).toBe(1);
    expect(countOccurrences(html, '- / Fast')).toBe(1);
    expect(countOccurrences(html, 'batch / Standard')).toBe(1);
  });

  it('formats timestamps with compact numeric date and time', () => {
    const html = renderCard({
      events: [{ ...events[0], timestamp: '2026-05-13T00:38:19+08:00' }],
    });

    expect(html).toContain('2026/05/13 00:38:19');
    expect(html).not.toContain('5/13/2026, 12:38:19 AM');
  });

  it('keeps the TTFT column visible when TTFT is missing', () => {
    const html = renderCard({
      events: [{ ...events[0], ttft_ms: undefined, speed_tps: undefined }],
    });

    const headers = extractTableHeaders(html);
    expect(headers.indexOf('TTFT')).toBeLessThan(headers.indexOf('Latency'));
    expect(cellForHeader(html, 'TTFT')).toBe('-');
    expect(cellForHeader(html, 'Latency')).toBe('120ms');
    expect(cellForHeader(html, 'Speed')).toBe('-');
  });

  it('keeps the Latency column visible when latency is missing', () => {
    const html = renderCard({
      events: [{ ...events[0], latency_ms: undefined, speed_tps: undefined }],
    });

    const headers = extractTableHeaders(html);
    expect(headers.indexOf('TTFT')).toBeLessThan(headers.indexOf('Latency'));
    expect(headers.indexOf('Latency')).toBeLessThan(headers.indexOf('Speed'));
    expect(cellForHeader(html, 'TTFT')).toBe('45ms');
    expect(cellForHeader(html, 'Latency')).toBe('--');
    expect(cellForHeader(html, 'Speed')).toBe('-');
  });

  it('shows a dash for zero TTFT values', () => {
    const html = renderCard({
      events: [{ ...events[0], ttft_ms: 0, speed_tps: undefined }],
    });

    expect(cellForHeader(html, 'TTFT')).toBe('-');
    expect(cellForHeader(html, 'Latency')).toBe('120ms');
    expect(cellForHeader(html, 'Speed')).toBe('-');
  });

  it('maps GET endpoints to WS and strips the v1 prefix', () => {
    const html = renderCard({
      events: [{ ...events[0], endpoint: 'GET /v1/responses' }],
    });

    expect(cellForHeader(html, 'Type')).toBe('WS');
    expect(cellForHeader(html, 'Endpoint')).toBe('/responses');
  });

  it('strips the v1 prefix when endpoint has no request method', () => {
    const html = renderCard({
      events: [{ ...events[0], endpoint: '/v1/chat/completions' }],
    });

    expect(cellForHeader(html, 'Type')).toBe('-');
    expect(cellForHeader(html, 'Endpoint')).toBe('/chat/completions');
  });

  it('renders cache rate after cache read and write with two decimal places', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 100, cache_read_tokens: 25 } }],
    });

    expect(html.indexOf('>Cache Read</th>')).toBeLessThan(html.indexOf('>Cache Write</th>'));
    expect(html.indexOf('>Cache Write</th>')).toBeLessThan(html.indexOf('>Cache Rate</th>'));
    expect(html.indexOf('>Cache Rate</th>')).toBeLessThan(html.indexOf('>Total Tokens</th>'));
    expect(html).toMatch(/<td class="[^"]*requestEventsNoWrapCell[^"]*">25<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">0<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">25\.00%<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">200<\/td>/);
  });

  it('keeps cache rate based on normalized input for all providers', () => {
    const html = renderCard({
      events: [{
        ...events[0],
        source_type: 'claude',
        tokens: { ...events[0].tokens, input_tokens: 400, cache_read_tokens: 600, total_tokens: 500 },
      }],
    });

    expect(html).toMatch(/<td class="[^"]*requestEventsNoWrapCell[^"]*">600<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">0<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">150\.00%<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">500<\/td>/);
    expect(html).not.toContain('60.00%');
  });

  it('shows a dash for cache rate when input tokens are zero', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 0, cache_read_tokens: 25 } }],
    });

    expect(html).toMatch(/<td class="[^"]*requestEventsNoWrapCell[^"]*">0<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">60<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">20<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">25<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">0<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">-<\/td><td class="[^"]*requestEventsNoWrapCell[^"]*">200<\/td>/);
  });

  it('renders source metadata inline with Ant Design tags', () => {
    const html = renderCard({
      events: [{ ...events[0], isDelete: true }],
    });

    expect(html).toContain('_requestEventsSourceStack_');
    expect(html).toContain('_requestEventsSourceValue_');
    expect(html).toContain('_requestEventsSourceTags_');
    expect(html).toContain('ant-tag');
    expect(html).toContain('Provider A');
    expect(html).toContain('openai');
    expect(html).toContain('Deleted');
  });

  it('uses backend source values while showing resolved source labels', () => {
    const html = renderCard({
      sourceFilter: 'source-a',
      sourceOptions: [{ value: 'source-a', label: 'Provider A', displayName: 'Team Prefix' }, { value: 'source-b', label: 'Provider B' }],
    });

    expect(countOccurrences(html, 'Team Prefix')).toBeGreaterThanOrEqual(1);
    expect(html).toContain('title="Team Prefix">Team Prefix<input');
    expect(html).toContain('aria-label="Source"');
  });

  it('uses backend model and source options instead of current page grouping', () => {
    const html = renderCard({ modelFilter: 'claude-opus', sourceFilter: 'source-b' });

    expect(html).toContain('title="claude-opus">claude-opus<input');
    expect(html).toContain('aria-label="Model"');
    expect(html).toContain('title="Provider B">Provider B<input');
    expect(html).toContain('aria-label="Source"');
  });

  it('renders a Result filter and no Credential filter control', () => {
    const html = renderCard({ resultFilter: 'failed' });

    expect(html).toContain('aria-label="Result"');
    expect(html).toContain('Failure');
    expect(html).not.toContain('aria-label="Credential"');
  });

  it('renders the Result badge as a request log trigger when request id is available', () => {
    const html = renderCard({
      events: [{ ...events[0], request_id: 'req-log-101' }],
      requestLogAccessEnabled: true,
      onRequestLogOpen: () => undefined,
    });

    expect(html).toContain('title="Click to view request log"');
    expect(html).toContain('aria-label="Success. View request log"');
    expect(html).toContain('_requestEventsResultLogButton_');
    expect(html).toContain('_requestEventsResultLogIcon_');
    expect(html).toMatch(/<button[^>]*>.*Success.*<\/button>/);
  });

  it('renders the Result badge as a request log trigger when the event id is missing', () => {
    const html = renderCard({
      events: [{ ...events[0], id: undefined, request_id: 'req-log-missing-id' }],
      requestLogAccessEnabled: true,
      onRequestLogOpen: () => undefined,
    });

    expect(html).toContain('title="Click to view request log"');
    expect(html).toContain('_requestEventsResultLogButton_');
  });

  it('keeps the Result badge label stable while a request log loads', () => {
    const html = renderCard({
      events: [{ ...events[0], request_id: 'req-log-101' }],
      requestLogAccessEnabled: true,
      onRequestLogOpen: () => undefined,
      requestLogLoadingEventId: '101',
    });

    expect(html).toContain('aria-label="Success. Loading request log"');
    expect(html).toContain('aria-busy="true"');
    expect(html).toMatch(/<button[^>]*>.*Success.*<\/button>/);
    expect(html).not.toMatch(/<button[^>]*>.*Loading\.\.\..*<\/button>/);
  });

  it('defines request log sections without request id or cache metadata', () => {
    expect(requestLogModalSource).toContain('<RequestLogSectionDisclosure');
    expect(requestLogModalSource).toContain('title={formatRequestLogSectionTitle(section.title, t)}');
    expect(requestLogModalSource).toContain('content={section.content}');
    expect(requestLogModalSource).toContain('defaultOpen={index === 0}');
    expect(requestLogModalSource).not.toContain('Request ID');
    expect(requestLogModalSource).not.toContain('Cached');
    expect(requestLogModalSource).not.toContain('Fresh');
    expect(requestLogModalSource).not.toContain('filename');
  });

  it('defines the direct Ant Design request-log modal with preserved widths and footer branches', () => {
    expect(requestLogModalSource).toContain('onCancel={onRequestLogClose ?? (() => undefined)}');
    expect(requestLogModalSource).toContain('width={requestLogTooLarge ? 360 : 920}');
    expect(requestLogModalSource).toContain('className={requestLogTooLarge ? styles.requestEventsLargeLogModal : undefined}');
    expect(requestEventsSource).toContain("const requestLogOpen = typeof document !== 'undefined' && Boolean(requestLogResponse || requestLogError || requestLogLoadingEventId);");
    expect(requestLogModalSource).toContain("t('usage_stats.request_events_log_too_large')");
    expect(requestLogModalSource).toContain("t('usage_stats.request_events_log_download')");
    expect(requestLogModalSource).toContain("t('common.cancel')");
    expect(requestLogModalSource).toContain(': null');
    expect(requestLogModalSource.indexOf('requestEventsLargeLogPrompt')).toBeLessThan(requestLogModalSource.indexOf('requestEventsLogSections'));
  });

  it('keeps selected filters visible when backend options do not include them', () => {
    const html = renderCard({
      modelFilter: 'claude-haiku',
      sourceFilter: 'source-c',
    });

    expect(html).toContain('claude-haiku');
    expect(html).toContain('source-c');
  });

  it('falls back to a computed page count when metadata is not populated', () => {
    const html = renderCard({ totalPages: 0, totalCount: 120, pageSize: 20 });

    expect(html).toContain('1 / 6');
  });

  it('shows total count in the title and uses Ant Design pagination', () => {
    const html = renderCard();

    expect(html).toContain('_requestEventsFiltersForm_');
    expect(html).toContain('_requestEventsFiltersGroup_');
    expect(html).toContain('<h2');
    expect(html).toContain('_requestEventsCountBadge_');
    expect(html).not.toContain('_requestEventsTitleRow_');
    expect(html).toContain('120 total events');
    expect(html).toContain('_requestEventsPaginationFooter_');
    expect(html).toContain('_requestEventsPaginationControls_');
    expect(html).toContain('ant-pagination');
    expect(html).toContain('aria-label="Page Size"');
    expect(html).toContain('_requestEventsActions_');
    expect(html).not.toContain('_requestEventsPageSizeControl_');
    expect(html).not.toContain('_requestEventsPaginationPage_');
    expect(html).not.toContain('_requestEventsPagerButton_');
    expect(html).not.toContain('<select');
    expect(html).not.toContain('_requestEventsPaginationItem_');
    expect(html).not.toContain('_requestEventsPageSizeSelectCompact_');
    expect(html).not.toContain('_usagePillShell_');
    expect(html).not.toContain('_requestEventsTableMeta_');
    expect(html).not.toContain('_requestEventsCountGroup_');
    expect(html).not.toContain('_requestEventsLimitHint_');
  });

  it('renders one Ant Design export dropdown instead of separate CSV and JSON buttons', () => {
    const html = renderCard({ modelFilter: 'claude-sonnet' });

    expect(html).toContain('Clear Filters');
    expect(countOccurrences(html, '>Export<')).toBe(1);
    expect(html.indexOf('aria-label="Result"')).toBeLessThan(html.indexOf('Clear Filters'));
    expect(requestEventsSource.indexOf('<RequestEventsColumnSelector')).toBeLessThan(requestEventsSource.indexOf('<RequestEventsExportMenu'));
    expect(requestEventsSource.indexOf('<RequestEventsColumnSelector')).toBeLessThan(requestEventsSource.indexOf('<div className={styles.requestEventsToolbar}>'));
    expect(requestEventsSource).toContain('<Dropdown');
    expect(requestEventsSource).toContain("trigger={['click']}");
    expect(requestEventsSource).toContain("{ key: 'csv', label: csvLabel }");
    expect(requestEventsSource).toContain("{ key: 'json', label: jsonLabel }");
    expect(requestEventsSource).toContain('<DownloadOutlined />');
    expect(html).not.toContain('_requestEventsExportButton_');
    expect(html).not.toContain('_requestEventsExportButtonInner_');
    expect(html).not.toContain('Export CSV');
    expect(html).not.toContain('Export JSON');
  });

  it('shows per-event cost returned by the backend', () => {
    const html = renderCard();

    expect(html).toContain('Total Cost');
    expect(html).toContain('$0.1234');
  });

  it('shows a dash when backend cost is unavailable', () => {
    const html = renderCard({
      events: [{ ...events[0], cost_usd: 0, cost_available: false }],
    });

    expect(html).toContain('Total Cost');
    expect(cellForHeader(html, 'Total Cost')).toBe('-');
    expect(html).toContain('title="Set pricing to calculate cost"');
  });

  it('renders the Columns control in the header and keeps pagination in the footer', () => {
    const html = renderCard({}, false);

    expect(html).toContain('aria-label="Columns"');
    expect(html).toContain('7/21');
    expect(requestEventsSource).toContain('<Checkbox');
    expect(requestEventsSource).toContain('icon={<TableOutlined />}');
    expect(requestEventsSource).not.toMatch(/requestEventsPaginationFooter[\s\S]*<RequestEventsColumnSelector/);
  });

  it('can render only the selected request event columns', () => {
    const html = renderCard({
      initialVisibleColumnIds: ['timestamp', 'model', 'total_cost'],
    });

    expect(extractTableHeaders(html)).toEqual(['Timestamp', 'Model', 'Total Cost']);
    expect(extractFirstTableRowCells(html)).toEqual(['2026/04/23 02:00:00', 'claude-sonnet', '$0.1234']);
    expect(html).not.toContain('title="Production Key"');
  });

  it('honors controlled request event column selection', () => {
    const html = renderCard({
      visibleColumnIds: ['timestamp', 'model'],
    });

    expect(extractTableHeaders(html)).toEqual(['Timestamp', 'Model']);
    expect(extractFirstTableRowCells(html)).toEqual(['2026/04/23 02:00:00', 'claude-sonnet']);
    expect(html).not.toContain('$0.1234');
  });

  it('keeps at least one request event column selected', () => {
    const selected: RequestEventColumnId[] = ['timestamp'];

    expect(toggleRequestEventColumnId(selected, 'timestamp')).toEqual(['timestamp']);
    expect(toggleRequestEventColumnId(selected, 'model')).toEqual(['timestamp', 'model']);
  });

  it('treats request event columns as controlled only when value and callback are both provided', () => {
    expect(isRequestEventColumnSelectionControlled(['timestamp'], () => undefined)).toBe(true);
    expect(isRequestEventColumnSelectionControlled(undefined, () => undefined)).toBe(false);
    expect(isRequestEventColumnSelectionControlled(['timestamp'], undefined)).toBe(false);
  });
});
