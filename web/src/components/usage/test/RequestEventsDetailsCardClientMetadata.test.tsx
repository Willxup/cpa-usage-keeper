// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const clientIP = '2001:0db8:85a3:0000:0000:8a2e:0370:7334';
const xForwardedFor = '192.0.2.1, 198.51.100.2, 203.0.113.3, 198.51.100.4';
const userAgent = 'Codex Desktop/0.146.0-alpha.3.1 (Mac OS 26.5.2; arm64) unknown (Codex Desktop; 26.721.81911)';

const baseEvent: UsageEvent = {
  id: '101',
  timestamp: '2026-07-29T02:00:00.000Z',
  api_key: 'Production Key',
  model: 'gpt-5',
  source: 'Provider A',
  source_raw: 'source-a',
  source_type: 'openai',
  auth_index: '1',
  failed: false,
  latency_ms: 120,
  ttft_ms: 45,
  speed_tps: 30,
  client_ip: clientIP,
  x_forwarded_for: xForwardedFor,
  user_agent: userAgent,
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
  pricing_style: 'openai',
};

const renderCardElement = (events: UsageEvent[]) => (
  <RequestEventsDetailsCard
    events={events}
    loading={false}
    totalCount={events.length}
    modelOptions={['gpt-5']}
    sourceOptions={[{ value: 'source-a', label: 'Provider A' }]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    visibleColumnIds={['client_ip', 'x_forwarded_for', 'user_agent']}
    columnOrder={['client_ip', 'x_forwarded_for', 'user_agent']}
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
  />
);

const mountCard = async (events: UsageEvent[]) => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => root.render(renderCardElement(events)));
  return {
    container,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
};

describe('RequestEventsDetailsCard client metadata columns', () => {
  it('limits displayed values while preserving a complete IPv6 address', async () => {
    const mounted = await mountCard([baseEvent]);

    try {
      const cells = Array.from(mounted.container.querySelectorAll('tbody td'));
      expect(cells.map((cell) => cell.textContent)).toEqual([
        clientIP,
        `${Array.from(xForwardedFor).slice(0, 48).join('')}...`,
        `${Array.from(userAgent).slice(0, 48).join('')}...`,
      ]);
      expect(cells.every((cell) => cell.getAttribute('title') === null)).toBe(true);
    } finally {
      await mounted.unmount();
    }
  });

  it('shows only the complete raw value in the shared custom tooltip', async () => {
    const mounted = await mountCard([baseEvent]);

    try {
      const cells = Array.from(mounted.container.querySelectorAll<HTMLTableCellElement>('tbody td'));
      for (const [cell, rawValue] of cells.map((cell, index) => (
        [cell, [clientIP, xForwardedFor, userAgent][index]] as const
      ))) {
        expect(cell.tabIndex).toBe(0);
        expect(cell.getAttribute('aria-label')).toBe(rawValue);

        await act(async () => {
          cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
        });
        const tooltip = document.body.querySelector('[role="tooltip"]');
        expect(tooltip?.textContent).toBe(rawValue);
        expect(tooltip?.querySelectorAll('span')).toHaveLength(1);

        await act(async () => {
          cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
        });
        expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
      }
    } finally {
      await mounted.unmount();
    }
  });

  it('does not attach a tooltip target to missing metadata', async () => {
    const mounted = await mountCard([{
      ...baseEvent,
      client_ip: null,
      x_forwarded_for: null,
      user_agent: null,
    }]);

    try {
      const cells = Array.from(mounted.container.querySelectorAll<HTMLTableCellElement>('tbody td'));
      expect(cells.map((cell) => cell.textContent)).toEqual(['-', '-', '-']);
      expect(cells.every((cell) => cell.tabIndex === -1)).toBe(true);

      await act(async () => {
        cells[0].dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      });
      expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
    } finally {
      await mounted.unmount();
    }
  });
});
