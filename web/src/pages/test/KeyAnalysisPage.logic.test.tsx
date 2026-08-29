// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import type { AnalysisLatencyDiagnostics, AnalysisResponse } from '@/lib/types';
import { serializeUsageRangeState } from '@/utils/usage/customRange';

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
  KeyViewerShell: ({ children, toolbar }: { children: React.ReactNode; toolbar: React.ReactNode }) => (
    <div>{toolbar}{children}</div>
  ),
}));

vi.mock('@/components/usage', () => ({
  AnalysisPanel: ({ analysis }: { analysis: AnalysisResponse | null }) => <div data-testid="analysis">{analysis?.timezone ?? 'empty'}</div>,
  TimeRangeControl: () => <div data-testid="range-control" />,
}));

vi.mock('@/hooks/useMediaQuery', () => ({ useMediaQuery: () => false }));
vi.mock('@/stores', () => ({
  useThemeStore: (selector: (state: { resolvedTheme: 'white' }) => unknown) => selector({ resolvedTheme: 'white' }),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { KeyAnalysisPage } from '../KeyAnalysisPage';

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
};

const deferred = <T,>(): Deferred<T> => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
};

const analysisResponse = (timezone: string): AnalysisResponse => ({
  granularity: 'hourly',
  timezone,
  token_usage: [],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_breakdown: {
    uncached_input_cost_usd: 0,
    cache_read_cost_usd: 0,
    cache_write_cost_usd: 0,
    output_cost_usd: 0,
    total_cost_usd: 0,
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
    const firstAnalysis = deferred<AnalysisResponse>();
    const secondAnalysis = deferred<AnalysisResponse>();
    const firstLatency = deferred<AnalysisLatencyDiagnostics>();
    const secondLatency = deferred<AnalysisLatencyDiagnostics>();
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
    expect(refreshButton).toBeDefined();
    await act(async () => {
      refreshButton?.click();
    });

    expect(firstSignal.aborted).toBe(true);
    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(2);
    await act(async () => {
      secondAnalysis.resolve(analysisResponse('new'));
      secondLatency.resolve(latencyResponse);
      await Promise.resolve();
    });
    expect(container.querySelector('[data-testid="analysis"]')?.textContent).toBe('new');

    await act(async () => {
      firstAnalysis.resolve(analysisResponse('old'));
      firstLatency.resolve(latencyResponse);
      await Promise.resolve();
    });
    expect(container.querySelector('[data-testid="analysis"]')?.textContent).toBe('new');
  });

  it('does not reload when the response timezone replaces a stored custom-range timezone', async () => {
    localStorage.setItem('cli-proxy-key-analysis-range-v1', serializeUsageRangeState({
      range: 'custom',
      customRange: { unit: 'day', start: '2026-08-20', end: '2026-08-21' },
      timeZone: 'America/New_York',
    }));
    const blockedAnalysis = deferred<AnalysisResponse>();
    const blockedLatency = deferred<AnalysisLatencyDiagnostics>();
    apiMocks.fetchKeyAnalysis
      .mockResolvedValueOnce(analysisResponse('Asia/Shanghai'))
      .mockReturnValue(blockedAnalysis.promise);
    apiMocks.fetchKeyAnalysisLatency
      .mockResolvedValueOnce(latencyResponse)
      .mockReturnValue(blockedLatency.promise);

    await act(async () => {
      root.render(<KeyAnalysisPage onNavigate={() => {}} />);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(apiMocks.fetchKeyAnalysis).toHaveBeenCalledTimes(1);
    expect(apiMocks.fetchKeyAnalysisLatency).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[data-testid="analysis"]')?.textContent).toBe('Asia/Shanghai');
  });

  it('returns to authentication when either viewer endpoint rejects the session', async () => {
    apiMocks.fetchKeyAnalysis.mockRejectedValue(new ApiError('expired', 401));
    apiMocks.fetchKeyAnalysisLatency.mockRejectedValue(new ApiError('expired', 401));
    const onAuthRequired = vi.fn();

    await act(async () => {
      root.render(<KeyAnalysisPage onNavigate={() => {}} onAuthRequired={onAuthRequired} />);
      await Promise.resolve();
    });

    expect(onAuthRequired).toHaveBeenCalled();
  });
});
