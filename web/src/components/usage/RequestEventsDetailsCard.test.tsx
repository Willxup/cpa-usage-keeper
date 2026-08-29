import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  RequestEventsDetailsCard,
  isRequestEventColumnSelectionControlled,
  shouldCloseMenuOnFocusLeave,
  shouldLoadMoreRequestEvents,
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
    speed_total_tps: 28.8,
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
    <RequestEventsDetailsCard
      events={events}
      loading={false}
      totalCount={120}
      modelOptions={['claude-sonnet', 'claude-opus']}
      sourceOptions={[{ value: 'source-a', label: 'Provider A' }, { value: 'source-b', label: 'Provider B' }]}
      modelFilter="__all__"
      sourceFilter="__all__"
      resultFilter="__all__"
      onModelFilterChange={() => undefined}
      onSourceFilterChange={() => undefined}
      onResultFilterChange={() => undefined}
      {...props}
    />,
  );

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;

describe('RequestEventsDetailsCard pagination', () => {
  it('renders the shared flush card heading without bolding its subtitle', () => {
    const html = renderCard();

    expect(html).toContain('card-flush');
    expect(html).toContain('keeper-card-title');
    expect(html).toContain('keeper-card-title-meta');
    expect(html).toContain('keeper-card-subtitle');
    expect(html).toMatch(/class="keeper-card-subtitle">[^<]+<\/p>/);
  });

  it('renders the title without the Event Stream eyebrow', () => {
    const html = renderCard();

    expect(html).toContain('Request Event Log');
    expect(html).not.toContain('Event Stream');
  });

  it('renders total events, columns, and incremental loading status', () => {
    const html = renderCard();

    expect(html).toContain('120 total events');
    expect(html).toContain('Effort');
    expect(html).not.toContain('Reasoning Level');
    expect(html.indexOf('>Timestamp</th>')).toBeLessThan(html.indexOf('>API Key</th>'));
    expect(html.indexOf('>API Key</th>')).toBeLessThan(html.indexOf('>Source</th>'));
    expect(html.indexOf('>Source</th>')).toBeLessThan(html.indexOf('>Model</th>'));
    expect(html.indexOf('>Model</th>')).toBeLessThan(html.indexOf('title="Reasoning Effort">Effort</th>'));
    expect(html.indexOf('title="Reasoning Effort">Effort</th>')).toBeLessThan(html.indexOf('>Speed Mode</th>'));
    expect(html.indexOf('>Speed Mode</th>')).toBeLessThan(html.indexOf('>Result</th>'));
    expect(html.indexOf('>Result</th>')).toBeLessThan(html.indexOf('>Request</th>'));
    expect(html.indexOf('>Request</th>')).toBeLessThan(html.indexOf('>Latency</th>'));
    expect(html.indexOf('>Latency</th>')).toBeLessThan(html.indexOf('title="Average output tokens per second after TTFT">Speed</th>'));
    expect(html.indexOf('title="Average output tokens per second after TTFT">Speed</th>')).toBeLessThan(html.indexOf('>Tokens</th>'));
    expect(html).toContain('title="Average visible output tokens per second including TTFT">Overall Speed</th>');
    expect(html.indexOf('>Tokens</th>')).toBeLessThan(html.indexOf('>Cache</th>'));
    expect(html.indexOf('>Cache</th>')).toBeLessThan(html.indexOf('>Cost</th>'));
    expect(html.indexOf('>Cost</th>')).toBeLessThan(html.indexOf('>Executor</th>'));
    expect(html).toContain('class="_requestEventsAPIKeyCell_');
    expect(html).toContain('title="Production Key">Production Key</td>');
    expect(html).toMatch(/<td class="[^"]*requestEventsNoWrapCell[^"]*">medium<\/td>/);
    expect(html).toContain('>Auto / Fast</td>');
    expect(html).toContain('>SSE</span>');
    expect(html).toContain('title="/messages">/messages</span>');
    expect(html).toContain('>120ms</span>');
    expect(html).toContain('>TTFT</span> 45ms</span>');
    expect(html).toMatch(/<td class="[^"]*requestEventsNoWrapCell[^"]*">30\.0 t\/s<\/td>/);
    expect(html).toMatch(/<td class="[^"]*requestEventsNoWrapCell[^"]*">28\.8 t\/s<\/td>/);
    expect(html).toContain('Loaded 1 / 120');
    expect(html).not.toContain('Previous');
    expect(html).not.toContain('Next');
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

    expect(html).toContain('>Auto / Fast</td>');
    expect(countOccurrences(html, '>Standard / Fast</td>')).toBe(2);
    expect(countOccurrences(html, '>Fast / Standard</td>')).toBe(2);
    expect(html).toContain('>Flex / Standard</td>');
    expect(html).toContain('>- / Fast</td>');
    expect(html).toContain('>batch / Standard</td>');
  });

  it('formats timestamps with compact numeric date and time', () => {
    const html = renderCard({
      events: [{ ...events[0], timestamp: '2026-05-13T00:38:19+08:00' }],
    });

    expect(html).toContain('>00:38:19</span>');
    expect(html).toContain('>2026/05/13</span>');
    expect(html).not.toContain('5/13/2026, 12:38:19 AM');
  });

  it('keeps TTFT visible inside Latency when TTFT is missing', () => {
    const html = renderCard({
      events: [{ ...events[0], ttft_ms: undefined, speed_tps: undefined }],
    });

    expect(html).toContain('>Latency</th>');
    expect(html).not.toContain('>TTFT</th>');
    expect(html).toContain('>TTFT</span> -</span>');
  });

  it('keeps the Latency column visible when latency is missing', () => {
    const html = renderCard({
      events: [{ ...events[0], latency_ms: undefined, speed_tps: undefined }],
    });

    expect(html.indexOf('>Latency</th>')).toBeLessThan(html.indexOf('title="Average output tokens per second after TTFT">Speed</th>'));
    expect(html).toContain('>--</span>');
    expect(html).toContain('>TTFT</span> 45ms</span>');
  });

  it('shows a dash for zero TTFT values', () => {
    const html = renderCard({
      events: [{ ...events[0], ttft_ms: 0, speed_tps: undefined }],
    });

    expect(html).toContain('>TTFT</span> -</span>');
  });

  it('maps GET endpoints to WS and strips the v1 prefix', () => {
    const html = renderCard({
      events: [{ ...events[0], endpoint: 'GET /v1/responses' }],
    });

    expect(html).toContain('>WS</span>');
    expect(html).toContain('title="/responses">/responses</span>');
  });

  it('strips the v1 prefix when endpoint has no request method', () => {
    const html = renderCard({
      events: [{ ...events[0], endpoint: '/v1/chat/completions' }],
    });

    expect(html).toContain('title="/chat/completions">/chat/completions</span>');
  });

  it('renders cache rate after cache read and write with two decimal places', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 100, cache_read_tokens: 25 } }],
    });

    expect(html.indexOf('>Tokens</th>')).toBeLessThan(html.indexOf('>Cache</th>'));
    expect(html).toContain('>25.00%</span>');
    expect(html).toContain('>Read</span> 25</span>');
    expect(html).toContain('>Write</span> 0</span>');
  });

  it('keeps cache rate based on normalized input for all providers', () => {
    const html = renderCard({
      events: [{
        ...events[0],
        source_type: 'claude',
        tokens: { ...events[0].tokens, input_tokens: 400, cache_read_tokens: 600, total_tokens: 500 },
      }],
    });

    expect(html).toContain('>150.00%</span>');
    expect(html).not.toContain('60.00%');
  });

  it('shows a dash for cache rate when input tokens are zero', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 0, cache_read_tokens: 25 } }],
    });

    expect(html).toContain('>Cache</th>');
    expect(html).toContain('>Read</span> 25</span>');
    expect(html).toContain('>Write</span> 0</span>');
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
      sourceOptions: [{ value: 'source-a', label: 'Provider A', displayName: 'Team Prefix' }, { value: 'source-b', label: 'Provider B' }],
    });

    expect(countOccurrences(html, 'Team Prefix')).toBeGreaterThanOrEqual(1);
    expect(html).toContain('aria-label="Source"><span class="_triggerText_c80422 ">Team Prefix</span>');
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
    expect(html).not.toContain('_requestEventsResultCompact_');
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

  it('renders request log content without request id or cache metadata', () => {
    const html = renderCard({
      requestLogResponse: {
        event_id: '101',
        request_id: 'req-log-101',
        filename: 'preview-req-log-101.log',
        available: true,
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
    expect(html).toContain('_requestEventsLogSectionChevron_');
    expect(html).toContain('_requestEventsLogSectionPanel_');
    expect(html).toContain('URL: /v1/responses');
    expect(html).not.toContain('Request ID');
    expect(html).not.toContain('Request ID: req-log-101');
    expect(html).not.toContain('<span>Cached</span>');
    expect(html).not.toContain('<span>Fresh</span>');
    expect(html).not.toContain('preview-req-log-101.log');
  });

  it('renders a compact large-log download prompt without opening log sections', () => {
    const html = renderCard({
      requestLogResponse: {
        event_id: '101',
        request_id: 'req-log-101',
        filename: 'large-request.log',
        available: true,
        previewable: false,
        too_large: true,
        downloadable: true,
        sections: [],
      },
      onRequestLogClose: () => undefined,
    });

    expect(html).toContain('Request Log Too Large');
    expect(html).toContain('Download Raw Log');
    expect(html).toContain('Cancel');
    expect(html).toContain('_requestEventsLargeLogModal_');
    expect(html).toContain('_requestEventsLargeLogPrompt_');
    expect(html).not.toContain('_requestEventsLogSections_');
  });

  it('keeps selected filters visible when backend options do not include them', () => {
    const html = renderCard({
      modelFilter: 'claude-haiku',
      sourceFilter: 'source-c',
    });

    expect(html).toContain('claude-haiku');
    expect(html).toContain('source-c');
  });


  it('shows total count in the title and uses the incremental loading footer', () => {
    const html = renderCard();

    expect(html).toContain('_requestEventsFiltersGroup_');
    expect(html).toContain('keeper-card-title-track');
    expect(html).toContain('_requestEventsCountBadge_');
    expect(html).toContain('120 total events');
    expect(html).toContain('_requestEventsPaginationFooter_');
    expect(html).toContain('_requestEventsPaginationControls_');
    expect(html).not.toContain('_requestEventsPageSizeControl_');
    expect(html).not.toContain('Rows per page');
    expect(html).not.toContain('<select');
    expect(html).toContain('_requestEventsPaginationPage_');
    expect(html).toContain('_requestEventsActions_');
    expect(html).not.toContain('_requestEventsPaginationItem_');
    expect(html).not.toContain('_requestEventsPageSizeSelectCompact_');
    expect(html).not.toContain('_usagePillShell_');
    expect(html).not.toContain('_requestEventsTableMeta_');
    expect(html).not.toContain('_requestEventsCountGroup_');
    expect(html).not.toContain('_requestEventsLimitHint_');
  });

  it('renders one export menu trigger instead of separate CSV and JSON buttons', () => {
    const html = renderCard({ modelFilter: 'claude-sonnet' });

    expect(html).toContain('Clear Filters');
    expect(countOccurrences(html, '>Export<')).toBe(1);
    expect(html.indexOf('aria-label="Result"')).toBeLessThan(html.indexOf('Clear Filters'));
    expect(html.indexOf('aria-label="Columns"')).toBeLessThan(html.indexOf('>Export<'));
    expect(html.indexOf('>Export<')).toBeLessThan(html.indexOf('aria-label="Result"'));
    expect(html).toContain('aria-haspopup="menu"');
    expect(countOccurrences(html, 'class="main-action-button-shell')).toBe(2);
    expect(countOccurrences(html, 'btn btn-primary btn-action main-action-button')).toBe(2);
    expect(html).not.toContain('_requestEventsExportButton_');
    expect(html).not.toContain('_requestEventsExportButtonInner_');
    expect(html).not.toContain('Export CSV');
    expect(html).not.toContain('Export JSON');
  });

  it('shows per-event cost returned by the backend', () => {
    const html = renderCard();

    expect(html).toContain('>Cost</th>');
    expect(html).toContain('$0.1234');
  });

  it('shows a dash when backend cost is unavailable', () => {
    const html = renderCard({
      events: [{ ...events[0], cost_usd: 0, cost_available: false }],
    });

    expect(html).toContain('>Cost</th>');
    expect(html).toContain('title="Set pricing to calculate cost"');
    expect(html).toMatch(/requestEventsStackedPrimary[^>]*>-<\/span>/);
  });

  it('renders the column settings trigger before Export', () => {
    const html = renderCard();

    expect(html).toContain('data-request-events-column-settings-trigger="true"');
    expect(html.indexOf('data-request-events-column-settings-trigger="true"')).toBeLessThan(html.indexOf('>Export<'));
    expect(html.indexOf('data-request-events-column-settings-trigger="true"')).toBeLessThan(html.indexOf('aria-label="Result"'));
    expect(html).not.toContain('_requestEventsColumnTrigger_');
  });

  it('can render only the selected request event columns', () => {
    const html = renderCard({
      initialVisibleColumnIds: ['timestamp', 'model', 'total_cost'],
    });

    expect(html).toContain('>Timestamp</th>');
    expect(html).toContain('>Model</th>');
    expect(html).toContain('>Cost</th>');
    expect(html).toContain('>02:00:00</span>');
    expect(html).toContain('>2026/04/23</span>');
    expect(html).toContain('<td class="_modelCell_');
    expect(html).toContain('$0.1234');
    expect(html).not.toContain('<th>API Key</th>');
    expect(html).not.toContain('<th>Source</th>');
    expect(html).not.toContain('>Latency</th>');
    expect(html).not.toContain('title="Production Key">Production Key</td>');
  });

  it('honors controlled request event column selection', () => {
    const html = renderCard({
      visibleColumnIds: ['timestamp', 'model'],
    });

    expect(html).toContain('>Timestamp</th>');
    expect(html).toContain('>Model</th>');
    expect(html).toContain('>02:00:00</span>');
    expect(html).toContain('>2026/04/23</span>');
    expect(html).toContain('<td class="_modelCell_');
    expect(html).not.toContain('<th>API Key</th>');
    expect(html).not.toContain('>Cost</th>');
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
  it('renders incremental loading status instead of page navigation in infinite mode', () => {
    const html = renderCard({
      hasMore: true,
      totalCount: 500,
      onLoadMore: () => undefined,
    });

    expect(html).toContain('Loaded 1 / 500');
    expect(html).toContain('Load more');
    expect(html).not.toContain('>Previous<');
    expect(html).not.toContain('>Next<');
    expect(html).toContain('aria-rowcount="501"');
  });

  it('preloads the next cursor batch before the scroller reaches the bottom', () => {
    expect(shouldLoadMoreRequestEvents({ scrollTop: 100, clientHeight: 600, scrollHeight: 2000 })).toBe(false);
    expect(shouldLoadMoreRequestEvents({ scrollTop: 400, clientHeight: 600, scrollHeight: 2000 })).toBe(true);
    expect(shouldLoadMoreRequestEvents({ scrollTop: 0, clientHeight: 0, scrollHeight: 0 })).toBe(false);
  });


  it('closes export menu only when focus leaves the menu container', () => {
    const insideTarget = {};
    const outsideTarget = {};
    const container = { contains: (target: EventTarget) => target === insideTarget };

    expect(shouldCloseMenuOnFocusLeave(container, insideTarget as EventTarget)).toBe(false);
    expect(shouldCloseMenuOnFocusLeave(container, outsideTarget as EventTarget)).toBe(true);
    expect(shouldCloseMenuOnFocusLeave(container, null)).toBe(true);
  });

});
