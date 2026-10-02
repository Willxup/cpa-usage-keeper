// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import type { AnalysisLatencyDiagnostics, AnalysisResponse } from '@/lib/types';
import { serializeUsageRangeState } from '@/utils/usage/customRange';
import { KEY_VIEWER_TIME_RANGE_STORAGE_KEY } from '@/features/key-viewer/timeRange';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const apiMocks = vi.hoisted(() => ({
  fetchKeyAnalysis: vi.fn(),
  fetchKeyAnalysisLatency: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchKeyAnalysis: apiMocks.fetchKeyAnalysis,
  fetchKeyAnalysisLatency: apiMocks.fetchKeyAnalysisLatency,
}));

vi.mock('@/features/key-viewer/KeyViewerShell', () => ({
  KeyViewerShell: ({ children, filters, onRefresh, refreshing }: { children: React.ReactNode; filters: React.ReactNode[]; onRefresh: () => void; refreshing: boolean }) => (
    <div>{filters}<button type="button" onClick={onRefresh} disabled={refreshing}>usage_stats.refresh</button>{children}</div>
  ),
}));

vi.mock('@/components/usage', () => ({
  AnalysisPanel: ({ analysis, loading, latencyDiagnostics }: { analysis: AnalysisResponse | null; loading: boolean; latencyDiagnostics: AnalysisLatencyDiagnostics | null }) => <>
    <div data-testid="analysis">{analysis?.timezone ?? 'empty'}</div>
    <div data-testid="analysis-cost">{analysis?.cost_summary.total_cost_usd ?? 'missing'}</div>
    <div data-testid="analysis-loading">{String(loading)}</div>
    <div data-testid="latency-points">{latencyDiagnostics?.total_points ?? 'missing'}</div>
  </>,
  TimeRangeControl: ({ onChange }: { onChange: (range: '8h') => void }) => <button data-testid="range-control" type="button" onClick={() => onChange('8h')}>range</button>,
}));

