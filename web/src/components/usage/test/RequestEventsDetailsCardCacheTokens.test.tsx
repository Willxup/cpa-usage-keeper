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

const renderCard = (props: Partial<React.ComponentProps<typeof RequestEventsDetailsCard>> = {}) => renderToStaticMarkup(
  <RequestEventsDetailsCard
    events={[event]}
    loading={false}
    totalCount={1}
    modelOptions={['gpt-5.6-terra']}
    sourceOptions={[{ value: 'openai', label: 'OpenAI' }]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    apiKeyOptions={[]}
    apiKeyFilter=""
    onApiKeyFilterChange={() => undefined}
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
    {...props}
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
    expect(cells[tokensIndex]).toBe('120100205');
    expect(cells[cacheIndex]).toBe('30.00%3010');
    expect(html).toContain('data-token-direction="input"');
    expect(html).toContain('data-token-direction="output"');
    expect(html).toContain('data-token-direction="reasoning"');
    expect(html).toContain('data-token-flow="upload"');
    expect(html).toContain('data-token-flow="download"');
    expect(html).toContain('data-cache-operation="read"');
    expect(html).toContain('data-cache-operation="write"');
    expect(html).toContain('data-cache-flow="upload"');
    expect(html).toContain('data-cache-flow="download"');
    expect(html).not.toContain('data-cache-rate-tone=');
  });

  it('uses compact token units in cells while keeping full values in the cell labels', () => {
    const html = renderCard({
      events: [{
        ...event,
        tokens: {
          input_tokens: 1_234_567,
          output_tokens: 2_345_678,
          reasoning_tokens: 12_345,
          cache_read_tokens: 3_456_789,
          cache_creation_tokens: 4_567_890,
          total_tokens: 5_678_901,
        },
      }],
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const tokensIndex = headers.indexOf('Tokens');
    const cacheIndex = headers.indexOf('Cache');

    expect(cells[tokensIndex]).toBe('5.68M1.23M2.35M12.35K');
    expect(cells[cacheIndex]).toBe('280.00%3.46M4.57M');
    expect(html).toContain('aria-label="Total Tokens: 5,678,901; Input: 1,234,567; Output: 2,345,678; Reasoning: 12,345"');
    expect(html).toContain('aria-label="Cache Rate: 280.00%; Cache Read: 3,456,789; Cache Write: 4,567,890"');
  });
});
