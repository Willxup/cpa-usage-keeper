// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RankingMetricSelect, RankingToolbar } from '../RankingToolbar';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      if (key === 'ranking.metric_ttft_average') {
        return 'This is the longest translated ranking metric';
      }
      if (key === 'ranking.metric_short_ttft_average') return 'TTFT';
      return key;
    },
  }),
}));

describe('RankingToolbar', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('renders the ranking period as one labelled select instead of four tabs', () => {
    const onPeriodChange = vi.fn();
    act(() => root.render(
      <RankingToolbar
        period="today"
        onPeriodChange={onPeriodChange}
      />,
    ));

    expect(container.querySelector('[data-ranking-toolbar]')).not.toBeNull();
    expect(container.querySelector('[data-ranking-periods]')).toBeNull();
    expect(container.querySelector('[data-ranking-metric]')).toBeNull();
    const periodTrigger = container.querySelector<HTMLButtonElement>('[data-ranking-period] button');
    expect(periodTrigger?.textContent).toContain('ranking.period_trigger_today');
    expect(periodTrigger?.getAttribute('aria-label')).toBe('ranking.period_trigger_today');
    act(() => periodTrigger?.click());
    const listbox = document.querySelector<HTMLElement>('[role="listbox"]');
    const yesterday = Array.from(listbox?.querySelectorAll('button') ?? [])
      .find((button) => button.textContent?.includes('ranking.period_yesterday'));
    act(() => (yesterday as HTMLButtonElement | undefined)?.click());
    expect(onPeriodChange).toHaveBeenCalledWith('yesterday');
  });

  it('includes the current metric in the collapsed select accessible name', () => {
    act(() => root.render(
      <RankingMetricSelect metric="overall" onMetricChange={vi.fn()} />,
    ));

    const metricTrigger = container.querySelector<HTMLButtonElement>('#ranking-metric-title');
    expect(metricTrigger?.getAttribute('aria-label')).toBe('ranking.metric_label: ranking.metric_short_overall');
  });
});
