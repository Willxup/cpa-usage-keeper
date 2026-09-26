// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions } from 'chart.js';
import i18n from '@/i18n';
import type { UsageComparisonItem, UsageOverviewComparisons } from '@/lib/types';
import { UsageComparisonCharts } from '../UsageComparisonCharts';
import { buildComparisonView, formatComparisonBucket } from '../usageComparisonData';

const chart = vi.hoisted(() => ({data: undefined as ChartData<'line'> | undefined, options: undefined as ChartOptions<'line'> | undefined}));
vi.mock('react-chartjs-2', () => ({Line: (props: {data: ChartData<'line'>; options: ChartOptions<'line'>}) => { chart.data=props.data; chart.options=props.options; return <canvas />; }}));
let container: HTMLDivElement;
let root: Root;
const item = (key: string, overrides: Partial<UsageComparisonItem> = {}): UsageComparisonItem => ({
  key, label: key, requests: 10, failures: 0, input_tokens: 80, output_tokens: 20,
  cache_read_tokens: 40, cache_creation_tokens: 0, reasoning_tokens: 5, total_tokens: 100, cost: 1, token_series:[40,0,60], ...overrides,
});
const comparisons = (items = [item('model-a')]): UsageOverviewComparisons => ({
  buckets:['2026-09-18','2026-09-19','2026-09-20'],granularity:'daily',timezone:'Asia/Shanghai',
  models:items,api_keys:[item('My Key')],auth_files:[item('auth.json')],ai_providers:[item('OpenAI')],
});
beforeEach(async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  chart.data=undefined;chart.options=undefined;
  container=document.createElement('div');document.body.appendChild(container);root=createRoot(container);
});
afterEach(async()=>{await act(async()=>root.unmount());container.remove();});
const render = async (data = comparisons(), keyViewer = false, loading = false) => act(async()=>root.render(<UsageComparisonCharts comparisons={data} loading={loading} keyViewer={keyViewer} />));
const click = async (selector:string) => act(async()=>container.querySelector<HTMLButtonElement>(selector)!.click());

