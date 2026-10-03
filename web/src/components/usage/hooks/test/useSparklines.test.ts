// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest';
import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { buildUsageSparklineSeries, useSparklines } from '../useSparklines';
import type { UsageOverviewPayload } from '../useUsageData';

const usageWithBackendSeries: UsageOverviewPayload = {
  usage: {
    total_requests: 9,
    success_count: 8,
    failure_count: 1,
    total_tokens: 900,
  },
  summary: {
    rpm: 0.075,
    tpm: 7.5,
    total_cost: 1,
    cost_available: true,
    input_tokens: 500,
    cache_read_tokens: 25,
    cache_creation_tokens: 5,
    reasoning_tokens: 0,
  },
  series: {
    buckets: ['2026-04-23T10:00:00Z', '2026-04-23T11:00:00Z'],
    requests: [2, 4],
    tokens: [200, 800],
    rpm: [2 / 60, 4 / 60],
    tpm: [200 / 60, 800 / 60],
    cost: [0.2, 0.8],
    cache_read_rate: [25, null],
  },
};

describe('buildUsageSparklineSeries', () => {
  it('prefers backend series over detail-derived fallback values', () => {
    const series = buildUsageSparklineSeries({
      usage: usageWithBackendSeries,
    });

    expect(series.labels).toEqual(['2026-04-23T10:00:00Z', '2026-04-23T11:00:00Z']);
    expect(series.requests).toEqual([2, 4]);
    expect(series.tokens).toEqual([200, 800]);
    expect(series.rpm).toEqual([2 / 60, 4 / 60]);
    expect(series.tpm).toEqual([200 / 60, 800 / 60]);
    expect(series.cost).toEqual([0.2, 0.8]);
    expect(series.cacheReadRate).toEqual([25, null]);
  });

  it('keeps cache rate empty when the backend omits a bucket cache rate', () => {
    const series = buildUsageSparklineSeries({
      usage: {
        ...usageWithBackendSeries,
        series: {
          ...usageWithBackendSeries.series!,
          buckets: ['2026-04-23T10:00:00Z'],
          requests: [1],
          cache_read_rate: [],
        },
      },
    });

    expect(series.cacheReadRate).toEqual([null]);
  });

  it('normalizes invalid sparkline series values to zero', () => {
    const invalidNumber = 'not-a-number' as unknown as number;
    const series = buildUsageSparklineSeries({
      usage: {
        ...usageWithBackendSeries,
        series: {
          ...usageWithBackendSeries.series!,
          buckets: ['2026-04-23T10:00:00Z'],
          requests: [invalidNumber],
          tokens: [-4],
          rpm: [Number.POSITIVE_INFINITY],
          tpm: [Number.NaN],
          cost: [invalidNumber],
          cache_read_rate: [invalidNumber],
        },
      },
    });

    expect(series.requests).toEqual([0]);
    expect(series.tokens).toEqual([0]);
    expect(series.rpm).toEqual([0]);
    expect(series.tpm).toEqual([0]);
    expect(series.cost).toEqual([0]);
    expect(series.cacheReadRate).toEqual([0]);
  });
});

async function renderSparklines(usage: UsageOverviewPayload) {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  function Harness() {
    const result = useSparklines({ usage, loading: false });
    return createElement('pre', null, JSON.stringify(result));
  }
  try {
    await act(async () => root.render(createElement(Harness)));
    return JSON.parse(container.textContent!) as ReturnType<typeof useSparklines>;
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
}

describe('useSparklines', () => {
  it('shows a marker for a single bucket without inventing historical samples', async () => {
    const usage = {
      ...usageWithBackendSeries,
      series: { ...usageWithBackendSeries.series!, buckets: ['2026-04-23T10:00:00Z'] },
    };
    const result = await renderSparklines(usage);
    const requests = result.requestsSparkline!.data;
    expect(requests.labels).toEqual(['2026-04-23T10:00:00Z']);
    expect(requests.datasets[0].data).toEqual([2]);
    expect(requests.datasets[0].pointRadius).toBe(2);
    expect(requests.datasets[0].pointBackgroundColor).toBe(requests.datasets[0].borderColor);
  });

  it('shows an isolated known cache-rate point while keeping full series as lines', async () => {
    const result = await renderSparklines(usageWithBackendSeries);
    expect(result.cacheReadRateSparkline!.data.datasets[0].data).toEqual([25, null]);
    expect(result.cacheReadRateSparkline!.data.datasets[0].pointRadius).toBe(2);
    expect(result.requestsSparkline!.data.datasets[0].pointRadius).toBe(0);
  });
});
