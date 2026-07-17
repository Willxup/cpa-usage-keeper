// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import {
  REQUEST_EVENT_COLUMN_IDS,
  RequestEventsDetailsCard,
} from '../RequestEventsDetailsCard';
import i18n from '@/i18n';
import type { UsageEvent } from '@/lib/types';

const baseEvent: UsageEvent = {
  id: '101',
  timestamp: '2026-04-23T02:00:00.000Z',
  api_key: 'Production Key',
  model: 'claude-sonnet',
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
};

const renderCardElement = (events: UsageEvent[]) => (
  <RequestEventsDetailsCard
    events={events}
    loading={false}
    page={1}
    pageSize={20}
    pageSizeOptions={[20, 50, 100, 500, 1000]}
    totalCount={events.length}
    totalPages={1}
    modelOptions={['claude-sonnet']}
    sourceOptions={[{ value: 'source-a', label: 'Provider A' }]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    visibleColumnIds={['service_tier']}
    onPageChange={() => undefined}
    onPageSizeChange={() => undefined}
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
  />
);

const renderCard = (events: UsageEvent[]) => renderToStaticMarkup(renderCardElement(events));

const mountCard = async (events: UsageEvent[]) => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => root.render(renderCardElement(events)));
  return {
    container,
    root,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
};

const extractSpeedModeCells = (html: string) => (
  Array.from(html.matchAll(/<tr><td\b[^>]*>(.*?)<\/td><\/tr>/gs), (match) => match[1])
);

const rectAt = (left: number, top: number, width = 40, height = 20): DOMRect => ({
  x: left,
  y: top,
  left,
  top,
  right: left + width,
  bottom: top + height,
  width,
  height,
  toJSON: () => ({}),
});

describe('RequestEventsDetailsCard Speed Mode column', () => {
  it('shows an immediate localized tooltip with mapped and raw request and response modes', async () => {
    const events = [{ ...baseEvent, service_tier: 'auto', response_service_tier: 'default' }];
    const mounted = await mountCard(events);
    const localizedLines = [
      ['en', 'Speed Mode: Auto (auto)', 'Response Speed Mode: Standard (default)'],
      ['zh', '速度模式：自动 (auto)', '响应速度模式：标准 (default)'],
      ['zh-TW', '速度模式：自動 (auto)', '回應速度模式：標準 (default)'],
    ] as const;

    try {
      for (const [language, requestLine, responseLine] of localizedLines) {
        await act(async () => {
          await i18n.changeLanguage(language);
          mounted.root.render(renderCardElement(events));
        });

        const cell = mounted.container.querySelector('tbody td');
        expect(cell).toBeInstanceOf(HTMLTableCellElement);
        expect(cell?.getAttribute('title')).toBeNull();

        await act(async () => {
          cell?.dispatchEvent(new MouseEvent('mouseover', {
            bubbles: true,
            clientX: 120,
            clientY: 80,
          }));
        });

        const tooltip = document.body.querySelector('[role="tooltip"]');
        expect(tooltip).not.toBeNull();
        expect(Array.from(tooltip?.querySelectorAll('span') ?? [], (line) => line.textContent)).toEqual([
          requestLine,
          responseLine,
        ]);

        await act(async () => {
          cell?.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
        });
        expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
      }
    } finally {
      await mounted.unmount();
      await i18n.changeLanguage('en');
    }
  });

  it('shows mapped request and response modes in one column', () => {
    const html = renderCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
      { ...baseEvent, id: '102', service_tier: 'standard', response_service_tier: 'standard' },
    ]);

    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('response_service_tier');
    expect(html).toContain('>Speed Mode</th>');
    expect(html).not.toContain('>Response Speed Mode</th>');
    expect(extractSpeedModeCells(html)).toEqual(['Auto / Standard', 'Standard / Standard']);
    expect(html).not.toContain('title="Speed Mode: Auto\nResponse Speed Mode: Standard"');
    expect(html).toContain('aria-label="Speed Mode: Auto (auto); Response Speed Mode: Standard (default)"');
  });

  it('keeps the tooltip open while focus remains after the mouse leaves', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
    ]);

    try {
      const cell = mounted.container.querySelector('tbody td') as HTMLTableCellElement;
      await act(async () => cell.focus());
      expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

      await act(async () => {
        cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
        cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
      });
      expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

      await act(async () => {
        cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
        cell.blur();
      });
      expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

      await act(async () => {
        cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
      });
      expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
    } finally {
      await mounted.unmount();
    }
  });

  it('exposes the expanded values once without a duplicate description', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
    ]);

    try {
      const cell = mounted.container.querySelector('tbody td') as HTMLTableCellElement;
      await act(async () => cell.focus());
      expect(cell.getAttribute('aria-label')).toBe('Speed Mode: Auto (auto); Response Speed Mode: Standard (default)');
      expect(cell.getAttribute('aria-describedby')).toBeNull();
    } finally {
      await mounted.unmount();
    }
  });

  it('repositions the tooltip when its scroll container or viewport changes', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
    ]);

    try {
      const cell = mounted.container.querySelector('tbody td') as HTMLTableCellElement;
      let currentRect = rectAt(100, 50);
      vi.spyOn(cell, 'getBoundingClientRect').mockImplementation(() => currentRect);

      await act(async () => {
        cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      });
      const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLDivElement;
      expect(tooltip.style.left).toBe('148px');
      expect(tooltip.style.top).toBe('80px');

      currentRect = rectAt(300, 200);
      await act(async () => {
        mounted.container.querySelector('table')?.parentElement?.dispatchEvent(new Event('scroll'));
      });
      expect(tooltip.style.left).toBe('320px');
      expect(tooltip.style.top).toBe('230px');

      currentRect = rectAt(400, 300);
      await act(async () => window.dispatchEvent(new Event('resize')));
      expect(tooltip.style.left).toBe('420px');
      expect(tooltip.style.top).toBe('330px');
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps a dash for each missing request or response mode in the cell and tooltip', () => {
    const html = renderCard([
      { ...baseEvent, service_tier: 'priority', response_service_tier: '' },
      { ...baseEvent, id: '102', service_tier: '', response_service_tier: 'default' },
      { ...baseEvent, id: '103', service_tier: '', response_service_tier: '' },
    ]);

    expect(extractSpeedModeCells(html)).toEqual(['Fast / -', '- / Standard', '- / -']);
    expect(html).toContain('aria-label="Speed Mode: Fast (priority); Response Speed Mode: -"');
    expect(html).toContain('aria-label="Speed Mode: -; Response Speed Mode: Standard (default)"');
    expect(html).toContain('aria-label="Speed Mode: -; Response Speed Mode: -"');
    expect(html).not.toContain('(-)');
  });
});
