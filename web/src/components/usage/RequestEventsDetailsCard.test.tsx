import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { RequestEventsDetailsCard } from './RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const events: UsageEvent[] = [
  {
    id: 101,
    timestamp: '2026-04-23T02:00:00.000Z',
    model: 'claude-sonnet',
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
    expect(html).toContain('Page (1/6)');
    expect(html).toContain('20');
    expect(html).toContain('50');
    expect(html).toContain('100');
    expect(html).toContain('500');
    expect(html).toContain('1000');
    expect(html).toContain('Previous');
    expect(html).toContain('Next');
    expect(html).toContain('disabled');
  });

  it('uses backend source values while showing resolved source labels', () => {
    const html = renderCard({ sourceFilter: 'source-a' });

    expect(countOccurrences(html, 'Provider A')).toBeGreaterThanOrEqual(2);
    expect(html).toContain('aria-label="Source"><span class="_triggerText_c80422 ">Provider A</span>');
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

    expect(html).toContain('Page (1/6)');
  });

  it('groups filters and pager controls in a compact toolbar', () => {
    const html = renderCard();

    expect(html).toContain('_requestEventsFiltersGroup_');
    expect(html).toContain('_requestEventsPaginationControls_');
    expect(html).toContain('_requestEventsPaginationItem_');
    expect(html).toContain('_requestEventsPagerButton_');
    expect(html).toContain('_requestEventsPageSizeSelectCompact_');
    expect(html).toContain('_requestEventsActions_');
    expect(html).toContain('_requestEventsTableMeta_');
  });

  it('hides export buttons while keeping clear filters available', () => {
    const html = renderCard({ modelFilter: 'claude-sonnet' });

    expect(html).toContain('Clear Filters');
    expect(html).not.toContain('Export CSV');
    expect(html).not.toContain('Export JSON');
  });
});
