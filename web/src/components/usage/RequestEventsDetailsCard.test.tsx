import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  RequestEventsDetailsCard,
  resolveRequestEventColumnMenuFocusIndex,
  toggleRequestEventColumnId,
  type RequestEventColumnId,
} from './RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const events: UsageEvent[] = [
  {
    id: '101',
    timestamp: '2026-04-23T02:00:00.000Z',
    api_key: 'Production Key',
    model: 'claude-sonnet',
    reasoning_effort: 'medium',
    endpoint: 'POST /v1/messages',
    source: 'Provider A',
    source_raw: 'source-a',
    source_type: 'openai',
    auth_index: '1',
    failed: false,
    latency_ms: 120,
    ttft_ms: 45,
    tokens: {
      input_tokens: 100,
      output_tokens: 60,
      reasoning_tokens: 20,
      cached_tokens: 20,
      cache_read_tokens: 20,
      cache_creation_tokens: 0,
      total_tokens: 200,
    },
    cost_usd: 0.1234,
    cost_available: true,
    pricing_style: 'claude',
  },
];

const renderCard = (props: Partial<React.ComponentProps<typeof RequestEventsDetailsCard>> = {}) =>
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
      {...props}
    />,
  );

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;

describe('RequestEventsDetailsCard pagination', () => {
  it('renders total events, current page, page size options, and disabled page buttons', () => {
    const html = renderCard();

    expect(html).toContain('120 total events');
    expect(html).toContain('Effort');
    expect(html).not.toContain('Reasoning Level');
    expect(html.indexOf('<th>Timestamp</th>')).toBeLessThan(html.indexOf('<th>Source</th>'));
    expect(html.indexOf('<th>Timestamp</th>')).toBeLessThan(html.indexOf('<th>API Key</th>'));
    expect(html.indexOf('<th>API Key</th>')).toBeLessThan(html.indexOf('<th>Source</th>'));
    expect(html.indexOf('<th>Source</th>')).toBeLessThan(html.indexOf('<th>Model</th>'));
    expect(html.indexOf('<th>Model</th>')).toBeLessThan(html.indexOf('<th title="Reasoning Effort">Effort</th>'));
    expect(html.indexOf('<th title="Time to First Token">TTFT</th>')).toBeLessThan(html.indexOf('<th title="Using latency_ms in ms">Latency</th>'));
    expect(html.indexOf('<th title="Using latency_ms in ms">Latency</th>')).toBeLessThan(html.indexOf('<th>Type</th>'));
    expect(html.indexOf('<th>Type</th>')).toBeLessThan(html.indexOf('<th>Endpoint</th>'));
    expect(html).toContain('class="_requestEventsAPIKeyCell_');
    expect(html).toContain('title="Production Key">Production Key</td>');
    expect(html).toContain('<td>medium</td>');
    expect(html).toMatch(/<td>SSE<\/td><td class="[^"]*requestEventsEndpointCell[^"]*" title="\/messages">\/messages<\/td>/);
    expect(html.indexOf('>45ms</td>')).toBeLessThan(html.indexOf('>120ms</td>'));
    expect(html).toContain('1 / 6');
    expect(html).toContain('20');
    expect(html).toContain('50');
    expect(html).toContain('100');
    expect(html).toContain('500');
    expect(html).toContain('1000');
    expect(html).toContain('Previous');
    expect(html).toContain('Next');
    expect(html).toContain('disabled');
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
      events: [{ ...events[0], ttft_ms: undefined }],
    });

    expect(html.indexOf('<th title="Time to First Token">TTFT</th>')).toBeLessThan(html.indexOf('<th title="Using latency_ms in ms">Latency</th>'));
    expect(html).toMatch(/Success<\/span><\/td><td class="[^"]*durationCell[^"]*">-<\/td><td class="[^"]*durationCell[^"]*">120ms<\/td>/);
  });

  it('shows a dash for zero TTFT values', () => {
    const html = renderCard({
      events: [{ ...events[0], ttft_ms: 0 }],
    });

    expect(html).toMatch(/Success<\/span><\/td><td class="[^"]*durationCell[^"]*">-<\/td><td class="[^"]*durationCell[^"]*">120ms<\/td>/);
  });

  it('maps GET endpoints to WS and strips the v1 prefix', () => {
    const html = renderCard({
      events: [{ ...events[0], endpoint: 'GET /v1/responses' }],
    });

    expect(html).toMatch(/<td>WS<\/td><td class="[^"]*requestEventsEndpointCell[^"]*" title="\/responses">\/responses<\/td>/);
  });

  it('strips the v1 prefix when endpoint has no request method', () => {
    const html = renderCard({
      events: [{ ...events[0], endpoint: '/v1/chat/completions' }],
    });

    expect(html).toMatch(/<td>-<\/td><td class="[^"]*requestEventsEndpointCell[^"]*" title="\/chat\/completions">\/chat\/completions<\/td>/);
  });

  it('renders cache rate after cached tokens with two decimal places', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 100, cached_tokens: 25 } }],
    });

    expect(html.indexOf('<th>Cached</th>')).toBeLessThan(html.indexOf('<th>Cache Rate</th>'));
    expect(html.indexOf('<th>Cache Rate</th>')).toBeLessThan(html.indexOf('<th>Total Tokens</th>'));
    expect(html).toContain('<td>25</td><td>25.00%</td><td>200</td>');
  });

  it('keeps cache rate based on normalized input for all providers', () => {
    const html = renderCard({
      events: [{
        ...events[0],
        source_type: 'claude',
        tokens: { ...events[0].tokens, input_tokens: 400, cached_tokens: 600, total_tokens: 500 },
      }],
    });

    expect(html).toContain('<td>600</td><td>150.00%</td><td>500</td>');
    expect(html).not.toContain('60.00%');
  });

  it('shows a dash for cache rate when input tokens are zero', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 0, cached_tokens: 25 } }],
    });

    expect(html).toContain('<td>0</td><td>60</td><td>20</td><td>25</td><td>-</td><td>200</td>');
  });

  it('stacks source value above source tags', () => {
    const html = renderCard({
      events: [{ ...events[0], isDelete: true }],
    });

    expect(html).toContain('_requestEventsSourceStack_');
    expect(html).toContain('_requestEventsSourceValue_');
    expect(html).toContain('_requestEventsSourceTags_');
    expect(html).toContain('_requestEventsDeletedTag_');
    expect(html).toContain('Provider A');
    expect(html).toContain('openai');
    expect(html).toContain('Deleted');
  });

  it('uses backend source values while showing resolved source labels', () => {
    const html = renderCard({
      sourceFilter: 'source-a',
      sourceOptions: [{ value: 'source-a', label: 'Provider A', displayName: 'Provider A(Team Prefix)' }, { value: 'source-b', label: 'Provider B' }],
    });

    expect(countOccurrences(html, 'Provider A(Team Prefix)')).toBeGreaterThanOrEqual(1);
    expect(html).toContain('aria-label="Source"><span class="_triggerText_c80422 ">Provider A(Team Prefix)</span>');
  });

  it('uses backend model and source options instead of current page grouping', () => {
    const html = renderCard({ modelFilter: 'claude-opus', sourceFilter: 'source-b' });

    expect(html).toContain('aria-label="Model"><span class="_triggerText_c80422 ">claude-opus</span>');
    expect(html).toContain('aria-label="Source"><span class="_triggerText_c80422 ">Provider B</span>');
  });

  it('renders a Result filter and no Credential filter control', () => {
    const html = renderCard({ resultFilter: 'failed' });

    expect(html).toContain('aria-label="Result"');
    expect(html).toContain('Failure');
    expect(html).not.toContain('aria-label="Credential"');
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

  it('shows total count in the title and uses the shared pager footer', () => {
    const html = renderCard();

    expect(html).toContain('_requestEventsFiltersGroup_');
    expect(html).toContain('_requestEventsTitleRow_');
    expect(html).toContain('_requestEventsCountBadge_');
    expect(html).toContain('120 total events');
    expect(html).toContain('_requestEventsPaginationFooter_');
    expect(html).toContain('_requestEventsPaginationControls_');
    expect(html).toContain('_requestEventsPageSizeControl_');
    expect(html).toContain('Size');
    expect(html).not.toContain('Rows per page');
    expect(html).toContain('_requestEventsPaginationPage_');
    expect(html).toContain('_requestEventsPagerButton_');
    expect(html).toContain('<select');
    expect(html).toContain('value="20"');
    expect(html).toContain('_requestEventsActions_');
    expect(html).not.toContain('_requestEventsPaginationItem_');
    expect(html).not.toContain('_requestEventsPageSizeSelectCompact_');
    expect(html).not.toContain('_usagePillShell_');
    expect(html).not.toContain('_requestEventsTableMeta_');
    expect(html).not.toContain('_requestEventsCountGroup_');
    expect(html).not.toContain('_requestEventsLimitHint_');
  });

  it('hides export buttons while keeping clear filters available', () => {
    const html = renderCard({ modelFilter: 'claude-sonnet' });

    expect(html).toContain('Clear Filters');
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
    expect(html).toContain('title="Set pricing to calculate cost">-</td>');
  });

  it('renders a column selector before the page size control', () => {
    const html = renderCard();

    expect(html).toContain('aria-label="Columns"');
    expect(html.indexOf('aria-label="Columns"')).toBeLessThan(html.indexOf('<span>Size</span>'));
    expect(html).toContain('>All</span>');
  });

  it('can render only the selected request event columns', () => {
    const html = renderCard({
      initialVisibleColumnIds: ['timestamp', 'model', 'total_cost'],
    });

    expect(html).toContain('<th>Timestamp</th>');
    expect(html).toContain('<th>Model</th>');
    expect(html).toContain('<th>Total Cost</th>');
    expect(html).toContain('2026/04/23 02:00:00');
    expect(html).toContain('<td class="_modelCell_');
    expect(html).toContain('$0.1234');
    expect(html).not.toContain('<th>API Key</th>');
    expect(html).not.toContain('<th>Source</th>');
    expect(html).not.toContain('<th title="Time to First Token">TTFT</th>');
    expect(html).not.toContain('<th title="Using latency_ms in ms">Latency</th>');
    expect(html).not.toContain('title="Production Key">Production Key</td>');
  });

  it('keeps at least one request event column selected', () => {
    const selected: RequestEventColumnId[] = ['timestamp'];

    expect(toggleRequestEventColumnId(selected, 'timestamp')).toEqual(['timestamp']);
    expect(toggleRequestEventColumnId(selected, 'model')).toEqual(['timestamp', 'model']);
  });

  it('cycles column menu focus for arrow and tab navigation', () => {
    expect(resolveRequestEventColumnMenuFocusIndex(0, 3, 'ArrowDown')).toBe(1);
    expect(resolveRequestEventColumnMenuFocusIndex(2, 3, 'ArrowDown')).toBe(0);
    expect(resolveRequestEventColumnMenuFocusIndex(0, 3, 'ArrowUp')).toBe(2);
    expect(resolveRequestEventColumnMenuFocusIndex(2, 3, 'Tab')).toBe(0);
    expect(resolveRequestEventColumnMenuFocusIndex(0, 3, 'Tab', true)).toBe(2);
    expect(resolveRequestEventColumnMenuFocusIndex(1, 3, 'Escape')).toBeNull();
    expect(resolveRequestEventColumnMenuFocusIndex(0, 0, 'ArrowDown')).toBeNull();
  });
});
