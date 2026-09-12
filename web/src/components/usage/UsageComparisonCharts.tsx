import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { UsageComparisonItem, UsageOverviewComparisons } from '@/lib/types';
import { Card } from '@/components/ui/Card';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { formatCompactNumber, formatUsd } from '@/utils/usage';
import styles from './UsageComparisonCharts.module.scss';

type Metric = 'tokens' | 'requests' | 'cost' | 'failures' | 'per_request' | 'unit_cost' | 'cache' | 'failure_rate';
const METRICS: Metric[] = ['tokens', 'requests', 'cost', 'failures'];
const EFFICIENCY_METRICS: Metric[] = ['per_request', 'unit_cost', 'cache', 'failure_rate'];
const PAGE_SIZE = 6;
const EMPTY: UsageComparisonItem[] = [];
const EMPTY_ITEM: UsageComparisonItem = { key: '', label: '', requests: 0, failures: 0, input_tokens: 0, output_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, total_tokens: 0, cost: null };
const percent = (numerator: number, denominator: number) => denominator > 0 ? `${(numerator / denominator * 100).toFixed(1)}%` : '—';
const metricValue = (item: UsageComparisonItem, metric: Metric): number | null => {
  if (metric === 'tokens') return item.total_tokens;
  if (metric === 'per_request') return item.requests > 0 ? item.total_tokens / item.requests : null;
  if (metric === 'unit_cost') return item.requests > 0 && item.cost !== null ? item.cost / item.requests * 1000 : null;
  if (metric === 'cache') return item.input_tokens > 0 ? item.cache_read_tokens / item.input_tokens * 100 : null;
  if (metric === 'failure_rate') return item.requests > 0 ? item.failures / item.requests * 100 : null;
  return item[metric];
};

