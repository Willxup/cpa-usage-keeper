// @vitest-environment happy-dom
import { act, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import { Line } from 'react-chartjs-2';
import { useSparklines, type SparklineBundle } from '../useSparklines';
import type { UsageOverviewPayload } from '../useUsageData';
import { sparklineOptions } from '@/utils/usage/chartConfig';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-chartjs-2', () => ({ Line: () => <canvas /> }));

it('keeps the same chart and data while loading and updates it without animation', async () => {
  let bundle: SparklineBundle | null = null;
  const usage = { series: { buckets: ['2026-10-03T10:00:00Z'], requests: [2] } } as UsageOverviewPayload;
  function Chart({ loading, data }: { loading: boolean; data: UsageOverviewPayload | null }) {
    const next = useSparklines({ usage: data, loading }).requestsSparkline;
    useEffect(() => { bundle = next; }, [next]);
    return next ? <Line data={next.data} options={sparklineOptions} /> : null;
  }
  const container = document.createElement('div');
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Chart loading data={null} />));
    expect(container.querySelector('canvas')).toBeNull();
    await act(async () => root.render(<Chart loading={false} data={usage} />));
    const canvas = container.querySelector('canvas');
    const oldBundle = bundle;
    await act(async () => root.render(<Chart loading data={usage} />));
    expect(container.querySelector('canvas')).toBe(canvas);
    expect(bundle).toBe(oldBundle);
    // A failed refresh retains the previous data too.
    await act(async () => root.render(<Chart loading={false} data={usage} />));
    expect(bundle).toBe(oldBundle);
    await act(async () => root.render(<Chart loading={false} data={{ ...usage, series: { ...usage.series!, requests: [3] } }} />));
    expect(container.querySelector('canvas')).toBe(canvas);
    expect((bundle as SparklineBundle | null)?.data.datasets[0].data).toEqual([3]);
    expect(sparklineOptions.animation).toBe(false);
  } finally { await act(async () => root.unmount()); }
});
