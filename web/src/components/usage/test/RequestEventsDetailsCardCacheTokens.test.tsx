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
  const row = html.match(/<tbody><tr>(.*?)<\/tr><\/tbody>/s)?.[1] ?? '';
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]));
};

const renderCard = () => renderToStaticMarkup(
  <RequestEventsDetailsCard
    events={[event]}
    loading={false}
    totalCount={1}
    modelOptions={['gpt-5.6-terra']}
    sourceOptions={[{ value: 'openai', label: 'OpenAI' }]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
  />,
);

describe('RequestEventsDetailsCard cache token columns', () => {
  it('uses one Tokens column and one Cache column', () => {
    expect(REQUEST_EVENT_COLUMN_IDS).toContain('total_tokens');
    expect(REQUEST_EVENT_COLUMN_IDS).toContain('cache_read_rate');
    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('cache_read_tokens' as never);
    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('cache_creation_tokens' as never);
    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('cached_tokens');
    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('cache_rate');
    expect(REQUEST_EVENT_COLUMN_IDS.indexOf('cache_read_rate')).toBe(
      REQUEST_EVENT_COLUMN_IDS.indexOf('total_tokens') + 1,
    );
  });

  it('stacks read, write, and the calculated rate in Cache', () => {
    const html = renderCard();
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const tokensIndex = headers.indexOf('Tokens');
    const cacheIndex = headers.indexOf('Cache');

    expect(tokensIndex).toBeGreaterThanOrEqual(0);
    expect(cacheIndex).toBe(tokensIndex + 1);
    expect(cells[tokensIndex]).toBe('120Input 100Output 20 (Reasoning 5)');
    expect(cells[cacheIndex]).toBe('30.00%Read 30Write 10');
  });
});
