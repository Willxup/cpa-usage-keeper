// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import { RequestEventsTestCard } from './requestEventsFixtures';
import i18n from '@/i18n';
import type { UsageEvent } from '@/lib/types';

const baseEvent: UsageEvent = {
  id: '101',
  timestamp: '2026-04-23T02:00:00.000Z',
  model: 'claude-sonnet',
  source: 'Provider A',
  service_tier: 'auto',
  effective_service_tier: 'priority',
  response_service_tier: 'default',
  failed: false,
  tokens: { input_tokens: 100, output_tokens: 60, total_tokens: 160 },
  cost_usd: 0.1234,
  cost_available: true,
};

const renderCardElement = (events: UsageEvent[]) => (
  <RequestEventsTestCard events={events} visibleColumnIds={['service_tier']} />
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

const cellText = (html: string) => {
  const container = document.createElement('div');
  container.innerHTML = html;
  return Array.from(container.querySelectorAll('tbody td'), (cell) => cell.textContent);
};

describe('RequestEventsDetailsCard service tier column', () => {
  it('shows localized tier details visually and accessibly, with Effective first', async () => {
    const events = [{ ...baseEvent, effective_service_tier: 'ultrafast' }];
    const mounted = await mountCard(events);
    const localizedLines = [
      ['en', 'Ultrafast', 'Effective Speed Mode: Ultrafast (ultrafast)', 'Speed Mode: Auto (auto)', 'Response Speed Mode: Standard (default)'],
      ['zh', '超快', '有效速度模式：超快 (ultrafast)', '速度模式：自动 (auto)', '响应速度模式：标准 (default)'],
      ['zh-TW', '超高速', '有效速度模式：超高速 (ultrafast)', '速度模式：自動 (auto)', '回應速度模式：標準 (default)'],
    ] as const;

    try {
      for (const [language, displayValue, ...lines] of localizedLines) {
        await act(async () => {
          await i18n.changeLanguage(language);
          mounted.root.render(renderCardElement(events));
        });
        const cell = mounted.container.querySelector<HTMLTableCellElement>('tbody td')!;
        expect(cell.textContent).toBe(displayValue);
        await act(async () => {
          cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, clientX: 120, clientY: 80 }));
        });
        const tooltipText = document.body.querySelector('[role="tooltip"]')!.textContent!;
        expect(tooltipText.replace(/\s+/g, '')).toBe(lines.join('').replace(/\s+/g, ''));
        expect(cell.getAttribute('aria-label')).toBe(lines.join('; '));
        await act(async () => cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })));
        expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
      }
    } finally {
      await mounted.unmount();
      await i18n.changeLanguage('en');
    }
  });

  it('handles missing tiers without losing available effective, request, or response values', () => {
    const html = renderCard([
      { ...baseEvent, effective_service_tier: undefined, response_service_tier: '' },
      { ...baseEvent, id: '102', service_tier: '', effective_service_tier: '', response_service_tier: 'default' },
      { ...baseEvent, id: '103', service_tier: '', effective_service_tier: 'ultrafast', response_service_tier: '' },
      { ...baseEvent, id: '104', service_tier: undefined, effective_service_tier: ' ', response_service_tier: undefined },
      { ...baseEvent, id: '105', service_tier: 'fast', effective_service_tier: ' ', response_service_tier: 'default' },
    ]);
    expect(cellText(html)).toEqual(['Auto / -', '- / Standard', 'Ultrafast', '- / -', 'Fast / Standard']);
  });

  it('keeps the tooltip open while focus remains after the mouse leaves', async () => {
    await i18n.changeLanguage('en');
    const mounted = await mountCard([baseEvent]);
    try {
      const cell = mounted.container.querySelector<HTMLTableCellElement>('tbody td')!;
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
      await act(async () => cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })));
      expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps the tooltip anchored after scrolling or resizing', async () => {
    const mounted = await mountCard([baseEvent]);
    const cell = mounted.container.querySelector<HTMLTableCellElement>('tbody td')!;
    const initialRect = new DOMRect(300, 50, 40, 20);
    let currentRect = initialRect;
    const getRect = vi.spyOn(cell, 'getBoundingClientRect').mockImplementation(() => currentRect);
    try {
      await act(async () => cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
      const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLDivElement;
      const initialLeft = Number.parseFloat(tooltip.style.left);
      const initialTop = Number.parseFloat(tooltip.style.top);
      currentRect = new DOMRect(400, 200, 40, 20);
      await act(async () => mounted.container.dispatchEvent(new Event('scroll')));
      expect(Number.parseFloat(tooltip.style.left) - initialLeft).toBeCloseTo(currentRect.left - initialRect.left);
      expect(Number.parseFloat(tooltip.style.top) - initialTop).toBeCloseTo(currentRect.top - initialRect.top);
      currentRect = new DOMRect(500, 300, 40, 20);
      await act(async () => window.dispatchEvent(new Event('resize')));
      expect(Number.parseFloat(tooltip.style.left) - initialLeft).toBeCloseTo(currentRect.left - initialRect.left);
      expect(Number.parseFloat(tooltip.style.top) - initialTop).toBeCloseTo(currentRect.top - initialRect.top);
    } finally {
      getRect.mockRestore();
      await mounted.unmount();
    }
  });
});
