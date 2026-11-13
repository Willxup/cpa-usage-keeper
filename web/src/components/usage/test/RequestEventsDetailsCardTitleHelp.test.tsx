// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import { RequestEventsTestCard } from './requestEventsFixtures';

const cases = [
  {
    language: 'en',
    title: 'Request Event Log',
    label: 'About request event log',
    count: '500 total events',
    help: 'Filter, inspect, and export request events from the most recent 90 days.',
  },
  {
    language: 'zh',
    title: '请求事件日志',
    label: '请求事件日志说明',
    count: '共 500 条事件',
    help: '筛选、查看并导出最近 90 天内的请求事件。',
  },
  {
    language: 'zh-TW',
    title: '請求事件記錄',
    label: '請求事件記錄說明',
    count: '共 500 筆事件',
    help: '篩選、查看並匯出最近 90 天內的請求事件。',
  },
] as const;

const dispatchPointer = async (target: Element, type: string, pointerType: string) => {
  await act(async () => {
    const event = new Event(type, { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'pointerType', { value: pointerType });
    target.dispatchEvent(event);
  });
};

describe('RequestEventsDetailsCard title help', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => {
      root.unmount();
      await i18n.changeLanguage('en');
    });
    container.remove();
  });

  it('keeps the title, accessible help and count in order without a subtitle in all languages', async () => {
    for (const testCase of cases) {
      await act(async () => {
        await i18n.changeLanguage(testCase.language);
        root.render(<RequestEventsTestCard events={[]} totalCount={500} />);
      });

      const heading = container.querySelector<HTMLHeadingElement>('.keeper-card-title')!;
      const button = container.querySelector<HTMLButtonElement>('[data-question-mark-help]')!;
      const titleMeta = container.querySelector<HTMLElement>('.keeper-card-title-meta')!;
      const count = Array.from(titleMeta.querySelectorAll('span'))
        .find((element) => element.textContent === testCase.count)!;
      expect(heading.textContent).toBe(testCase.title);
      expect(button).not.toBeNull();
      expect(button.getAttribute('aria-label')).toBe(testCase.label);
      expect(heading.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(button.compareDocumentPosition(count) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(container.querySelector('.keeper-card-subtitle')).toBeNull();
      expect(i18n.getResource(testCase.language, 'translation', 'usage_stats.request_events_help')).toBe(testCase.help);
      expect(document.getElementById(button.getAttribute('aria-describedby')!)?.textContent).toBe(testCase.help);
      const tooltip = document.getElementById(button.getAttribute('aria-controls')!)!;
      expect(tooltip.getAttribute('role')).toBe('tooltip');
      expect(tooltip.textContent).toBe(testCase.help);
      expect(tooltip.getAttribute('aria-hidden')).toBe('true');
    }
  });

  it('reveals help on hover, focus and touch and dismisses it appropriately', async () => {
    await act(async () => {
      await i18n.changeLanguage('en');
      root.render(<RequestEventsTestCard events={[]} totalCount={500} />);
    });
    const button = container.querySelector<HTMLButtonElement>('[data-question-mark-help]')!;
    expect(button).not.toBeNull();
    const tooltip = document.getElementById(button.getAttribute('aria-controls')!)!;

    expect(button.getAttribute('aria-expanded')).toBe('false');
    await act(async () => button.focus());
    expect(button.getAttribute('aria-expanded')).toBe('true');
    expect(tooltip.getAttribute('aria-hidden')).toBe('false');
    await act(async () => button.blur());
    expect(tooltip.getAttribute('aria-hidden')).toBe('true');

    await dispatchPointer(button, 'pointerdown', 'touch');
    expect(button.getAttribute('aria-expanded')).toBe('true');
    expect(tooltip.getAttribute('aria-hidden')).toBe('false');
    await dispatchPointer(document.body, 'pointerdown', 'touch');
    expect(button.getAttribute('aria-expanded')).toBe('false');
    expect(tooltip.getAttribute('aria-hidden')).toBe('true');

    await dispatchPointer(button, 'pointerover', 'mouse');
    expect(tooltip.getAttribute('aria-hidden')).toBe('false');
    await dispatchPointer(button, 'pointerout', 'mouse');
    await act(async () => new Promise((resolve) => setTimeout(resolve, 150)));
    expect(tooltip.getAttribute('aria-hidden')).toBe('true');
  });
});