vi.mock('@/hooks/useMediaQuery', () => ({ useMediaQuery: () => false }));
vi.mock('@/stores', async (original) => ({
  ...await original<typeof import('@/stores')>(),
  useThemeStore: (selector: (state: { resolvedTheme: 'white' }) => unknown) => selector({ resolvedTheme: 'white' }),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { KeyAnalysisPage } from '../KeyAnalysisPage';

const analysisResponse = (timezone: string, cost = 0): AnalysisResponse => ({
  granularity: 'hourly',
  timezone,
  token_usage: [],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_summary: {
    total_cost_usd: cost,
    cost_available: true,
  },
  model_efficiency: [],
  heatmap: { api_keys: [], api_key_labels: {}, models: [], cells: [] },
});

const latencyResponse: AnalysisLatencyDiagnostics = {
  points: [],
  density: [],
  total_points: 0,
  sampled: false,
  p95_ttft_ms: 0,
  p95_latency_ms: 0,
  max_ttft_ms: 0,
  max_latency_ms: 0,
};

describe('KeyAnalysisPage requests', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    localStorage.clear();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    apiMocks.fetchKeyAnalysis.mockReset();
    apiMocks.fetchKeyAnalysisLatency.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('aborts the previous load and ignores its stale response after manual refresh', async () => {
    const firstAnalysis = Promise.withResolvers<AnalysisResponse>();
    const secondAnalysis = Promise.withResolvers<AnalysisResponse>();
    const firstLatency = Promise.withResolvers<AnalysisLatencyDiagnostics>();
    const secondLatency = Promise.withResolvers<AnalysisLatencyDiagnostics>();
    apiMocks.fetchKeyAnalysis
      .mockReturnValueOnce(firstAnalysis.promise)
      .mockReturnValueOnce(secondAnalysis.promise);
    apiMocks.fetchKeyAnalysisLatency
      .mockReturnValueOnce(firstLatency.promise)
      .mockReturnValueOnce(secondLatency.promise);

    await act(async () => {
      root.render(<KeyAnalysisPage onNavigate={() => {}} />);
    });
    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(1);
    const firstSignal = apiMocks.fetchKeyAnalysis.mock.calls[0][1] as AbortSignal;

    const refreshButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('usage_stats.refresh'));
    await act(async () => {
      refreshButton!.click();
    });

    expect(firstSignal.aborted).toBe(true);
    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(2);
    await act(async () => {
      secondAnalysis.resolve(analysisResponse('new'));
      secondLatency.resolve(latencyResponse);
    });
    expect(container.querySelector('[data-testid="analysis"]')?.textContent).toBe('new');

    await act(async () => {
      firstAnalysis.resolve(analysisResponse('old'));
      firstLatency.resolve(latencyResponse);
    });
    expect(container.querySelector('[data-testid="analysis"]')?.textContent).toBe('new');
  });

  it('does not reload when the response timezone replaces a stored custom-range timezone', async () => {
    localStorage.setItem(KEY_VIEWER_TIME_RANGE_STORAGE_KEY, serializeUsageRangeState({
      range: 'custom',
      customRange: { unit: 'day', start: '2026-08-20', end: '2026-08-21' },
      timeZone: 'America/New_York',
    }));
    const blockedAnalysis = Promise.withResolvers<AnalysisResponse>();
    const blockedLatency = Promise.withResolvers<AnalysisLatencyDiagnostics>();
    apiMocks.fetchKeyAnalysis
      .mockResolvedValueOnce(analysisResponse('Asia/Shanghai'))
      .mockReturnValue(blockedAnalysis.promise);
    apiMocks.fetchKeyAnalysisLatency
      .mockResolvedValueOnce(latencyResponse)
      .mockReturnValue(blockedLatency.promise);

    await act(async () => {
      root.render(<KeyAnalysisPage onNavigate={() => {}} />);
    });

    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(1);
    expect(apiMocks.fetchKeyAnalysisLatency).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[data-testid="analysis"]')?.textContent).toBe('Asia/Shanghai');
  });

  it.each(['fetchKeyAnalysis', 'fetchKeyAnalysisLatency'] as const)('returns to authentication when %s rejects the session', async (endpoint) => {
    apiMocks.fetchKeyAnalysis.mockResolvedValue(analysisResponse('UTC'));
    apiMocks.fetchKeyAnalysisLatency.mockResolvedValue(latencyResponse);
    apiMocks[endpoint].mockRejectedValue(new ApiError('expired', 401));
    const onAuthRequired = vi.fn();

    await act(async () => {
      root.render(<KeyAnalysisPage onNavigate={() => {}} onAuthRequired={onAuthRequired} />);
    });

    expect(onAuthRequired).toHaveBeenCalled();
  });

  it('shows a temporary busy notice without a zero-cost result and keeps latency independent on first load', async () => {
    apiMocks.fetchKeyAnalysis.mockRejectedValue(new ApiError('Cost statistics are being updated', 503, 'costs_busy'));
    apiMocks.fetchKeyAnalysisLatency.mockResolvedValue({ ...latencyResponse, total_points: 3 });
    const onAuthRequired = vi.fn();

    await act(async () => root.render(<KeyAnalysisPage onNavigate={() => {}} onAuthRequired={onAuthRequired} />));

    expect(container.textContent).toContain('key_analysis.costs_busy');
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('missing');
    expect(container.querySelector('[data-testid="analysis-loading"]')?.textContent).toBe('true');
    expect(container.querySelector('[data-testid="latency-points"]')?.textContent).toBe('3');
    expect(onAuthRequired).not.toHaveBeenCalled();
  });

  it('keeps the same-range cost through a busy refresh, then replaces it on the next success', async () => {
    apiMocks.fetchKeyAnalysis.mockResolvedValueOnce(analysisResponse('UTC', 4.25))
      .mockRejectedValueOnce(new ApiError('busy', 503, 'costs_busy'))
      .mockResolvedValueOnce(analysisResponse('UTC', 7.5));
    apiMocks.fetchKeyAnalysisLatency.mockResolvedValue(latencyResponse);
    const onAuthRequired = vi.fn();
    await act(async () => root.render(<KeyAnalysisPage onNavigate={() => {}} onAuthRequired={onAuthRequired} />));
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('4.25');

    const refresh = () => Array.from(container.querySelectorAll('button')).find((button) => button.textContent === 'usage_stats.refresh')!.click();
    await act(async () => refresh());
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('4.25');
    expect(container.textContent).toContain('key_analysis.costs_busy');
    expect(onAuthRequired).not.toHaveBeenCalled();

    await act(async () => refresh());
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('7.5');
    expect(container.textContent).not.toContain('key_analysis.costs_busy');
    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(3);
  });

  it('does not show another range or account cost when that request is busy', async () => {
    apiMocks.fetchKeyAnalysis.mockResolvedValueOnce(analysisResponse('UTC', 4.25))
      .mockRejectedValue(new ApiError('busy', 503, 'costs_busy'));
    apiMocks.fetchKeyAnalysisLatency.mockResolvedValue(latencyResponse);
    const firstKey = { display_key: 'key-one' };
    await act(async () => root.render(<KeyAnalysisPage apiKey={firstKey} onNavigate={() => {}} />));
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('4.25');

    await act(async () => container.querySelector('[data-testid="range-control"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('missing');
    expect(container.textContent).toContain('key_analysis.costs_busy');

    await act(async () => root.render(<KeyAnalysisPage apiKey={{ display_key: 'key-two' }} onNavigate={() => {}} />));
    expect(container.querySelector('[data-testid="analysis-cost"]')?.textContent).toBe('missing');
    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(3);
  });
});
