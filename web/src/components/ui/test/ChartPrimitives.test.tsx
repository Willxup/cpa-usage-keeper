// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ChartSurface, MetricChip, SectionToolbar } from '../index';

describe('ChartSurface, SectionToolbar, and MetricChip primitives', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it('renders SectionToolbar heading, meta range, and action MetricChip', async () => {
    await act(async () => {
      root.render(
        <div>
          <SectionToolbar
            title="Recent Activity"
            headingId="activity-title"
            meta="10/11 00:00 – 10/18 20:00"
            actions={<MetricChip variant="stat" tone="up" label="Trend" value="+8.20%" />}
          />
          <ChartSurface data-surface="chart">Canvas</ChartSurface>
        </div>,
      );
    });

    expect(container.querySelector('#activity-title')?.textContent).toBe('Recent Activity');
    expect(container.textContent).toContain('10/11 00:00 – 10/18 20:00');
    expect(container.textContent).toContain('+8.20%');
    expect(container.querySelector('[data-surface="chart"]')?.textContent).toBe('Canvas');
  });
});
