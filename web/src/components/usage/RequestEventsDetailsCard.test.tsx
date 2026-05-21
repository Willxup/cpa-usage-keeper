import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { RequestEventsDetailsCard } from './RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const events: UsageEvent[] = [
  {
    id: '101',
    timestamp: '2026-04-23T02:00:00.000Z',
    model: 'claude-sonnet',
    reasoning_effort: 'medium',
    source: 'Provider A',
    source_raw: 'source-a',
    source_type: 'openai',
    auth_index: '1',
    failed: false,
    latency_ms: 120,
    tokens: {
      input_tokens: 100,
      output_tokens: 60,
      reasoning_tokens: 20,
      cached_tokens: 20,
      total_tokens: 200,
    },
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
      modelPrices={{}}
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
    expect(html).toContain('Reasoning Level');
    expect(html).toContain('<td>medium</td>');
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

  it('renders cache rate after cached tokens with two decimal places', () => {
    const html = renderCard({
      events: [{ ...events[0], tokens: { ...events[0].tokens, input_tokens: 100, cached_tokens: 25 } }],
    });

    expect(html.indexOf('<th>Cached</th>')).toBeLessThan(html.indexOf('<th>Cache Rate</th>'));
    expect(html.indexOf('<th>Cache Rate</th>')).toBeLessThan(html.indexOf('<th>Total Tokens</th>'));
    expect(html).toContain('<td>25</td><td>25.00%</td><td>200</td>');
  });

  it('uses Claude token semantics for cache rate', () => {
    const html = renderCard({
      events: [{
        ...events[0],
        source_type: 'claude',
        tokens: { ...events[0].tokens, input_tokens: 400, cached_tokens: 600, total_tokens: 500 },
      }],
    });

    expect(html).toContain('<td>600</td><td>60.00%</td><td>500</td>');
    expect(html).not.toContain('150.00%');
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

  it('shows per-event cost when model pricing exists', () => {
    const html = renderCard({
      modelPrices: {
        'claude-sonnet': { prompt: 15, completion: 75, cache: 1.5 },
      },
    });

    expect(html).toContain('Total Cost');
    expect(html).toContain('$0.0057');
  });
});
