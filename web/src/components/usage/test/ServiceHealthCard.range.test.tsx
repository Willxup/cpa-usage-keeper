// @vitest-environment happy-dom

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import '@/i18n';
import type { UsageActivityResponse } from '@/lib/types';
import { ServiceHealthCard } from '../ServiceHealthCard';
import { buildUsageActivityFixture } from './activityFixtures';

const BLOCK_COUNT = 7 * 52;

function buildActivity(windowHours: number, columns = 52): UsageActivityResponse {
  const fixture = buildUsageActivityFixture();
  const windowStart = Date.parse('2026-07-01T00:00:00Z');
  const windowEnd = windowStart + windowHours * 60 * 60 * 1000;
  const bucketMilliseconds = (windowEnd - windowStart) / BLOCK_COUNT;
  return {
    ...fixture,
    window: windowHours === 24 ? 'day' : windowHours === 7 * 24 ? 'week' : 'month',
    grain: windowHours === 24 ? 'short' : windowHours === 7 * 24 ? 'medium' : 'long',
    timezone: 'UTC',
    total_success: 1,
    total_failure: 0,
    success_rate: 100,
    rows: 2,
    columns,
    bucket_seconds: Math.ceil(bucketMilliseconds / 1000),
    window_start: new Date(windowStart).toISOString(),
    window_end: new Date(windowEnd).toISOString(),
    blocks: fixture.blocks.map((block, index) => ({
      ...block,
      start_time: new Date(windowStart + index * bucketMilliseconds).toISOString(),
      end_time: new Date(windowStart + (index + 1) * bucketMilliseconds).toISOString(),
      success: index === BLOCK_COUNT - 1 ? 1 : 0,
      failure: 0,
      rate: index === BLOCK_COUNT - 1 ? 1 : -1,
    })),
  };
}

describe('ServiceHealthCard activity ranges', () => {
  it.each([
    ['Day', 24],
    ['Week', 7 * 24],
    ['Month', 30 * 24],
  ])('keeps %s at the fixed 7 by 52 grid', (_label, windowHours) => {
    // 后端 columns/rows 即使暂时异常，前端也不能缩减已经确认的 364 格布局。
    const html = renderToStaticMarkup(createElement(ServiceHealthCard, {
      activity: buildActivity(windowHours, 13),
      loading: false,
      requestIdentity: `admin::${windowHours}h:::`,
    }));

    expect(html.match(/role="gridcell"/g)).toHaveLength(BLOCK_COUNT);
    expect(html).toContain('aria-rowcount="7"');
    expect(html).toContain('aria-colcount="52"');
  });
});

describe('ServiceHealthCard range changes', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const renderActivity = (activity: UsageActivityResponse, requestIdentity: string) => {
    act(() => root.render(createElement(ServiceHealthCard, {
      activity,
      loading: false,
      requestIdentity,
    })));
  };

  it('clears tooltip state across an Activity request A to B to A transition', () => {
    renderActivity(buildActivity(24), 'admin::8h:::');
    const firstBlock = container.querySelector<HTMLElement>('[role="gridcell"]');
    expect(firstBlock).not.toBeNull();

    act(() => firstBlock!.focus());
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    renderActivity(buildActivity(7 * 24), 'admin::2d:::');
    expect(document.querySelector('[role="tooltip"]')).toBeNull();

    renderActivity(buildActivity(24), 'admin::8h:::');
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  it('keeps an open tooltip when the same Activity request refreshes', () => {
    const requestIdentity = 'admin::8h:::';
    renderActivity(buildActivity(24), requestIdentity);
    const firstBlock = container.querySelector<HTMLElement>('[role="gridcell"]');
    act(() => firstBlock!.focus());

    renderActivity(buildActivity(24), requestIdentity);

    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();
  });

  it('shows status totals and rate in a fixed tooltip', () => {
    renderActivity(buildUsageActivityFixture(), 'admin::day:::');

    const cells = container.querySelectorAll<HTMLElement>('[role="gridcell"]');
    act(() => cells.item(cells.length - 1).focus());

    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
    expect(tooltip!.textContent).toContain('OK 2');
    expect(tooltip!.textContent).toContain('Fail 1');
    expect(tooltip!.textContent).toContain('(66.7%)');
    expect(tooltip!.style.position).toBe('fixed');
    expect(tooltip!.style.bottom).toBe('auto');
    expect(tooltip!.style.right).toBe('auto');
  });
});
