// @vitest-environment happy-dom

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import '@/i18n';
import type { UsageActivityResponse } from '@/lib/types';
import { ServiceHealthCard } from '../ServiceHealthCard';

const BLOCK_COUNT = 7 * 52;

function buildActivity(windowHours: number, columns = 52): UsageActivityResponse {
  const windowStart = Date.parse('2026-07-01T00:00:00Z');
  const windowEnd = windowStart + windowHours * 60 * 60 * 1000;
  const bucketMilliseconds = (windowEnd - windowStart) / BLOCK_COUNT;
  return {
    window: windowHours === 24 ? '24h' : windowHours === 7 * 24 ? '7d' : '30d',
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
    blocks: Array.from({ length: BLOCK_COUNT }, (_, index) => ({
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
    ['24h', 24],
    ['Today', 24],
    ['Yesterday', 24],
    ['7d', 7 * 24],
    ['30d', 30 * 24],
    ['Custom 5h', 24],
    ['Custom 2d', 7 * 24],
    ['Custom 8d', 30 * 24],
  ])('keeps %s at the fixed 7 by 52 grid', (_label, windowHours) => {
    // 后端 columns/rows 即使暂时异常，前端也不能缩减已经确认的 364 格布局。
    const html = renderToStaticMarkup(createElement(ServiceHealthCard, {
      activity: buildActivity(windowHours, 13),
      loading: false,
      requestIdentity: `admin::${windowHours}h:::`,
    }));

    expect(html.match(/role="listitem"/g)).toHaveLength(BLOCK_COUNT);
    expect(html).toMatch(/--health-grid-columns:52(?:;|&quot;)/);
    expect(html).toMatch(/--health-grid-rows:7(?:;|&quot;)/);
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
    document.body.replaceChildren();
  });

  it('clears tooltip state across an Activity request A to B to A transition', () => {
    act(() => root.render(createElement(ServiceHealthCard, {
      activity: buildActivity(24),
      loading: false,
      requestIdentity: 'admin::8h:::',
    })));
    const firstBlock = container.querySelector<HTMLElement>('[role="listitem"]');
    expect(firstBlock).not.toBeNull();

    act(() => firstBlock?.focus());
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => root.render(createElement(ServiceHealthCard, {
      activity: buildActivity(7 * 24),
      loading: false,
      requestIdentity: 'admin::2d:::',
    })));
    expect(document.querySelector('[role="tooltip"]')).toBeNull();

    act(() => root.render(createElement(ServiceHealthCard, {
      activity: buildActivity(24),
      loading: false,
      requestIdentity: 'admin::8h:::',
    })));
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  it('keeps an open tooltip when the same Activity request refreshes', () => {
    const requestIdentity = 'admin::8h:::';
    act(() => root.render(createElement(ServiceHealthCard, {
      activity: buildActivity(24),
      loading: false,
      requestIdentity,
    })));
    const firstBlock = container.querySelector<HTMLElement>('[role="listitem"]');
    act(() => firstBlock?.focus());

    act(() => root.render(createElement(ServiceHealthCard, {
      activity: buildActivity(24),
      loading: false,
      requestIdentity,
    })));

    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();
  });
});