function ComparisonChart({ items, dimension, loading }: { items: UsageComparisonItem[]; dimension: 'models' | 'api_keys' | 'efficiency'; loading: boolean }) {
  const { t } = useTranslation();
  const efficiency = dimension === 'efficiency';
  const [metric, setMetric] = useState<Metric>(efficiency ? 'per_request' : 'tokens');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);
  const title = t(`usage_stats.comparison_${dimension}`);
  const ranked = useMemo(() => [...items].sort((a, b) => {
    const av = metricValue(a, metric), bv = metricValue(b, metric);
    if (av === null || bv === null) return av === bv ? a.key.localeCompare(b.key) : av === null ? 1 : -1;
    return bv - av || a.key.localeCompare(b.key);
  }), [items, metric]);
  // 排序、搜索和翻页只影响展示；分母始终来自完整筛选范围，不能把当前页误当作总体。
  const total = useMemo(() => items.reduce((sum, item) => sum + (metricValue(item, metric) ?? 0), 0), [items, metric]);
  const filtered = useMemo(() => ranked.filter((item) => item.label.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())), [ranked, search]);
  const currentPage = Math.min(page, Math.max(0, Math.ceil(filtered.length / PAGE_SIZE) - 1));
  const visible = filtered.slice(currentPage * PAGE_SIZE, (currentPage + 1) * PAGE_SIZE);
  const maximum = Math.max(0, metricValue(ranked[0] ?? EMPTY_ITEM, metric) ?? 0);
  const partialCost = (metric === 'cost' || metric === 'unit_cost') && items.some((item) => item.cost === null);
  const format = (value: number | null) => value === null ? '—' : metric === 'cost' || metric === 'unit_cost' ? formatUsd(value) : metric === 'cache' || metric === 'failure_rate' ? `${value.toFixed(1)}%` : formatCompactNumber(value);
  const topThree = ranked.slice(0, 3).reduce((sum, item) => sum + (metricValue(item, metric) ?? 0), 0);

  return <Card title={title} subtitle={t('usage_stats.comparison_subtitle')} className={styles.card} data-comparison={dimension}>
    <div className={styles.controls} role="group" aria-label={`${title}: ${t('usage_stats.comparison_metric')}`}>
      {(efficiency ? EFFICIENCY_METRICS : METRICS).map((value) => <button key={value} type="button" aria-pressed={metric === value} onClick={() => { setMetric(value); setPage(0); }}>{t(`usage_stats.comparison_${value}`)}</button>)}
    </div>
    <div className={styles.summary}>
      <div><span>{t(efficiency ? 'usage_stats.comparison_highest' : partialCost ? 'usage_stats.comparison_known_cost' : `usage_stats.comparison_${metric}`)}</span><strong>{efficiency ? format(metricValue(ranked[0] ?? EMPTY_ITEM, metric)) : format(total)}</strong></div>
      <div><span>{t('usage_stats.comparison_active')}</span><strong>{items.length.toLocaleString()}</strong></div>
      {!efficiency && <div><span>{t('usage_stats.comparison_top3')}</span><strong>{percent(topThree, total)}</strong></div>}
    </div>
    <input className={styles.search} type="search" value={search} placeholder={t('usage_stats.comparison_search')} aria-label={`${title}: ${t('usage_stats.comparison_search')}`} onInput={(event) => { setSearch(event.currentTarget.value); setPage(0); }} />
    <div className={styles.chart} aria-busy={loading} aria-label={title}>
      {loading && items.length === 0 ? <div className={styles.empty}><LoadingSpinner size={18} />{t('common.loading')}</div> : visible.length === 0 ? <div className={styles.empty}>{t('usage_stats.comparison_empty')}</div> : visible.map((item) => {
        const value = metricValue(item, metric);
        const width = maximum > 0 && value !== null ? Math.max(0, value / maximum * 100) : 0;
        return <details key={item.key} className={styles.row}>
          <summary>
            <div className={styles.topline}><span className={styles.name} title={item.label}>{item.label}</span><strong>{format(value)}</strong>{!efficiency && <span className={styles.share}>{value === null ? '—' : percent(value, total)}</span>}</div>
            <div className={styles.track} aria-hidden="true"><span className={styles.bar} data-metric={metric} style={{ width: `${width}%` }}>{metric === 'requests' && item.failures > 0 && <span className={styles.failed} style={{ width: `${Math.min(100, item.failures / item.requests * 100)}%` }} />}</span></div>
            <div className={styles.meta}>
              <span>{t('usage_stats.comparison_success')} <b>{percent(item.requests - item.failures, item.requests)}</b></span>
              <span>{t('usage_stats.comparison_cache')} <b>{percent(item.cache_read_tokens, item.input_tokens)}</b></span>
              <span>{t('usage_stats.comparison_per_request')} <b>{item.requests > 0 ? formatCompactNumber(item.total_tokens / item.requests) : '—'}</b></span>
            </div>
          </summary>
          <dl className={styles.details}>
            {([
              ['requests', item.requests], ['failures', item.failures], ['input', item.input_tokens], ['output', item.output_tokens],
              ['cache_read', item.cache_read_tokens], ['cache_write', item.cache_creation_tokens], ['reasoning', item.reasoning_tokens],
            ] as const).map(([label, amount]) => <div key={label}><dt>{t(`usage_stats.comparison_${label}`)}</dt><dd>{amount.toLocaleString()}</dd></div>)}
            <div><dt>{t('usage_stats.comparison_cost')}</dt><dd>{item.cost === null ? '—' : formatUsd(item.cost)}</dd></div>
            <div><dt>{t('usage_stats.comparison_unit_cost')}</dt><dd>{item.cost === null || item.requests === 0 ? '—' : formatUsd(item.cost / item.requests * 1000)}</dd></div>
          </dl>
        </details>;
      })}
    </div>
    <div className={styles.footer}><span>{t('usage_stats.comparison_page', { start: filtered.length ? currentPage * PAGE_SIZE + 1 : 0, end: Math.min((currentPage + 1) * PAGE_SIZE, filtered.length), total: filtered.length })}</span><div>
      <button type="button" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>{t('usage_stats.comparison_previous')}</button>
      <button type="button" disabled={(currentPage + 1) * PAGE_SIZE >= filtered.length} onClick={() => setPage(currentPage + 1)}>{t('usage_stats.comparison_next')}</button>
    </div></div>
    <p className={styles.note}>{t(efficiency ? 'usage_stats.comparison_efficiency_hint' : partialCost ? 'usage_stats.comparison_partial_hint' : 'usage_stats.comparison_hint')}</p>
  </Card>;
}

export function UsageComparisonCharts({ comparisons, loading, keyViewer = false }: { comparisons?: UsageOverviewComparisons; loading: boolean; keyViewer?: boolean }) {
  return <div className={styles.grid}>
    <ComparisonChart dimension="models" items={comparisons?.models ?? EMPTY} loading={loading} />
    <ComparisonChart dimension={keyViewer ? "efficiency" : "api_keys"} items={(keyViewer ? comparisons?.models : comparisons?.api_keys) ?? EMPTY} loading={loading} />
  </div>;
}
