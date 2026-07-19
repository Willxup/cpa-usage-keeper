// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
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

const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();

const extractSpeedModeCells = (html: string) => (
  Array.from(
    html.matchAll(/<tr\b[^>]*data-row-key="[^"]+"[^>]*>\s*<td\b[^>]*>(.*?)<\/td>\s*<\/tr>/gs),
    (match) => textFromMarkup(match[1]),
  )
);

const findSpeedModeTarget = (container: HTMLElement) => (
  container.querySelector('tbody tr[data-row-key] td [tabindex="0"]') as HTMLElement | null
);

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

        const cell = findSpeedModeTarget(mounted.container);
        expect(cell).toBeInstanceOf(HTMLElement);
        expect(cell?.getAttribute('title')).toBeNull();

        await act(async () => {
          cell?.focus();
        });

        const tooltip = document.body.querySelector('[role="tooltip"]');
        expect(tooltip).not.toBeNull();
        expect(Array.from(tooltip?.querySelectorAll('span') ?? [], (line) => line.textContent)).toEqual([
          requestLine,
          responseLine,
        ]);

        await act(async () => {
          cell?.blur();
        });
      }
    } finally {
      await mounted.unmount();
      await i18n.changeLanguage('en');
    }
  }, 15_000);

  it('shows mapped request and response modes in one column', async () => {
    await i18n.changeLanguage('en');
    const html = renderCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
      { ...baseEvent, id: '102', service_tier: 'standard', response_service_tier: 'standard' },
    ]);

    expect(REQUEST_EVENT_COLUMN_IDS).not.toContain('response_service_tier');
    expect(html).toContain('>Speed Mode</th>');
    expect(html).not.toContain('>Response Speed Mode</th>');
    expect(extractSpeedModeCells(html)).toEqual(['Auto / Standard', 'Standard / Standard']);
    expect(html).not.toContain('title="Speed Mode: Auto\nResponse Speed Mode: Standard"');
    expect(html).toContain('_requestEventsSpeedModeValue_');
  });

  it('keeps the Ant Design tooltip available while the value remains focused', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
    ]);

    try {
      const cell = findSpeedModeTarget(mounted.container) as HTMLElement;
      await act(async () => cell.focus());
      const describedBy = cell.getAttribute('aria-describedby');
      expect(describedBy).toBeTruthy();
      expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Speed Mode: Auto (auto)');

      await act(async () => {
        cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
        cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
      });
      expect(cell.getAttribute('aria-describedby')).toBe(describedBy);
    } finally {
      await mounted.unmount();
    }
  });

  it('exposes expanded values through the Ant Design tooltip description', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
    ]);

    try {
      const cell = findSpeedModeTarget(mounted.container) as HTMLElement;
      await act(async () => cell.focus());
      const describedBy = cell.getAttribute('aria-describedby');
      expect(describedBy).toBeTruthy();
      expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(
        'Speed Mode: Auto (auto)Response Speed Mode: Standard (default)',
      );
      expect(cell.getAttribute('aria-label')).toBeNull();
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps the Ant Design tooltip attached across table scroll and viewport changes', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([
      { ...baseEvent, service_tier: 'auto', response_service_tier: 'default' },
    ]);

    try {
      const cell = findSpeedModeTarget(mounted.container) as HTMLElement;
      await act(async () => cell.focus());
      const describedBy = cell.getAttribute('aria-describedby');
      expect(describedBy).toBeTruthy();

      await act(async () => {
        cell.closest('.ant-table-body')?.dispatchEvent(new Event('scroll'));
        window.dispatchEvent(new Event('resize'));
      });

      expect(cell.getAttribute('aria-describedby')).toBe(describedBy);
      expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();
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
    expect(html).not.toContain('(-)');
  });
});
