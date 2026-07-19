import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import {
  REQUEST_EVENT_COLUMN_IDS,
  RequestEventsDetailsCard,
} from '../RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const event: UsageEvent = {
  id: 'cache-event',
  timestamp: '2026-07-10T08:00:00Z',
  api_key: 'Production Key',
  model: 'gpt-5.6-terra',
  source: 'OpenAI',
  source_raw: 'openai',
  source_type: 'openai',
  auth_index: '1',
  failed: false,
  latency_ms: 120,
  tokens: {
    input_tokens: 100,
    output_tokens: 20,
    reasoning_tokens: 5,
    cache_read_tokens: 30,
    cache_creation_tokens: 10,
    total_tokens: 120,
  },
  cost_usd: 0.1,
  cost_available: true,
  pricing_style: 'openai',
};

const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();

const extractTableHeaders = (html: string) => (
  Array.from(html.matchAll(/<th\b[^>]*>(.*?)<\/th>/gs), (match) => textFromMarkup(match[1]))
);

const extractFirstTableRowCells = (html: string) => {
  const row = html.match(/<tr\b[^>]*data-row-key="[^"]+"[^>]*>(.*?)<\/tr>/s)?.[1] ?? '';
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]));
};

const renderCard = () => renderToStaticMarkup(
  <RequestEventsDetailsCard
    events={[event]}
    loading={false}
    page={1}
    pageSize={20}
    pageSizeOptions={[20, 50, 100]}
    totalCount={1}
    totalPages={1}
    modelOptions={['gpt-5.6-terra']}
    sourceOptions={[{ value: 'openai', label: 'OpenAI' }]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    onPageChange={() => undefined}
    onPageSizeChange={() => undefined}
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
    initialVisibleColumnIds={['cache_read_tokens', 'cache_creation_tokens', 'cache_read_rate']}
  />,
);

describe('RequestEventsDetailsCard cache token columns', () => {
  it('uses cache read and cache write column ids instead of the legacy cached id', () => {
    expect(REQUEST_EVENT_COLUMN_IDS).toContain('cache_read_tokens');
    expect(REQUEST_EVENT_COLUMN_IDS).toContain('cache_creation_tokens');
    expect(REQUEST_EVENT_COLUMN_IDS).toContain('cache_read_rate');
    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('cached_tokens');
    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('cache_rate');
    expect(REQUEST_EVENT_COLUMN_IDS.indexOf('cache_read_tokens')).toBe(
      REQUEST_EVENT_COLUMN_IDS.indexOf('reasoning_tokens') + 1,
    );
    expect(REQUEST_EVENT_COLUMN_IDS.indexOf('cache_creation_tokens')).toBe(
      REQUEST_EVENT_COLUMN_IDS.indexOf('cache_read_tokens') + 1,
    );
  });

  it('renders read and write separately while calculating cache rate from cache read tokens', () => {
    const html = renderCard();
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const readIndex = headers.indexOf('Cache Read');
    const writeIndex = headers.indexOf('Cache Write');
    const rateIndex = headers.indexOf('Cache Rate');

    expect(readIndex).toBeGreaterThanOrEqual(0);
    expect(writeIndex).toBe(readIndex + 1);
    expect(rateIndex).toBe(writeIndex + 1);
    expect(cells[readIndex]).toBe('30');
    expect(cells[writeIndex]).toBe('10');
    expect(cells[rateIndex]).toBe('30.00%');
  });
});
