// @vitest-environment happy-dom
import { act, type ComponentProps } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { Chart, Plugin } from 'chart.js';
import type { OverviewRealtimeBlock } from '@/lib/types';
import { OverviewRealtimePanel } from '../OverviewRealtimePanel';

let speedProps: ComponentProps<typeof import('../model-speed/RealtimeModelSpeedCard').RealtimeModelSpeedCard>;
vi.mock('../model-speed/RealtimeModelSpeedCard', () => ({ RealtimeModelSpeedCard: (props: typeof speedProps) => { speedProps = props; return <section data-model-speed-card>Speed</section>; } }));
let linePlugins: Plugin<'line'>[];
vi.mock('react-chartjs-2', () => ({ Line: ({ plugins }: { plugins: Plugin<'line'>[] }) => { linePlugins = plugins; return <canvas />; }, Chart: () => <canvas />, Bar: () => <canvas />, Scatter: () => <canvas /> }));
vi.mock('@/i18n', () => ({ default: { language: 'en', t: (key: string) => key } }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
let root: ReturnType<typeof createRoot>;
let container: HTMLDivElement;
beforeEach(() => { container = document.createElement('div'); document.body.append(container); root = createRoot(container); });
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });
const response: OverviewRealtimeBlock = { window: '15m', bucket_seconds: 30, token_velocity: [], request_level: [], cache_level: [], current_usage: { models: [], api_keys: [], credentials: [], ai_providers: [] }, model_speed: { available: false, series: [] }, timezone: 'Asia/Shanghai' };

it('pairs model speed with Cache Level and forwards the existing response and viewer scope', async () => {
  await act(async () => root.render(<OverviewRealtimePanel realtime={response} loading={false} window="15m" onWindowChange={vi.fn()} isDark isMobile={false} modelSpeedPreferenceScope="key-viewer:42" modelSpeedQueryKey="15m:key42" />));
  const speed = container.querySelector('[data-model-speed-card]')!;
  expect(speed.nextElementSibling?.textContent).toContain('usage_stats.overview_realtime_cache_level');
  expect(speed.parentElement?.children).toHaveLength(2);
  expect(speedProps.speed).toBe(response.model_speed);
  expect(speedProps.timezone).toBe('Asia/Shanghai');
  expect(speedProps.preferenceScope).toBe('key-viewer:42');
  expect(speedProps.queryKey).toBe('15m:key42');
  await act(async () => root.render(<OverviewRealtimePanel realtime={response} loading window="15m" onWindowChange={vi.fn()} isDark isMobile={false} />));
  expect(speedProps.loading).toBe(false);
});

it('waits for the matching window response before giving candidates to a new speed query', async () => {
  await act(async () => root.render(<OverviewRealtimePanel realtime={response} loading={false} window="30m" onWindowChange={vi.fn()} isDark={false} isMobile={false} modelSpeedQueryKey="30m:key42" />));
  expect(speedProps.queryKey).toBe('30m:key42');
  expect(speedProps.speed).toBeUndefined();
  expect(speedProps.loading).toBe(true);
  const current = { ...response, window: '30m' as const };
  await act(async () => root.render(<OverviewRealtimePanel realtime={current} loading={false} window="30m" onWindowChange={vi.fn()} isDark={false} isMobile={false} modelSpeedQueryKey="30m:key42" />));
  expect(speedProps.speed).toBe(current.model_speed);
  expect(speedProps.loading).toBe(false);
});


it('uses complete responses for query and manual replay when model speed is omitted', async () => {
  const old = { ...response, model_speed: undefined };
  const current = { ...old, window: '30m' as const };
  const chart = { stop: vi.fn(), reset: vi.fn() };
  const draw = () => linePlugins.find(p => p.id === 'keeperChartLifecycle')!.beforeDatasetsUpdate!(chart as unknown as Chart<'line'>, { mode: 'default' }, {});
  const render = async (data: OverviewRealtimeBlock, window: '15m' | '30m', signal?: { source: object; revision: number }) => {
    await act(async () => root.render(<OverviewRealtimePanel realtime={data} loading={false} window={window} onWindowChange={vi.fn()} isDark={false} isMobile={false} chartRefreshSignal={signal} />));
    draw();
  };
  await render(old, '15m');
  expect(chart.reset).toHaveBeenCalledTimes(1);
  await render(old, '30m');
  expect(chart.reset).toHaveBeenCalledTimes(1);
  await render(current, '30m');
  expect(chart.reset).toHaveBeenCalledTimes(2);
  const automatic = { ...current };
  await render(automatic, '30m');
  expect(chart.reset).toHaveBeenCalledTimes(2);
  await render(automatic, '30m', { source: current, revision: 1 });
  expect(chart.reset).toHaveBeenCalledTimes(2);
  const manual = { ...current };
  await render(manual, '30m', { source: manual, revision: 2 });
  expect(chart.reset).toHaveBeenCalledTimes(3);
  expect(speedProps.refreshSignal).toBeUndefined();
  const withSpeed = { ...manual, model_speed: response.model_speed };
  await render(withSpeed, '30m', { source: withSpeed, revision: 3 });
  expect(speedProps.refreshSignal).toEqual({ source: withSpeed.model_speed, revision: 3 });
});
