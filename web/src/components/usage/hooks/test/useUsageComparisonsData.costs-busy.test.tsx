// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import type { UsageTimeRange } from '@/lib/types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const apiMocks = vi.hoisted(() => ({ fetchUsageOverviewComparisons: vi.fn() }));
vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  ...apiMocks,
}));

import { useUsageComparisonsData } from '../useUsageComparisonsData';

const response = (cost: number) => ({ models: [{ key: 'model-a', cost }], api_keys: [] });

function Probe({ viewerIdentity, range = 'today', keyViewer = true }: { viewerIdentity: object; range?: UsageTimeRange; keyViewer?: boolean }) {
  const { comparisons, loading, error, loadComparisons } = useUsageComparisonsData({ keyViewer, viewerIdentity, range });
  return <div>
    <div data-testid="cost">{comparisons?.models[0]?.cost ?? 'missing'}</div>
    <div data-testid="loading">{String(loading)}</div>
    <div data-testid="error">{error}</div>
    <button type="button" onClick={() => void loadComparisons()}>refresh</button>
  </div>;
}

describe('Key Viewer comparison cost reads', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    apiMocks.fetchUsageOverviewComparisons.mockReset();
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('keeps same-range stored fees through costs_busy and hides them when the viewer or range changes', async () => {
    apiMocks.fetchUsageOverviewComparisons.mockResolvedValueOnce(response(4.25))
      .mockRejectedValue(new ApiError('busy', 503, 'costs_busy'));
    const firstViewer = { display_key: 'key-one' };
    await act(async () => root.render(<Probe viewerIdentity={firstViewer} />));
    expect(container.querySelector('[data-testid="cost"]')?.textContent).toBe('4.25');

    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    expect(container.querySelector('[data-testid="cost"]')?.textContent).toBe('4.25');
    expect(container.querySelector('[data-testid="error"]')?.textContent).toBe('COSTS_BUSY');
    expect(apiMocks.fetchUsageOverviewComparisons.mock.calls[1][1]).toMatchObject({ keyViewer: true });

    await act(async () => root.render(<Probe viewerIdentity={firstViewer} range="8h" />));
    expect(container.querySelector('[data-testid="cost"]')?.textContent).toBe('missing');
    await act(async () => root.render(<Probe viewerIdentity={{ display_key: 'key-two' }} range="8h" />));
    expect(container.querySelector('[data-testid="cost"]')?.textContent).toBe('missing');
    expect(apiMocks.fetchUsageOverviewComparisons).toHaveBeenCalledTimes(4);
  });

  it('does not synthesize a zero-cost comparison on the first busy response', async () => {
    apiMocks.fetchUsageOverviewComparisons.mockRejectedValue(new ApiError('busy', 503, 'costs_busy'));
    await act(async () => root.render(<Probe viewerIdentity={{ display_key: 'key-one' }} />));
    expect(container.querySelector('[data-testid="cost"]')?.textContent).toBe('missing');
    expect(container.querySelector('[data-testid="error"]')?.textContent).toBe('COSTS_BUSY');
  });

  it('does not expose the Viewer-only busy marker through the admin hook', async () => {
    apiMocks.fetchUsageOverviewComparisons.mockRejectedValue(new ApiError('busy', 503, 'costs_busy'));
    await act(async () => root.render(<Probe viewerIdentity={{}} keyViewer={false} />));
    expect(container.querySelector('[data-testid="error"]')?.textContent).toBe('busy');
  });
});