it('renders a single stacked area chart with all four dimensions and zero-filled idle buckets',async()=>{
  await render();
  expect(container.querySelectorAll('canvas')).toHaveLength(1);
  expect(container.querySelectorAll('[data-dimension]')).toHaveLength(4);
  expect(chart.data?.datasets[0].data).toEqual([40,0,60]);
  expect(chart.data?.datasets[0]).toMatchObject({fill:'origin',borderWidth:0,pointRadius:0,cubicInterpolationMode:'monotone'});
  expect(chart.options?.scales?.y?.stacked).toBe(true);
  for(const [dimension,label] of [['api_keys','My Key'],['auth_files','auth.json'],['ai_providers','OpenAI'],['models','model-a']]){
    await click('[data-dimension="'+dimension+'"]');
    expect(chart.data?.datasets[0].label).toBe(label);
    expect(container.querySelector('[data-dimension="'+dimension+'"]')?.getAttribute('aria-pressed')).toBe('true');
  }
});
it('aggregates Others at each time bucket and retains every total and unknown price',()=>{
  const items=Array.from({length:7},(_,i)=>item('m'+i,{total_tokens:100-i,token_series:[60-i,0,40],cost:i===6?null:1}));
  const view=buildComparisonView(items,'Others');
  expect(view.rows).toHaveLength(6);
  expect(view.rows[5]).toMatchObject({other:true,total_tokens:189,token_series:[109,0,80],requests:20,cost:null});
  expect(view.rows.reduce((n,r)=>n+(r.share??0),0)).toBeCloseTo(100);
  for(let i=0;i<3;i++) expect(view.rows.reduce((n,r)=>n+(r.token_series?.[i]??0),0)).toBe(items.reduce((n,r)=>n+r.token_series![i],0));
});
it('shows the same ranked series in chart, legend and interval summaries',async()=>{
  await render(comparisons(Array.from({length:7},(_,i)=>item('m'+i,{total_tokens:100-i}))));
  expect(chart.data?.datasets).toHaveLength(6);
  expect(container.querySelectorAll('[data-series-key]')).toHaveLength(6);
  expect(container.querySelectorAll('[data-comparison-entry]')).toHaveLength(6);
  expect(container.querySelector('[data-comparison-entry="__comparison_others__"]')).not.toBeNull();
});
it('toggles a series without changing range totals and resets visibility on dimension switch',async()=>{
  await render();
  await click('[data-series-key="model-a"]');
  expect(chart.data?.datasets[0].hidden).toBe(true);
  expect(container.querySelector('[data-comparison-total]')?.textContent).toContain('100');
  await click('[data-dimension="api_keys"]');
  expect(chart.data?.datasets[0].hidden).toBe(false);
});
it('preserves detailed metrics on focus and updates summaries on polling',async()=>{
  await render(comparisons([item('model-a',{cost:null})]));
  await act(async()=>container.querySelector<HTMLButtonElement>('[data-comparison-entry]')!.focus());
  expect(document.querySelector('[role="tooltip"]:not([hidden])')?.textContent).toContain('Cost: —');
  expect(document.querySelector('[role="tooltip"]:not([hidden])')?.textContent).toContain('Requests: 10');
  await render(comparisons([item('model-b',{cost:0})]),false,true);
  expect(container.textContent).toContain('model-b');
  expect(container.textContent).not.toContain('model-a');
  expect(document.querySelector('[role="tooltip"]:not([hidden])')).toBeNull();
  expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
});
it('keeps same-label identities separate when polling changes their ranking',async()=>{
  await render(comparisons([item('a',{label:'Same',total_tokens:200}),item('b',{label:'Same'})]));
  await click('[data-series-key="a"]');
  await render(comparisons([item('a',{label:'Same'}),item('b',{label:'Same',total_tokens:300})]));
  expect(container.querySelectorAll('[data-series-key]')).toHaveLength(2);
  expect(chart.data?.datasets.map(dataset=>dataset.hidden)).toEqual([false,true]);
  expect(container.querySelector('[data-series-key="a"]')?.getAttribute('aria-pressed')).toBe('false');
  expect(container.querySelector('[data-series-key="b"]')?.getAttribute('aria-pressed')).toBe('true');
});
it('limits Key Viewer to models and its own API Key even if restricted arrays are supplied',async()=>{
  await render(comparisons(),true);
  expect(container.querySelectorAll('[data-dimension]')).toHaveLength(2);
  expect(container.querySelector('[data-dimension="auth_files"]')).toBeNull();
  expect(container.querySelector('[data-dimension="ai_providers"]')).toBeNull();
  await click('[data-dimension="api_keys"]');
  expect(chart.data?.datasets[0].label).toBe('My Key');
});
it('renders loading and empty states and makes a single bucket visible',async()=>{
  await act(async()=>root.render(<UsageComparisonCharts loading />));
  expect(container.textContent).toContain('Loading');
  await render(comparisons([]));
  expect(container.querySelector('canvas')).toBeNull();
  await render({...comparisons(),buckets:['2026-09-18'],models:[item('one',{token_series:[100]})]});
  expect(chart.data?.datasets[0].pointRadius).toBeGreaterThan(0);
});
it('formats date-only days without a timezone shift and hourly instants in the response timezone',()=>{
  expect(formatComparisonBucket('2026-09-18','daily','America/Los_Angeles','en')).toBe('Sep 18');
  expect(formatComparisonBucket('2026-09-18T00:00:00Z','hourly','Asia/Shanghai','en')).toBe('08:00');
  expect(formatComparisonBucket('2026-09-18T08:00:00+08:00','hourly','Local','en')).toBe('08:00');
  expect(formatComparisonBucket('2026-09-18T08:00:00+08:00','hourly','invalid','en')).toBe('08:00');
  expect(formatComparisonBucket('2026-09-18T00:00:00Z','hourly','Asia/Shanghai','en',true)).toBe('Sep 18, 2026, 08:00');
});
