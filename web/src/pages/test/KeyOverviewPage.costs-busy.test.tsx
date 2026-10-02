// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import type { OverviewRealtimeBlock } from '@/lib/types';
import type { UsageOverviewPayload } from '@/components/usage/hooks/useUsageData';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const apiMocks = vi.hoisted(() => ({ fetchKeyOverview: vi.fn(), fetchKeyOverviewRealtime: vi.fn() }));
const hookMocks = vi.hoisted(() => ({ loadActivity: vi.fn(async () => {}), loadComparisons: vi.fn(async () => {}) }));

vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  ...apiMocks,
}));
vi.mock('@/features/key-viewer/KeyViewerShell', () => ({
  KeyViewerShell: ({ children, filters, onRefresh }: { children: React.ReactNode; filters: React.ReactNode[]; onRefresh: () => void }) =>
    <div>{filters}<button type="button" data-testid="refresh" onClick={onRefresh}>refresh</button>{children}</div>,
}));
vi.mock('@/components/usage/UsageComparisonCharts', () => ({ UsageComparisonCharts: () => <div data-testid="comparisons" /> }));
vi.mock('@/components/usage', () => ({
  StatCards: ({ usage, loading }: { usage: UsageOverviewPayload | null; loading: boolean }) =>
    <div data-testid="overview-cost">{loading ? 'loading' : usage?.summary?.total_cost ?? 0}</div>,
  OverviewRealtimePanel: ({ realtime, loading, error }: { realtime?: OverviewRealtimeBlock; loading: boolean; error: string }) => <>
    <div data-testid="realtime-cost">{loading && !realtime ? 'loading' : realtime?.current_usage.models[0]?.cost ?? 'missing'}</div>
    <div>{error}</div>
  </>,
  RecentActivityPanel: () => <div />,
  TimeRangeControl: ({ onChange }: { onChange: (range: '8h') => void }) =>
    <button data-testid="range-change" type="button" onClick={() => onChange('8h')}>range</button>,
  useRecentActivityWindow: () => ({ request: { window: 'today' }, manualWindow: null, setWindow: () => {} }),
  useUsageActivityData: () => ({ activity: null, activityMatchesRequest: false, loading: false, error: '', requestIdentity: 'today', loadActivity: hookMocks.loadActivity }),
  useUsageComparisonsData: () => ({ comparisons: null, loading: false, error: '', loadComparisons: hookMocks.loadComparisons }),
  useSparklines: () => ({ requestsSparkline: null, tokensSparkline: null, rpmSparkline: null, tpmSparkline: null, cacheReadRateSparkline: null, costSparkline: null }),
}));
vi.mock('@/hooks/useMediaQuery', () => ({ useMediaQuery: () => false }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

import { KeyOverviewPage } from '../KeyOverviewPage';

const overview = (cost: number) => ({
  timezone: 'UTC', usage: { total_requests: 1, success_count: 1, failure_count: 0, total_tokens: 100 },
  summary: { total_cost: cost, cost_available: true },
});
const realtime = (cost: number) => ({ window: '15m', current_usage: { models: [{ cost }] } });
const busy = () => new ApiError('Cost statistics are being updated', 503, 'costs_busy');

describe('KeyOverviewPage cost-read busy responses', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    localStorage.clear();
    apiMocks.fetchKeyOverview.mockReset();
    apiMocks.fetchKeyOverviewRealtime.mockReset();
    hookMocks.loadActivity.mockClear();
    hookMocks.loadComparisons.mockClear();
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
  });

  it('shows a temporary notice and no zero-cost overview before the first successful read', async () => {
    apiMocks.fetchKeyOverview.mockRejectedValue(busy());
    const onAuthRequired = vi.fn();

    await act(async () => root.render(<KeyOverviewPage onNavigate={() => {}} onAuthRequired={onAuthRequired} />));

    expect(container.textContent).toContain('key_overview.costs_busy');
    expect(container.querySelector('[data-testid="overview-cost"]')?.textContent).toBe('loading');
    expect(onAuthRequired).not.toHaveBeenCalled();
    expect(apiMocks.fetchKeyOverview).toHaveBeenCalledTimes(1);
  });

  it('keeps the same-range cost through busy refresh, then accepts the next successful value', async () => {
    apiMocks.fetchKeyOverview.mockResolvedValueOnce(overview(4.25)).mockRejectedValueOnce(busy()).mockResolvedValueOnce(overview(7.5));
    await act(async () => root.render(<KeyOverviewPage onNavigate={() => {}} />));
    expect(container.querySelector('[data-testid="overview-cost"]')?.textContent).toBe('4.25');

    await act(async () => container.querySelector<HTMLButtonElement>('[data-testid="refresh"]')!.click());
    expect(container.querySelector('[data-testid="overview-cost"]')?.textContent).toBe('4.25');
    expect(container.textContent).toContain('key_overview.costs_busy');

    await act(async () => container.querySelector<HTMLButtonElement>('[data-testid="refresh"]')!.click());
    expect(container.querySelector('[data-testid="overview-cost"]')?.textContent).toBe('7.5');
    expect(container.textContent).not.toContain('key_overview.costs_busy');
  });

  it('does not display an old range or account cost while the new read is busy', async () => {
    apiMocks.fetchKeyOverview.mockResolvedValueOnce(overview(4.25)).mockRejectedValue(busy());
    const firstKey = { display_key: 'key-one' };
    await act(async () => root.render(<KeyOverviewPage apiKey={firstKey} onNavigate={() => {}} />));
    await act(async () => container.querySelector<HTMLButtonElement>('[data-testid="range-change"]')!.click());
    expect(container.querySelector('[data-testid="overview-cost"]')?.textContent).toBe('loading');

    await act(async () => root.render(<KeyOverviewPage apiKey={{ display_key: 'key-two' }} onNavigate={() => {}} />));
    expect(container.querySelector('[data-testid="overview-cost"]')?.textContent).toBe('loading');
    expect(apiMocks.fetchKeyOverview).toHaveBeenCalledTimes(3);
  });

  it('retains realtime cost on busy refresh without treating it as an auth failure', async () => {
    apiMocks.fetchKeyOverviewRealtime.mockResolvedValueOnce(realtime(2.5)).mockRejectedValueOnce(busy());
    const onAuthRequired = vi.fn();
    await act(async () => root.render(<KeyOverviewPage page="realtime" onNavigate={() => {}} onAuthRequired={onAuthRequired} />));
    expect(container.querySelector('[data-testid="realtime-cost"]')?.textContent).toBe('2.5');

    await act(async () => container.querySelector<HTMLButtonElement>('[data-testid="refresh"]')!.click());
    expect(container.querySelector('[data-testid="realtime-cost"]')?.textContent).toBe('2.5');
    expect(container.textContent).toContain('key_overview.costs_busy');
    expect(onAuthRequired).not.toHaveBeenCalled();
  });

  it('uses a loading placeholder for the first busy realtime response', async () => {
    apiMocks.fetchKeyOverviewRealtime.mockRejectedValue(busy());
    await act(async () => root.render(<KeyOverviewPage page="realtime" onNavigate={() => {}} />));
    expect(container.querySelector('[data-testid="realtime-cost"]')?.textContent).toBe('loading');
    expect(container.textContent).toContain('key_overview.costs_busy');
  });
});
