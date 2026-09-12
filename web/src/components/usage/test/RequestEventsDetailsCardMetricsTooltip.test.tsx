// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import type { UsageEvent } from '@/lib/types';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';

const baseEvent: UsageEvent = {
  id: 'metrics-tooltip-event',
  timestamp: '2026-08-31T02:00:00.000Z',
  api_key: 'Production Key',
  model: 'gpt-5',
  source: 'Provider A',
  source_raw: 'source-a',
  source_type: 'openai',
  auth_index: '1',
  failed: false,
  latency_ms: 120,
  ttft_ms: 45,
  tokens: {
    input_tokens: 100,
    output_tokens: 60,
    reasoning_tokens: 20,
    cache_read_tokens: 20,
    cache_creation_tokens: 5,
    total_tokens: 200,
  },
  cost_usd: 0.1234,
  cost_available: true,
  pricing_style: 'openai',
};

const largeTokenEvent: UsageEvent = {
  ...baseEvent,
  id: 'metrics-tooltip-large-event',
  tokens: {
    input_tokens: 73_893_802,
    output_tokens: 280_802,
    reasoning_tokens: 160_430,
    cache_read_tokens: 69_897_984,
    cache_creation_tokens: 0,
    total_tokens: 74_174_604,
  },
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
    visibleColumnIds={['total_tokens', 'cache_read_rate']}
    columnOrder={['total_tokens', 'cache_read_rate']}
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
    root,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
};

const tooltipLines = () => Array.from(
  document.body.querySelectorAll<HTMLElement>('[role="tooltip"] span'),
  (line) => line.textContent,
);

describe('RequestEventsDetailsCard token and cache tooltips', () => {
  it('shows all token and cache values in shared localized tooltips', async () => {
    const mounted = await mountCard([baseEvent]);

    try {
      const cells = mounted.container.querySelectorAll<HTMLTableCellElement>('tbody td');
      const tokensCell = cells[0];
      const cacheCell = cells[1];
      expect(tokensCell).toBeInstanceOf(HTMLTableCellElement);
      expect(cacheCell).toBeInstanceOf(HTMLTableCellElement);
      expect(tokensCell.tabIndex).toBe(0);
      expect(cacheCell.tabIndex).toBe(0);
      expect(tokensCell.getAttribute('title')).toBeNull();
      expect(cacheCell.getAttribute('title')).toBeNull();
      expect(tokensCell.querySelectorAll('[title]')).toHaveLength(0);
      expect(cacheCell.querySelectorAll('[title]')).toHaveLength(0);

      const localizedCases = [
        {
          language: 'en',
          tokenLines: ['Total Tokens: 200', 'Input: 100', 'Output: 60', 'Reasoning: 20'],
          cacheLines: ['Cache Rate: 20.00%', 'Cache Read: 20', 'Cache Write: 5'],
        },
        {
          language: 'zh',
          tokenLines: ['总 Token：200', '输入：100', '输出：60', '推理：20'],
          cacheLines: ['缓存率：20.00%', '缓存读取：20', '缓存写入：5'],
        },
        {
          language: 'zh-TW',
          tokenLines: ['總 Token：200', '輸入：100', '輸出：60', '推理：20'],
          cacheLines: ['快取率：20.00%', '快取讀取：20', '快取寫入：5'],
        },
      ] as const;

      for (const testCase of localizedCases) {
        await act(async () => {
          await i18n.changeLanguage(testCase.language);
          mounted.root.render(renderCardElement([baseEvent]));
        });
        const renderedCells = mounted.container.querySelectorAll<HTMLTableCellElement>('tbody td');
        const renderedTokensCell = renderedCells[0];
        const renderedCacheCell = renderedCells[1];

        expect(renderedTokensCell.getAttribute('aria-label')).toBe(testCase.tokenLines.join('; '));
        expect(renderedCacheCell.getAttribute('aria-label')).toBe(testCase.cacheLines.join('; '));

        await act(async () => {
          renderedTokensCell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
        });
        expect(tooltipLines()).toEqual(testCase.tokenLines);

        await act(async () => {
          renderedTokensCell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
          renderedCacheCell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
        });
        expect(tooltipLines()).toEqual(testCase.cacheLines);

        await act(async () => {
          renderedCacheCell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
        });
        expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
      }
    } finally {
      await mounted.unmount();
      await i18n.changeLanguage('en');
    }
  });

  it('compacts list values while keeping complete token and cache values in the tooltip', async () => {
    const mounted = await mountCard([largeTokenEvent]);

    try {
      const cells = mounted.container.querySelectorAll<HTMLTableCellElement>('tbody td');
      const tokensCell = cells[0];
      const cacheCell = cells[1];
      expect(tokensCell.textContent).toContain('74.17M');
      expect(tokensCell.textContent).toContain('73.89M');
      expect(tokensCell.textContent).toContain('280.80K');
      expect(tokensCell.textContent).toContain('160.43K');
      expect(cacheCell.textContent).toContain('94.59%');
      expect(cacheCell.textContent).toContain('69.90M');
      expect(cacheCell.textContent).toContain('0');
      expect(tokensCell.getAttribute('aria-label')).toContain('Total Tokens: 74,174,604');
      expect(cacheCell.getAttribute('aria-label')).toContain('Cache Read: 69,897,984');

      await act(async () => {
        tokensCell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      });
      expect(tooltipLines()).toEqual([
        'Total Tokens: 74,174,604',
        'Input: 73,893,802',
        'Output: 280,802',
        'Reasoning: 160,430',
      ]);

      await act(async () => {
        tokensCell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }));
        cacheCell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      });
      expect(tooltipLines()).toEqual([
        'Cache Rate: 94.59%',
        'Cache Read: 69,897,984',
        'Cache Write: 0',
      ]);
    } finally {
      await mounted.unmount();
    }
  });
});
