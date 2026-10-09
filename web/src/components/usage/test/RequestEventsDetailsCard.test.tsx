import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  type RequestEventsDetailsCard,
  isRequestEventColumnSelectionControlled,
  shouldCloseMenuOnFocusLeave,
  shouldLoadMoreRequestEvents,
  toggleRequestEventColumnId,
  type RequestEventColumnId,
} from '../RequestEventsDetailsCard';
import { RequestEventsTestCard, extractTableHeaders, extractFirstTableRowCells } from './requestEventsFixtures';
import type { UsageEvent } from '@/lib/types';

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

const renderCard = (props: Partial<React.ComponentProps<typeof RequestEventsDetailsCard>> = {}) =>
  renderToStaticMarkup(
    <RequestEventsTestCard
      events={events}
      totalCount={120}
      modelOptions={['claude-sonnet', 'claude-opus']}
      sourceOptions={[{ value: 'source-a', label: 'Provider A' }, { value: 'source-b', label: 'Provider B' }]}
      {...props}
    />,
  );

const tableValues = (html: string) => {
  const cells = extractFirstTableRowCells(html);
  return Object.fromEntries(extractTableHeaders(html).map((header, index) => [header, cells[index]]));
};

describe('RequestEventsDetailsCard', () => {
  it('renders the event title, total and incremental loading status', () => {
    const html = renderCard();
    expect(html).toContain('Request Event Log');
    expect(html).toContain('120 total events');
    expect(html).toContain('Loaded 1 / 120');
    for (const obsoleteControl of ['Event Stream', 'Rows per page', '>Previous<', '>Next<']) {
      expect(html).not.toContain(obsoleteControl);
    }
    expect(tableValues(html)['API Key']).toBe('Production Key');
  });

  it.each([
    ['auto', 'priority', 'Auto / Fast'],
    ['default', 'priority', 'Standard / Fast'],
    ['standard', 'priority', 'Standard / Fast'],
    ['priority', 'default', 'Fast / Standard'],
    ['fast', 'default', 'Fast / Standard'],
    ['flex', 'default', 'Flex / Standard'],
    ['', 'priority', '- / Fast'],
    ['batch', 'default', 'batch / Standard'],
  ])('maps request mode %j independently of response mode %j', (request, response, expected) => {
    const html = renderCard({
      visibleColumnIds: ['service_tier'],
      events: [{ ...events[0], service_tier: request, response_service_tier: response }],
    });
    expect(tableValues(html)['Speed Mode']).toBe(expected);
  });

  it('formats the API timestamp as compact numeric date and time', () => {
    const html = renderCard({ events: [{ ...events[0], timestamp: '2026-05-13T00:38:19+08:00' }] });
    expect(tableValues(html).Timestamp).toBe('00:38:192026/05/13');
  });

  it.each([undefined, 0])('keeps missing or zero TTFT %s visible within Latency', (ttft) => {
    const html = renderCard({ events: [{ ...events[0], ttft_ms: ttft }] });
    expect(extractTableHeaders(html)).not.toContain('TTFT');
    expect(tableValues(html).Latency).toBe('120msTTFT -');
  });

  it('keeps Latency and Speed visible when their values are missing', () => {
    const html = renderCard({ events: [{ ...events[0], latency_ms: undefined, speed_tps: undefined }] });
    expect(tableValues(html)).toMatchObject({ Latency: '--TTFT 45ms', Speed: '-' });
  });

  it.each([
    ['GET /v1/responses', 'WS/responses'],
    ['/v1/chat/completions', '-/chat/completions'],
  ])('formats endpoint %j', (endpoint, expected) => {
    const html = renderCard({ events: [{ ...events[0], endpoint }] });
    expect(tableValues(html).Request).toBe(expected);
  });

  it('keeps cache rate based on normalized input for Claude too', () => {
    const html = renderCard({ events: [{
      ...events[0], source_type: 'claude',
      tokens: { ...events[0].tokens, input_tokens: 400, cache_read_tokens: 600, total_tokens: 500 },
    }] });
    expect(tableValues(html).Cache).toBe('150.00%6000');
  });

  it('shows a dash for cache rate when input tokens are zero', () => {
    const html = renderCard({ events: [{
      ...events[0], tokens: { ...events[0].tokens, input_tokens: 0, cache_read_tokens: 25 },
    }] });
    expect(tableValues(html).Cache).toBe('-250');
  });

  it('uses backend source values while showing resolved labels', () => {
    const html = renderCard({
      sourceFilter: 'source-a',
      sourceOptions: [{ value: 'source-a', label: 'Provider A', displayName: 'Team Prefix' }],
    });
    expect(html).toMatch(/<input[^>]*role="combobox"[^>]*aria-label="Source"[^>]*value="Team Prefix"/);
  });

  it('uses backend options independently of current page grouping', () => {
    const html = renderCard({ modelFilter: 'claude-opus', sourceFilter: 'source-b' });
    expect(html).toMatch(/<input[^>]*role="combobox"[^>]*aria-label="Model"[^>]*value="claude-opus"/);
    expect(html).toMatch(/<input[^>]*role="combobox"[^>]*aria-label="Source"[^>]*value="Provider B"/);
  });

  it('renders the selected Status filter without a Credential control', () => {
    const html = renderCard({ resultFilter: 'failed' });
    expect(html).toContain('aria-label="Status"');
    expect(html).toContain('Failure');
    expect(html).not.toContain('aria-label="Credential"');
  });

  it.each(['101', undefined])('opens request logs for request IDs with event ID %s', (id) => {
    const html = renderCard({
      events: [{ ...events[0], id, request_id: 'req-log-101' }],
      requestLogAccessEnabled: true,
      onRequestLogOpen: () => undefined,
    });
    expect(html).toContain('title="Click to view request log"');
    expect(html).toMatch(/<button[^>]*aria-label="Success. View request log"[^>]*>.*Success.*<\/button>/);
  });

  it('keeps the result label stable while a request log loads', () => {
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

  it('renders log content without exposing request ID, filename or cache metadata', () => {
    const html = renderCard({
      requestLogResponse: {
        event_id: '101', request_id: 'req-log-101', filename: 'preview-req-log-101.log', available: true,
        sections: [
          { title: 'REQUEST INFO', content: 'URL: /v1/responses' },
          { title: 'API RESPONSE ERROR', content: '{"error":"quota exceeded"}' },
        ],
      },
      onRequestLogClose: () => undefined,
    });
    expect(html).toContain('Request Info');
    expect(html).toContain('API Response Error');
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain('URL: /v1/responses');
    for (const hidden of ['Request ID', '<span>Cached</span>', '<span>Fresh</span>', 'preview-req-log-101.log']) {
      expect(html).not.toContain(hidden);
    }
  });

  it('offers a download for oversized logs without rendering sections', () => {
    const html = renderCard({
      requestLogResponse: {
        event_id: '101', request_id: 'req-log-101', filename: 'large-request.log',
        available: true, previewable: false, too_large: true, downloadable: true, sections: [],
      },
      onRequestLogClose: () => undefined,
    });
    expect(html).toContain('Request Log Too Large');
    expect(html).toContain('Download Raw Log');
    expect(html).toContain('Cancel');
    expect(html).not.toContain('aria-controls=');
  });

  it('keeps selected filters visible when absent from backend options', () => {
    const html = renderCard({ modelFilter: 'claude-haiku', sourceFilter: 'source-c' });
    expect(html).toContain('claude-haiku');
    expect(html).toContain('source-c');
  });

  it('places one export menu after Columns and before filters', () => {
    const html = renderCard({ modelFilter: 'claude-sonnet' });
    expect(html).toContain('Clear Filters');
    expect(html.match(/>Export</g)).toHaveLength(1);
    expect(html.indexOf('aria-label="Columns"')).toBeGreaterThan(-1);
    expect(html.indexOf('aria-label="Columns"')).toBeLessThan(html.indexOf('>Export<'));
    expect(html.indexOf('>Export<')).toBeLessThan(html.indexOf('aria-label="Status"'));
    expect(html.indexOf('aria-label="Status"')).toBeLessThan(html.indexOf('Clear Filters'));
    expect(html).toContain('aria-haspopup="menu"');
    expect(html).not.toContain('Export CSV');
    expect(html).not.toContain('Export JSON');
  });

  it('shows a dash and pricing hint when backend cost is unavailable', () => {
    const html = renderCard({ events: [{ ...events[0], cost_usd: 0, cost_available: false }] });
    expect(tableValues(html).Cost).toBe('-Claude Style');
    expect(html).toContain('title="Set pricing to calculate cost"');
  });

  it.each([
    { initialVisibleColumnIds: ['timestamp', 'model', 'total_cost'] as RequestEventColumnId[] },
    { visibleColumnIds: ['timestamp', 'model'] as RequestEventColumnId[] },
  ])('renders only the supplied column selection %j', (props) => {
    const html = renderCard(props);
    expect(tableValues(html)).toEqual({
      Timestamp: '02:00:002026/04/23', Model: 'claude-sonnet',
      ...(props.initialVisibleColumnIds ? { Cost: '$0.1234Claude Style' } : {}),
    });
  });

  it('keeps at least one request event column selected', () => {
    const selected: RequestEventColumnId[] = ['timestamp'];
    expect(toggleRequestEventColumnId(selected, 'timestamp')).toEqual(['timestamp']);
    expect(toggleRequestEventColumnId(selected, 'model')).toEqual(['timestamp', 'model']);
  });

  it('requires both value and callback for controlled selection', () => {
    expect(isRequestEventColumnSelectionControlled(['timestamp'], () => undefined)).toBe(true);
    expect(isRequestEventColumnSelectionControlled(undefined, () => undefined)).toBe(false);
    expect(isRequestEventColumnSelectionControlled(['timestamp'], undefined)).toBe(false);
  });

  it('shows loaded status, a load-more action and the full accessible row count', () => {
    const html = renderCard({ hasMore: true, totalCount: 500, onLoadMore: () => undefined });
    expect(html).toContain('Loaded 1 / 500');
    expect(html).toContain('Load more');
    expect(html).not.toContain('>Previous<');
    expect(html).not.toContain('>Next<');
    expect(html).toContain('aria-rowcount="501"');
  });

  it('preloads cursor batches near the bottom while ignoring an empty scroller', () => {
    expect(shouldLoadMoreRequestEvents({ scrollTop: 100, clientHeight: 600, scrollHeight: 2000 })).toBe(false);
    expect(shouldLoadMoreRequestEvents({ scrollTop: 400, clientHeight: 600, scrollHeight: 2000 })).toBe(true);
    expect(shouldLoadMoreRequestEvents({ scrollTop: 0, clientHeight: 0, scrollHeight: 0 })).toBe(false);
  });

  it('closes the export menu only when focus leaves its container', () => {
    const inside = new EventTarget();
    const outside = new EventTarget();
    const container = { contains: (target: EventTarget) => target === inside };
    expect(shouldCloseMenuOnFocusLeave(container, inside)).toBe(false);
    expect(shouldCloseMenuOnFocusLeave(container, outside)).toBe(true);
    expect(shouldCloseMenuOnFocusLeave(container, null)).toBe(true);
  });
});
