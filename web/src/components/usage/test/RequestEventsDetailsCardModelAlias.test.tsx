import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const events: UsageEvent[] = [
  {
    id: '101',
    timestamp: '2026-04-23T02:00:00.000Z',
    api_key: 'Production Key',
    model: 'claude-sonnet',
    model_alias: 'sonnet-business',
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
    <RequestEventsDetailsCard
      events={events}
      loading={false}
      totalCount={1}
      modelOptions={['claude-sonnet']}
      sourceOptions={[{ value: 'source-a', label: 'Provider A' }]}
      modelFilter="__all__"
      sourceFilter="__all__"
      resultFilter="__all__"
      onModelFilterChange={() => undefined}
      onSourceFilterChange={() => undefined}
      onResultFilterChange={() => undefined}
      {...props}
    />,
  );

const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();

const extractTableHeaders = (html: string) =>
  Array.from(html.matchAll(/<th\b[^>]*>(.*?)<\/th>/gs), (match) => textFromMarkup(match[1]));

const extractFirstTableRowCells = (html: string) => {
  const row = html.match(/<tbody><tr>(.*?)<\/tr><\/tbody>/s)?.[1] ?? '';
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]));
};

describe('RequestEventsDetailsCard model stack', () => {
  it('shows model alias below model by default', () => {
    const html = renderCard();
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const modelHeaderIndex = headers.indexOf('Model');
    const effortHeaderIndex = headers.indexOf('Effort');

    expect(modelHeaderIndex).toBeGreaterThanOrEqual(0);
    expect(effortHeaderIndex).toBeGreaterThanOrEqual(0);
    expect(modelHeaderIndex).toBeLessThan(effortHeaderIndex);
    expect(headers).not.toContain('Model Alias');
    expect(cells[modelHeaderIndex]).toBe('claude-sonnetsonnet-business');
  });

  it('renders a dash when model alias is missing', () => {
    const html = renderCard({
      events: [{ ...events[0], model_alias: '' }],
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const modelHeaderIndex = headers.indexOf('Model');

    expect(modelHeaderIndex).toBeGreaterThanOrEqual(0);
    expect(cells[modelHeaderIndex]).toBe('claude-sonnet-');
  });

  it('renders a dash when model alias matches the model name', () => {
    const html = renderCard({
      events: [{ ...events[0], model_alias: 'claude-sonnet' }],
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const modelHeaderIndex = headers.indexOf('Model');

    expect(modelHeaderIndex).toBeGreaterThanOrEqual(0);
    expect(cells[modelHeaderIndex]).toBe('claude-sonnet-');
  });
});

describe('RequestEventsDetailsCard client metadata columns', () => {
  it('shows client metadata after the core request metrics', () => {
    const html = renderCard({
      events: [{
        ...events[0],
        client_ip: '192.0.2.10',
        x_forwarded_for: '203.0.113.5, 198.51.100.8',
        user_agent: 'test-client/1.0',
      }],
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    expect(headers.slice(-3)).toEqual([
      'Client IP',
      'X-Forwarded-For',
      'User Agent',
    ]);
    expect(cells.slice(-3)).toEqual([
      '192.0.2.10',
      '203.0.113.5, 198.51.100.8',
      'test-client/1.0',
    ]);
  });

  it('renders dashes for missing client metadata', () => {
    const html = renderCard();
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);

    for (const header of ['Client IP', 'X-Forwarded-For', 'User Agent']) {
      expect(cells[headers.indexOf(header)]).toBe('-');
    }
  });
});
