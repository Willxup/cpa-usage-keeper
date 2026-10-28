// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Chart, ChartData, ChartOptions, Plugin } from 'chart.js';
import type { AnalysisModelSpeedPayload } from '@/lib/types';
import { modelSpeedStorageKey } from '../modelSpeedSelection';
import { AnalysisModelSpeedCard } from '../AnalysisModelSpeedCard';

let captured: { data: ChartData<'scatter', { x: number; y: number | null }[]>; options: ChartOptions<'scatter'>; plugins: Plugin<'scatter'>[] };
vi.mock('react-chartjs-2', () => ({ Scatter: (props: typeof captured) => { captured = props; return <canvas data-speed-chart />; } }));
vi.mock('@/i18n', () => ({ default: { language: 'en', t: (key: string) => key } }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ i18n: { language: 'en' }, t: (key: string, values?: { samples: string }) => values ? `${key}: ${values.samples}` : key }) }));
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const speed: AnalysisModelSpeedPayload = {
  buckets: [{ start: '2026-10-08T00:00:00+08:00', end: '2026-10-08T03:00:00+08:00' }, { start: '2026-10-08T03:00:00+08:00', end: '2026-10-08T06:00:00+08:00' }],
  series: [
    { model: 'slow-model-full-name', total_tokens: 1000, speed_sample_count: 3, samples: [{ speed_tps_sum: 120, speed_sample_count: 3, mean_tps: 40 }, { speed_tps_sum: 0, speed_sample_count: 0, mean_tps: null }] },
    { model: 'fast-model-full-name', total_tokens: 100, speed_sample_count: 1, samples: [{ speed_tps_sum: 80, speed_sample_count: 1, mean_tps: 80 }, { speed_tps_sum: 0, speed_sample_count: 0, mean_tps: null }] },
    { model: 'no-speed-model', total_tokens: 2000, speed_sample_count: 0, samples: [{ speed_tps_sum: 0, speed_sample_count: 0, mean_tps: null }, { speed_tps_sum: 0, speed_sample_count: 0, mean_tps: null }] },
  ],
};

