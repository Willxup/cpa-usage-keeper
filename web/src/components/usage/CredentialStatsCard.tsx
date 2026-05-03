import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Bar } from 'react-chartjs-2';
import type { ChartData, ChartOptions, TooltipItem } from 'chart.js';
import { Card } from '@/components/ui/Card';
import { formatCompactNumber } from '@/utils/usage';
import type { UsageIdentity } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

export interface CredentialStatsCardProps {
  credentials: UsageIdentity[];
  loading: boolean;
}

export interface CredentialRow {
  key: string;
  displayName: string;
  type: string;
  success: number;
  failure: number;
  total: number;
  successRate: number;
}

export function buildCredentialRows(credentials: UsageIdentity[]): CredentialRow[] {
  return credentials
    .map((credential) => {
      const displayName = String(credential.name || credential.identity || '').trim() || '-';
      const sourceType = String(credential.auth_type_name || credential.type || '').trim();
      const key = String(credential.id || credential.identity || '').trim() || displayName;
      const success = Number(credential.success_count) || 0;
      const failure = Number(credential.failure_count) || 0;
      const total = Number(credential.total_requests) || success + failure;
      return {
        key,
        displayName,
        type: sourceType,
        success,
        failure,
        total,
        successRate: total > 0 ? (success / total) * 100 : 100,
      };
    })
    .sort((a, b) => b.total - a.total);
}

export function getTopCredentialRows(rows: CredentialRow[], limit = 10): CredentialRow[] {
  return rows.filter((row) => row.total > 0).slice(0, limit);
}

