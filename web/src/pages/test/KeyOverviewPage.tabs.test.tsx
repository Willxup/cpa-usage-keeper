// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { isKeyViewerPath, KEY_VIEWER_PAGE_PATHS } from '@/features/key-viewer/navigation';

const api = vi.hoisted(() => ({ fetchKeyOverview: vi.fn(), fetchKeyOverviewRealtime: vi.fn(), fetchKeyActivity: vi.fn() }));
vi.mock('@/lib/api', async (original) => ({ ...await original<typeof import('@/lib/api')>(), ...api }));
vi.mock('react-chartjs-2', () => ({ Bar: () => null, Chart: () => null, Doughnut: () => null, Line: () => null, Scatter: () => null }));
import { KeyOverviewPage } from '../KeyOverviewPage';

it('routes and loads only the selected Key Viewer tab and cancels inactive requests', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  localStorage.clear();
  Object.values(api).forEach((mock) => mock.mockReset().mockReturnValue(new Promise(() => undefined)));
  const container = document.createElement('div'); document.body.appendChild(container);
  const root = createRoot(container);
  try {
    expect(Object.keys(KEY_VIEWER_PAGE_PATHS).slice(0, 2)).toEqual(['overview', 'realtime']);
    expect(isKeyViewerPath('/key-realtime')).toBe(true);
    await act(async () => root.render(<KeyOverviewPage onNavigate={() => undefined} />));
    expect(api.fetchKeyOverview).toHaveBeenCalledOnce();
    expect(api.fetchKeyOverviewRealtime).not.toHaveBeenCalled();
    expect(api.fetchKeyActivity).toHaveBeenCalledOnce();
    const overviewSignal = api.fetchKeyOverview.mock.calls[0][1] as AbortSignal;
    await act(async () => root.render(<KeyOverviewPage page="realtime" onNavigate={() => undefined} />));
    expect(overviewSignal.aborted).toBe(true);
    expect(api.fetchKeyOverviewRealtime).toHaveBeenCalledOnce();
    expect(api.fetchKeyOverview).toHaveBeenCalledOnce();
    expect(api.fetchKeyActivity).toHaveBeenCalledOnce();
    expect(container.querySelector('[data-time-range-trigger]')).toBeNull();
    const realtimeSignal = api.fetchKeyOverviewRealtime.mock.calls[0][0].signal as AbortSignal;
    await act(async () => root.render(<KeyOverviewPage onNavigate={() => undefined} />));
    expect(realtimeSignal.aborted).toBe(true);
    expect(api.fetchKeyOverview).toHaveBeenCalledTimes(2);
    expect(api.fetchKeyOverviewRealtime).toHaveBeenCalledOnce();
  } finally { await act(async () => root.unmount()); container.remove(); localStorage.clear(); }
});
