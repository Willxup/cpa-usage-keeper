import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { beforeEach, expect, it, vi } from 'vitest';
import type { RealtimeWindowSummary } from '@/lib/types';
import i18n from '@/i18n';

const charts = vi.hoisted(() => ({ mixed: [] as Array<Record<string, unknown>>, doughnut: [] as Array<Record<string, unknown>> }));
vi.mock('react-chartjs-2', () => ({
  Chart: (props: Record<string, unknown>) => { charts.mixed.push(props); return null; },
  Doughnut: (props: Record<string, unknown>) => { charts.doughnut.push(props); return null; },
}));
import { buildRealtimeCacheData, RealtimeDiagnostics, RealtimeWindowCards } from '../RealtimeInsights';

const summary: RealtimeWindowSummary = { requests: 10, failures: 2, token_requests: 5, cached_requests: 2, total_tokens: 1000, input_tokens: 800, output_tokens: 200, cache_read_tokens: 400, cache_creation_tokens: 100, reasoning_tokens: 80, cost: null };
beforeEach(async () => { await i18n.changeLanguage('en'); charts.mixed = []; charts.doughnut = []; });

it('shows cache reach and token cache share using their own denominators', () => {
  const html = renderToStaticMarkup(<><RealtimeWindowCards summary={summary} window="15m" /><RealtimeDiagnostics insights={{summary,outcomes:[]}} labels={[]} isDark={false} isMobile={false} /></>);
  const options=charts.doughnut[0].options as {plugins:{realtimeCacheShareCenter:{value:string}}};
  expect(options.plugins.realtimeCacheShareCenter.value).toBe('40.0%'); // 缓存读取 / Token 构成总量。
  expect(html).toContain('40.0%'); // 使用缓存的成功请求 / 有 Token 的成功请求。
  expect(html).toContain('—');
});

it('keeps real zero cache values and missing ratios distinct in the combined chart', () => {
  const data = buildRealtimeCacheData([
    { bucket: 'a', input_tokens: 100, cache_read_tokens: 20, cache_creation_tokens: 10, cache_read_rate: 20 },
    { bucket: 'b', input_tokens: 100, cache_read_tokens: 0, cache_creation_tokens: 0, cache_read_rate: 0 },
    { bucket: 'c', input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, cache_read_rate: null },
  ], ['a', 'b', 'c'], key => key);
  expect(data.datasets[0].data).toEqual([70, 100, 0]);
  expect(data.datasets[1].data).toEqual([20, 0, 0]);
  expect(data.datasets[2].data).toEqual([10, 0, 0]);
  expect(data.datasets[3].data).toEqual([20, 0, null]);
  expect(data.datasets[3].yAxisID).toBe('rate');
});

it('keeps request buckets non-overlapping and excludes reasoning from token composition', () => {
  const html = renderToStaticMarkup(<RealtimeDiagnostics insights={{ summary, outcomes: [{bucket:'a',requests:4,failures:1},{bucket:'b',requests:0,failures:0}] }} labels={['a','b']} isDark={false} isMobile={false} />);
  const data = charts.mixed[0].data as {datasets: Array<{data: Array<number|null>}>};
  expect(data.datasets.map(d=>d.data)).toEqual([[3,0],[1,0],[25,null]]);
  const mix = charts.doughnut[0].data as {datasets: Array<{data: number[]}>};
  expect(mix.datasets[0].data).toEqual([300,400,100,200]);
  expect(html).toContain('Reasoning');
  expect(html).toContain('80');
});

it('renders unavailable ratios as dashes for an empty window without NaN', () => {
  const empty = Object.fromEntries(Object.keys(summary).map(key=>[key,0])) as unknown as RealtimeWindowSummary;
  const html = renderToStaticMarkup(<><RealtimeWindowCards summary={empty} window="60m" /><RealtimeDiagnostics insights={{summary:empty,outcomes:[]}} labels={[]} isDark isMobile /></>);
  expect(html).toContain('—');
  expect(html).not.toContain('NaN');
  expect(charts.doughnut).toHaveLength(0);
});

it('draws updated center values before the canvas tooltip without a DOM overlay', () => {
  const render = (read: number, dark: boolean) => renderToStaticMarkup(<RealtimeDiagnostics insights={{summary:{...summary,cache_read_tokens:read},outcomes:[]}} labels={[]} isDark={dark} isMobile={false} />);
  render(400,false);
  const first=charts.doughnut[0];
  render(200,true);
  const next=charts.doughnut[1];
  const plugins=next.plugins as Array<{afterDatasetsDraw:(chart:unknown,args:unknown,options:unknown)=>void}>;
  expect(plugins[0]).toBe((first.plugins as unknown[])[0]);
  const options=next.options as {plugins:{realtimeCacheShareCenter:{value:string,label:string}}};
  const texts:string[]=[];
  plugins[0].afterDatasetsDraw({
    width:200, chartArea:{left:0,right:200,top:0,bottom:200},
    ctx:{save(){},restore(){},fillText(value:string){texts.push(value)}},
  },{},options.plugins.realtimeCacheShareCenter);
  expect(texts).toEqual(['20.0%','Cache read share']);
});