describe('Analysis model speed card', () => {
  let root: Root;
  let container: HTMLDivElement;
  beforeEach(() => { localStorage.clear(); container = document.createElement('div'); document.body.append(container); root = createRoot(container); });
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks(); });
  const render = async (response = speed, granularity: 'hourly' | 'daily' = 'hourly', options: { loading?: boolean; queryKey?: string; refreshSignal?: { source: object; revision: number } } = {}) => { await act(async () => root.render(<AnalysisModelSpeedCard speed={response} granularity={granularity} loading={options.loading ?? false} queryKey={options.queryKey} refreshSignal={options.refreshSignal} isDark={false} isMobile={false} timezone="Asia/Shanghai" />)); };
  const event = async (x: number, y = 100, type: 'mousemove' | 'click' | 'mouseout' = 'mousemove', plugin = captured.plugins[0], replay = false) => {
    const chart = { canvas: container.querySelector('canvas'), chartArea: { left: 0, right: 300, top: 0, bottom: 300 }, scales: { x: { getValueForPixel: (value: number) => value }, y: { getPixelForValue: () => 300 } }, getDatasetMeta: () => ({ data: [] }), getActiveElements: () => [], setActiveElements: vi.fn(), draw: vi.fn(), render: vi.fn() } as unknown as Chart<'scatter'>;
    await act(async () => plugin.afterEvent!(chart, { event: { type, x, y, native: null }, replay, inChartArea: true }, {}));
    return chart;
  };

  it('plots arithmetic means, null gaps, a shared zero-based axis and stable model colors', async () => {
    await render();
    expect(captured.data.datasets.map((dataset) => dataset.label)).toEqual(['no-speed-model', 'slow-model-full-name', 'fast-model-full-name']);
    expect(captured.data.datasets[0].data[0].y).toBeNull();
    expect(captured.data.datasets[1].data[0].y).toBeCloseTo(40);
    expect(captured.data.datasets[1].data[1].y).toBeNull();
    expect(captured.options.scales?.y?.min).toBe(0);
  });

  it('shows all selected models when hovering a point or empty space in its group and hides beneath the picker', async () => {
    await render();
    await event(0.3);
    const tooltip = document.querySelector<HTMLElement>('[data-model-speed-tooltip]')!;
    expect(tooltip.textContent).toContain('fast-model-full-name');
    expect(tooltip.textContent).toContain('slow-model-full-name');
    expect(tooltip.textContent).toContain('no-speed-model');
    expect(tooltip.textContent).toContain('usage_stats.no_data');
    expect(tooltip.textContent).toContain('80.0 t/s');
    expect(tooltip.textContent).toContain('00:00');
    expect(tooltip.textContent).toContain('03:00');
    expect(Array.from(tooltip.querySelectorAll('li strong:first-child'), (item) => item.textContent)).toEqual(['fast-model-full-name', 'slow-model-full-name', 'no-speed-model']);
    await event(1.25);
    expect(document.querySelector('[data-model-speed-tooltip]')!.querySelectorAll('li')).toHaveLength(3);
    expect(document.querySelector('[data-model-speed-tooltip]')!.textContent).not.toContain('t/s');
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    expect(document.querySelector('[data-model-speed-picker]')).not.toBeNull();
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
  });

  it('uses time-only hourly ticks and title, with emphasized speed beside the model and samples below', async () => {
    await render();
    const tick = captured.options.scales?.x?.ticks?.callback as (value: number) => string;
    expect([tick(0), tick(1)]).toEqual(['00:00', '03:00']);
    await event(0.3);
    const tooltip = document.querySelector<HTMLElement>('[data-model-speed-tooltip]')!;
    expect(tooltip.querySelector(':scope > strong')!.textContent).toBe('00:00 – 03:00');
    const value = tooltip.querySelector<HTMLElement>('[data-model-speed-value]')!;
    expect(value.textContent).toBe('80.0 t/s');
    expect(getComputedStyle(value).color).toBe('#111827');
    expect(tooltip.querySelector('li small')!.textContent).toBe('usage_stats.model_speed_samples: 1');
  });

  it('uses short month/day ticks and a date-only tooltip interval for daily response buckets', async () => {
    const daily = structuredClone(speed);
    daily.buckets = [
      { start: '2026-10-08T00:00:00+08:00', end: '2026-10-09T00:00:00+08:00' },
      { start: '2026-10-09T00:00:00+08:00', end: '2026-10-10T00:00:00+08:00' },
    ];
    await render(daily, 'daily');
    const tick = captured.options.scales?.x?.ticks?.callback as (value: number) => string;
    expect([tick(0), tick(1)]).toEqual(['10/8', '10/9']);
    await event(0.3);
    expect(document.querySelector('[data-model-speed-tooltip] > strong')!.textContent).toBe('10/8 – 10/9');
  });

  it('shows weighted whole-range means in the legend and keeps models with no samples unavailable', async () => {
    const unequal = structuredClone(speed);
    unequal.series[0].samples[1] = { speed_tps_sum: 80, speed_sample_count: 1, mean_tps: 80 };
    await render(unequal);
    const legend = container.querySelector('[data-model-speed-legend]')!;
    const rows = Array.from(legend.querySelectorAll('li'));
    expect(rows.find((row) => row.title === 'slow-model-full-name')!.textContent).toContain('50.0 t/s');
    expect(rows.find((row) => row.title === 'no-speed-model')!.textContent).toContain('usage_stats.no_data');
    expect(rows.find((row) => row.title === 'no-speed-model')!.textContent).not.toContain('0.0 t/s');
  });

  it('highlights a legend model across buckets, restores on leave and keeps missing samples absent', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({ matches: query === '(hover: hover) and (pointer: fine)', addEventListener: vi.fn(), removeEventListener: vi.fn() }) as unknown as MediaQueryList);
    const response = structuredClone(speed);
    response.series[0].samples[1] = { speed_tps_sum: 60, speed_sample_count: 1, mean_tps: 60 };
    await render(response);
    const chart = { stop: vi.fn(), reset: vi.fn() } as unknown as Chart<'scatter'>;
    const update = () => captured.plugins[0].beforeDatasetsUpdate!(chart, { mode: 'default', cancelable: true }, {});
    update();
    const baseline = captured.data.datasets.map((dataset) => ({ ...dataset }));
    const button = container.querySelector<HTMLButtonElement>('li[title="slow-model-full-name"] button')!;
    await event(0.3);
    await act(async () => button.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(button.dataset.highlighted).toBe('true');
    expect(captured.data.datasets[1].pointRadius).toBe(6);
    expect(captured.data.datasets[1].borderWidth).toBe(2.5);
    expect(captured.data.datasets[1].data.map((point) => point.y)).toEqual([40, 60]);
    expect(captured.data.datasets[2].backgroundColor).not.toEqual(baseline[2].backgroundColor);
    expect(captured.data.datasets[0].data.map((point) => point.y)).toEqual([null, null]);
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
    update();
    expect(chart.reset).toHaveBeenCalledTimes(1);
    await act(async () => button.dispatchEvent(new PointerEvent('pointerout', { bubbles: true, pointerType: 'mouse' })));
    expect(captured.data.datasets.map((dataset) => dataset.pointRadius)).toEqual([4, 4, 4]);
    expect(captured.data.datasets.map((dataset) => dataset.backgroundColor)).toEqual(baseline.map((dataset) => dataset.backgroundColor));
  });

  it('supports keyboard focus and toggle selection and clears selection when the query changes', async () => {
    await render();
    const button = container.querySelector<HTMLButtonElement>('li[title="slow-model-full-name"] button')!;
    await act(async () => button.focus());
    expect(captured.data.datasets[1].pointRadius).toBe(6);
    await act(async () => button.blur());
    expect(captured.data.datasets[1].pointRadius).toBe(4);
    await act(async () => button.click());
    expect(button.getAttribute('aria-pressed')).toBe('true');
    expect(captured.data.datasets[1].pointRadius).toBe(6);
    await act(async () => button.click());
    expect(button.getAttribute('aria-pressed')).toBe('false');
    expect(captured.data.datasets[1].pointRadius).toBe(4);
    await act(async () => button.click());
    await render(speed, 'hourly', { queryKey: 'different-range' });
    expect(button.getAttribute('aria-pressed')).toBe('false');
    expect(captured.data.datasets[1].pointRadius).toBe(4);
  });

  it('does not revive a pinned model when returning to the previous query', async () => {
    await render(speed, 'hourly', { queryKey: 'range-a' });
    const button = container.querySelector<HTMLButtonElement>('li[title="fast-model-full-name"] button')!;
    await act(async () => button.click());
    await render(speed, 'hourly', { queryKey: 'range-b' });
    await render(speed, 'hourly', { queryKey: 'range-a' });
    expect(button.getAttribute('aria-pressed')).toBe('false');
    expect(captured.data.datasets.every((dataset) => dataset.pointRadius === 4)).toBe(true);
  });

  it('does not revive a pinned model when it is removed and added back', async () => {
    await render();
    await act(async () => container.querySelector<HTMLButtonElement>('li[title="fast-model-full-name"] button')!.click());
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    const option = () => Array.from(document.querySelectorAll<HTMLButtonElement>('[data-model-speed-picker] [role="option"]')).find((item) => item.textContent === 'fast-model-full-name')!;
    await act(async () => option().click());
    await act(async () => option().click());
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })));
    expect(container.querySelector('li[title="fast-model-full-name"] button')!.getAttribute('aria-pressed')).toBe('false');
    expect(captured.data.datasets.every((dataset) => dataset.pointRadius === 4)).toBe(true);
  });

  it('keeps the tooltip dismissed on event replay after legend focus, until a real canvas event', async () => {
    await render();
    await event(0.3);
    expect(document.querySelector('[data-model-speed-tooltip]')).not.toBeNull();
    await act(async () => container.querySelector<HTMLButtonElement>('li[title="slow-model-full-name"] button')!.focus());
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
    await event(0.3, 100, 'mousemove', captured.plugins[0], true);
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
    await event(0.3);
    expect(document.querySelector('[data-model-speed-tooltip]')).not.toBeNull();
    await event(1.2, 100, 'mousemove', captured.plugins[0], true);
    expect(document.querySelector('[data-model-speed-tooltip] > strong')!.textContent).toBe('03:00 – 06:00');
  });

  it('starts pending style restoration when focusing the already pinned model', async () => {
    await render();
    const button = container.querySelector<HTMLButtonElement>('li[title="slow-model-full-name"] button')!;
    await act(async () => button.click());
    const chart = await event(0.3);
    const data = captured.data;
    await act(async () => button.focus());
    expect(captured.data).toBe(data);
    expect(chart.setActiveElements).toHaveBeenCalledWith([]);
    expect(chart.render).toHaveBeenCalledTimes(1);
  });

  it('keeps a pinned model on refresh but clears it when the visible model selection changes', async () => {
    await render();
    const button = container.querySelector<HTMLButtonElement>('li[title="slow-model-full-name"] button')!;
    await act(async () => button.click());
    await render(structuredClone(speed));
    expect(captured.data.datasets[1].pointRadius).toBe(6);
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    const option = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-model-speed-picker] [role="option"]')).find((item) => item.textContent === 'slow-model-full-name')!;
    await act(async () => option.click());
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })));
    expect(captured.data.datasets.map((dataset) => dataset.label)).toEqual(['no-speed-model', 'fast-model-full-name']);
    expect(captured.data.datasets.every((dataset) => dataset.pointRadius === 4)).toBe(true);
  });

  it('keeps the old canvas while refreshing and resets only for matched success or a new query', async () => {
    await render();
    const canvas = container.querySelector('canvas');
    const chart = { stop: vi.fn(), reset: vi.fn() } as unknown as Chart<'scatter'>;
    const update = () => captured.plugins[0].beforeDatasetsUpdate!(chart, { mode: 'default', cancelable: true }, {});
    update();
    expect(chart.reset).toHaveBeenCalledTimes(1);
    await render(speed, 'hourly', { loading: true });
    expect(container.querySelector('canvas')).toBe(canvas);
    update();
    expect(chart.reset).toHaveBeenCalledTimes(1);
    const refreshed = structuredClone(speed);
    await render(refreshed);
    update();
    expect(chart.reset).toHaveBeenCalledTimes(1);
    await render(refreshed, 'hourly', { refreshSignal: { source: speed, revision: 1 } });
    update();
    expect(chart.reset).toHaveBeenCalledTimes(1);
    await render(refreshed, 'hourly', { refreshSignal: { source: refreshed, revision: 2 } });
    update();
    expect(chart.reset).toHaveBeenCalledTimes(2);
    update();
    expect(chart.reset).toHaveBeenCalledTimes(2);
    await render(refreshed, 'hourly', { queryKey: 'changed-range' });
    update();
    update(); // resize/theme while the new query is pending
    expect(chart.reset).toHaveBeenCalledTimes(2);
    await render(structuredClone(refreshed), 'hourly', { queryKey: 'changed-range' });
    update();
    expect(chart.reset).toHaveBeenCalledTimes(3);
  });

  it('honors reduced motion without resetting the chart for a successful replay', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({ matches: query === '(prefers-reduced-motion: reduce)', media: query, addEventListener: vi.fn(), removeEventListener: vi.fn() }) as unknown as MediaQueryList);
    await render(speed, 'hourly', { refreshSignal: { source: speed, revision: 1 } });
    const chart = { stop: vi.fn(), reset: vi.fn() } as unknown as Chart<'scatter'>;
    captured.plugins[0].beforeDatasetsUpdate!(chart, { mode: 'default', cancelable: true }, {});
    expect(captured.options.animation).toBe(false);
    expect(chart.reset).not.toHaveBeenCalled();
  });

  it('highlights only a precisely hit stem or dot while retaining every grouped tooltip row', async () => {
    await render();
    let active: { datasetIndex: number; index: number }[] = [];
    const widths: number[] = [];
    const context = { lineWidth: 0, strokeStyle: '', save: vi.fn(), restore: vi.fn(), beginPath: vi.fn(), moveTo: vi.fn(), lineTo: vi.fn(), stroke: () => widths.push(context.lineWidth) };
    const metadata = [{ data: [{ x: 80, y: 300, skip: true }] }, { data: [{ x: 100, y: 180 }] }, { data: [{ x: 120, y: 60 }] }];
    const chart = { canvas: container.querySelector('canvas'), ctx: context, data: captured.data, chartArea: { left: 0, right: 300, top: 0, bottom: 300 }, scales: { x: { getValueForPixel: () => 0 }, y: { getPixelForValue: () => 300 } }, getDatasetMeta: (index: number) => metadata[index], getActiveElements: () => active, setActiveElements: (items: typeof active) => { active = items; }, draw: vi.fn(), render: vi.fn(), stop: vi.fn(), reset: vi.fn() } as unknown as Chart<'scatter'>;
    const move = async (x: number, y: number) => { await act(async () => captured.plugins[0].afterEvent!(chart, { event: { type: 'mousemove', x, y, native: null }, replay: false, inChartArea: true }, {})); };
    await move(122, 250);
    expect(active).toEqual([{ datasetIndex: 2, index: 0 }]);
    expect(document.querySelector('[data-model-speed-highlighted] strong')!.textContent).toBe('fast-model-full-name');
    expect(document.querySelectorAll('[data-model-speed-tooltip] li')).toHaveLength(3);
    captured.plugins[0].beforeDatasetsDraw!(chart, { cancelable: true }, {});
    expect(widths).toEqual([1.5, 3]);
    await move(105, 250);
    expect(active).toEqual([]);
    expect(document.querySelector('[data-model-speed-highlighted]')).toBeNull();
    expect(document.querySelectorAll('[data-model-speed-tooltip] li')).toHaveLength(3);
    await move(80, 250);
    expect(active).toEqual([]);
    await move(100, 179);
    expect(active).toEqual([{ datasetIndex: 1, index: 0 }]);
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    expect(active).toEqual([]);
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
  });

  it('keeps the registered plugin reading current selections and refreshed response data', async () => {
    await render();
    const originalPlugin = captured.plugins[0];
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    const option = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-model-speed-picker] [role="option"]')).find((item) => item.textContent === 'fast-model-full-name')!;
    await act(async () => option.click());
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })));
    expect(captured.plugins[0]).toBe(originalPlugin);
    await event(0, 100, 'mousemove', originalPlugin);
    expect(document.querySelector('[data-model-speed-tooltip]')!.textContent).not.toContain('fast-model-full-name');
    const refreshed = structuredClone(speed);
    refreshed.series[0].samples[0].speed_tps_sum = 600;
    await render(refreshed);
    expect(captured.plugins[0]).toBe(originalPlugin);
    await event(0, 100, 'mousemove', originalPlugin);
    expect(document.querySelector('[data-model-speed-tooltip]')!.textContent).toContain('200.0 t/s');
  });

  it('lets the pointer pass through the tooltip, follows the next time group and closes on canvas leave', async () => {
    await render();
    await event(0.3);
    const tooltip = document.querySelector<HTMLElement>('[data-model-speed-tooltip]')!;
    expect(getComputedStyle(tooltip).pointerEvents).toBe('none');
    expect(tooltip.querySelector('button')).toBeNull();
    await event(1.25);
    expect(document.querySelector('[data-model-speed-tooltip]')!.textContent).toContain('06:00');
    await event(1.25, 100, 'mouseout');
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
  });

  it('keeps a touch-selected time group open on canvas leave and dismisses on an outside tap', async () => {
    await render();
    await act(async () => container.querySelector('canvas')!.dispatchEvent(new PointerEvent('pointerdown', { pointerType: 'touch', bubbles: true })));
    await event(0.3, 100, 'click');
    await event(0.3, 100, 'mouseout');
    expect(document.querySelector('[data-model-speed-tooltip]')!.textContent).toContain('80.0 t/s');
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { pointerType: 'touch', bubbles: true })));
    expect(document.querySelector('[data-model-speed-tooltip]')).toBeNull();
  });

  it('shows no data when the current range offers no models to choose', async () => {
    await render({ buckets: [], series: [] });
    expect(container.textContent).toContain('usage_stats.no_data');
    expect(container.textContent).not.toContain('usage_stats.model_speed_empty_selection');
  });

  it('preserves explicit empty selection and displays the selection empty state', async () => {
    localStorage.setItem(modelSpeedStorageKey('analysis', 'admin'), JSON.stringify({ version: 1, models: [] }));
    await render();
    expect(container.querySelector('canvas')).toBeNull();
    expect(container.textContent).toContain('usage_stats.model_speed_empty_selection');
  });
});