function CredentialStatsTitle({ title, subtitle, eyebrow }: { title: string; subtitle: string; eyebrow: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <span className={styles.sectionEyebrow}>{eyebrow}</span>
      <h3 className={styles.sectionTitle}>{title}</h3>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export function CredentialStatsCard({
  credentials,
  loading,
}: CredentialStatsCardProps) {
  const { t } = useTranslation();
  const rows = useMemo(() => buildCredentialRows(credentials), [credentials]);

  return (
    <Card
      title={
        <CredentialStatsTitle
          eyebrow={t('usage_stats.credential_stats_eyebrow')}
          title={t('usage_stats.credential_stats_title')}
          subtitle={t('usage_stats.credential_stats_subtitle')}
        />
      }
      className={styles.detailsFixedCard}
    >
      {loading ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : rows.length > 0 ? (
        <div className={styles.detailsScroll}>
          <div className={styles.tableWrapper}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>{t('usage_stats.credential_name')}</th>
                  <th>{t('usage_stats.requests_count')}</th>
                  <th>{t('usage_stats.success_rate')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.key}>
                    <td className={styles.modelCell}>
                      <span>{row.displayName}</span>
                      {row.type && <span className={styles.credentialType}>{row.type}</span>}
                    </td>
                    <td>
                      <span className={styles.requestCountCell}>
                        <span>{formatCompactNumber(row.total)}</span>
                        <span className={styles.requestBreakdown}>
                          (<span className={styles.statSuccess}>{row.success.toLocaleString()}</span>{' '}
                          <span className={styles.statFailure}>{row.failure.toLocaleString()}</span>)
                        </span>
                      </span>
                    </td>
                    <td>
                      <span
                        className={
                          row.successRate >= 95
                            ? styles.statSuccess
                            : row.successRate >= 80
                              ? styles.statNeutral
                              : styles.statFailure
                        }
                      >
                        {row.successRate.toFixed(1)}%
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        <div className={styles.hint}>{t('usage_stats.no_data')}</div>
      )}
    </Card>
  );
}

export function CredentialTopChartCard({ credentials, loading }: CredentialStatsCardProps) {
  const { t } = useTranslation();
  const rows = useMemo(() => buildCredentialRows(credentials), [credentials]);
  const topRows = useMemo(() => getTopCredentialRows(rows), [rows]);

  const chartData = useMemo<ChartData<'bar'>>(() => ({
    labels: topRows.map((row) => row.displayName),
    datasets: [
      {
        label: t('usage_stats.failure'),
        data: topRows.map((row) => row.failure),
        backgroundColor: 'rgba(239, 68, 68, 0.78)',
        hoverBackgroundColor: 'rgba(239, 68, 68, 0.88)',
        borderColor: 'transparent',
        borderWidth: 0,
        borderSkipped: false,
        borderRadius: topRows.map((row) => ({
          topLeft: 0,
          bottomLeft: 0,
          topRight: row.success > 0 ? 0 : 6,
          bottomRight: row.success > 0 ? 0 : 6,
        })),
        stack: 'requests',
      },
      {
        label: t('usage_stats.success'),
        data: topRows.map((row) => row.success),
        backgroundColor: 'rgba(34, 197, 94, 0.76)',
        hoverBackgroundColor: 'rgba(34, 197, 94, 0.86)',
        borderColor: 'transparent',
        borderWidth: 0,
        borderSkipped: false,
        borderRadius: topRows.map((row) => ({
          topLeft: row.failure > 0 ? 0 : 6,
          bottomLeft: row.failure > 0 ? 0 : 6,
          topRight: 6,
          bottomRight: 6,
        })),
        stack: 'requests',
      },
    ],
  }), [topRows, t]);

  const chartOptions = useMemo<ChartOptions<'bar'>>(() => ({
    indexAxis: 'y',
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        displayColors: true,
        callbacks: {
          title: (items: TooltipItem<'bar'>[]) => {
            const index = items[0]?.dataIndex ?? 0;
            return topRows[index]?.displayName ?? '';
          },
          afterBody: (items: TooltipItem<'bar'>[]) => {
            const index = items[0]?.dataIndex ?? 0;
            const row = topRows[index];
            if (!row) return [];
            return [
              `${t('usage_stats.total_requests')}: ${row.total.toLocaleString()}`,
              `${t('usage_stats.success_rate')}: ${row.successRate.toFixed(1)}%`,
            ];
          },
        },
      },
    },
    scales: {
      x: {
        stacked: true,
        beginAtZero: true,
        grid: {
          color: 'rgba(148, 163, 184, 0.18)',
        },
        ticks: {
          precision: 0,
          color: '#94a3b8',
        },
      },
      y: {
        stacked: true,
        grid: {
          display: false,
        },
        ticks: {
          display: false,
        },
      },
    },
  }), [topRows, t]);

  return (
    <Card
      title={
        <CredentialStatsTitle
          eyebrow={t('usage_stats.credential_top_chart_eyebrow')}
          title={t('usage_stats.credential_top_chart_title')}
          subtitle={t('usage_stats.credential_top_chart_hint')}
        />
      }
    >
      {loading ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : topRows.length > 0 ? (
        <div className={styles.credentialChartContent}>
          <div className={styles.chartLegend} aria-label={t('usage_stats.credential_top_chart_title')}>
            {chartData.datasets.map((dataset) => (
              <div key={dataset.label} className={styles.legendItem} title={dataset.label}>
                <span className={styles.legendDot} style={{ backgroundColor: String(dataset.backgroundColor) }} />
                <span className={styles.legendLabel}>{dataset.label}</span>
              </div>
            ))}
          </div>

          <div className={styles.credentialChartGrid}>
            <div
              className={styles.credentialChartLabels}
              style={{ gridTemplateRows: `repeat(${topRows.length}, minmax(0, 1fr))` }}
              aria-hidden="true"
            >
              {topRows.map((row) => (
                <div key={row.key} className={styles.credentialChartLabelItem} title={row.displayName}>
                  <span className={styles.credentialChartLabelName}>{row.displayName}</span>
                </div>
              ))}
            </div>
            <div className={styles.credentialChartArea}>
              <Bar data={chartData} options={chartOptions} />
            </div>
          </div>
        </div>
      ) : (
        <div className={styles.hint}>{t('usage_stats.no_data')}</div>
      )}
    </Card>
  );
}
