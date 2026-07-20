import { useMemo, type CSSProperties, type ReactNode } from 'react';
import { Badge, Card, Empty, Skeleton, Space, Tag, Tooltip, Typography } from 'antd';
import { useTranslation } from 'react-i18next';
import { SectionHeader } from '@/components/layout';
import type { ServiceHealthData, StatusBlockDetail } from '@/utils/usage';
import type { UsageOverviewPayload } from './hooks/useUsageData';
import styles from './UsageOverview.module.scss';

function rateToColor(rate: number): string {
  const t = Math.max(0, Math.min(1, rate));
  const localT = t < 0.5 ? t * 2 : (t - 0.5) * 2;
  const from = t < 0.5 ? 'var(--status-danger)' : 'var(--status-warning)';
  const to = t < 0.5 ? 'var(--status-warning)' : 'var(--status-success)';
  return `color-mix(in srgb, ${from} ${((1 - localT) * 100).toFixed(1)}%, ${to})`;
}

function formatDateTime(timestamp: number): string {
  const date = new Date(timestamp);
  const month = (date.getMonth() + 1).toString().padStart(2, '0');
  const day = date.getDate().toString().padStart(2, '0');
  const h = date.getHours().toString().padStart(2, '0');
  const m = date.getMinutes().toString().padStart(2, '0');
  return `${month}/${day} ${h}:${m}`;
}

export function parseTime(value?: string): number {
  if (!value) return 0;
  const nanosecondBoundary = value.match(/^(.*T\d{2}:\d{2}:\d{2})\.9{4,}(Z|[+-]\d{2}:\d{2})$/);
  if (nanosecondBoundary) {
    const rounded = Date.parse(`${nanosecondBoundary[1]}${nanosecondBoundary[2]}`) + 1000;
    return Number.isFinite(rounded) ? rounded : 0;
  }
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function TooltipContent({
  detail,
  timeRange,
  noRequestsLabel,
  successLabel,
  failureLabel,
}: {
  detail: StatusBlockDetail;
  timeRange: string;
  noRequestsLabel: string;
  successLabel: string;
  failureLabel: string;
}) {
  const total = detail.success + detail.failure;

  return (
    <div className={styles.healthTooltipContent}>
      <Typography.Text className={styles.healthTooltipTime}>{timeRange}</Typography.Text>
      {total > 0 ? (
        <Space size={10} wrap>
          <Badge status="success" text={`${successLabel} ${detail.success}`} />
          <Badge status="error" text={`${failureLabel} ${detail.failure}`} />
          <Typography.Text type="secondary">{(detail.rate * 100).toFixed(1)}%</Typography.Text>
        </Space>
      ) : (
        <Typography.Text type="secondary">{noRequestsLabel}</Typography.Text>
      )}
    </div>
  );
}

export interface ServiceHealthCardProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
}

export function ServiceHealthCard({ usage, loading }: ServiceHealthCardProps) {
  const { t } = useTranslation();

  const healthData: ServiceHealthData = useMemo(() => {
    const blockDetails = (usage?.service_health?.block_details ?? []).map((block) => ({
      startTime: Date.parse(block.start_time),
      endTime: Date.parse(block.end_time),
      success: Number(block.success ?? 0),
      failure: Number(block.failure ?? 0),
      rate: Number(block.rate ?? -1),
    }));
    const rows = Number(usage?.service_health?.rows ?? 7) || 7;
    return {
      totalSuccess: Number(usage?.service_health?.total_success ?? 0),
      totalFailure: Number(usage?.service_health?.total_failure ?? 0),
      successRate: Number(usage?.service_health?.success_rate ?? 0),
      rows,
      columns: Number(usage?.service_health?.columns ?? Math.max(1, Math.ceil(blockDetails.length / rows))) || 1,
      bucketSeconds: Number(usage?.service_health?.bucket_seconds ?? 0),
      windowStart: parseTime(usage?.service_health?.window_start),
      windowEnd: parseTime(usage?.service_health?.window_end),
      blockDetails,
    };
  }, [usage]);

  const hasRequests = healthData.totalSuccess + healthData.totalFailure > 0;
  const hasTimeline = healthData.blockDetails.length > 0;
  const windowLabel = healthData.windowStart > 0 && healthData.windowEnd > 0
    ? `${formatDateTime(healthData.windowStart)} – ${formatDateTime(healthData.windowEnd)}`
    : t('usage_stats.service_health_window');
  const successLabel = t('status_bar.success_short');
  const failureLabel = t('status_bar.failure_short');
  const noRequestsLabel = t('status_bar.no_requests');
  const healthCountsLabel = `${successLabel} ${healthData.totalSuccess}, ${failureLabel} ${healthData.totalFailure}`;
  const rateColor = !hasRequests
    ? 'default'
    : healthData.successRate >= 90
      ? 'success'
      : healthData.successRate >= 50
        ? 'warning'
        : 'error';
  const gridStyle = useMemo(
    () => ({
      '--health-grid-columns': String(healthData.columns),
      '--health-grid-rows': String(healthData.rows),
      '--health-grid-aspect-columns': String(healthData.columns),
      '--health-grid-aspect-rows': String(healthData.rows),
      '--health-grid-width': '100%',
    }) as CSSProperties,
    [healthData.columns, healthData.rows]
  );

  let timelineContent: ReactNode;
  if (loading) {
    timelineContent = <Skeleton active title={false} paragraph={{ rows: 4 }} />;
  } else if (!hasTimeline) {
    timelineContent = <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={noRequestsLabel} />;
  } else {
    timelineContent = (
      <div className={styles.healthGridScroller}>
        <div className={styles.healthGrid} style={gridStyle} role="list" aria-label={healthCountsLabel}>
          {healthData.blockDetails.map((detail, idx) => {
            const isIdle = detail.rate === -1;
            const blockStyle = isIdle ? undefined : { backgroundColor: rateToColor(detail.rate) };
            const timeRange = `${formatDateTime(detail.startTime)} – ${formatDateTime(detail.endTime)}`;
            const summary = detail.success + detail.failure > 0
              ? `${successLabel} ${detail.success}, ${failureLabel} ${detail.failure}`
              : noRequestsLabel;

            return (
              <Tooltip
                key={`${detail.startTime}-${idx}`}
                title={(
                  <TooltipContent
                    detail={detail}
                    timeRange={timeRange}
                    noRequestsLabel={noRequestsLabel}
                    successLabel={successLabel}
                    failureLabel={failureLabel}
                  />
                )}
                trigger={['hover', 'focus', 'click']}
                placement="top"
              >
                <div
                  className={styles.healthBlockWrapper}
                  role="listitem"
                  tabIndex={0}
                  aria-label={t('usage_stats.service_health_block_label', { timeRange, summary })}
                >
                  <div
                    className={`${styles.healthBlock} ${isIdle ? styles.healthBlockIdle : ''}`}
                    style={blockStyle}
                  />
                </div>
              </Tooltip>
            );
          })}
        </div>
      </div>
    );
  }

  return (
    <Card
      className={styles.healthCard}
      variant="outlined"
      size="small"
      title={(
        <SectionHeader
          headingLevel={2}
          title={t('usage_stats.service_health_title')}
          description={t('usage_stats.service_health_subtitle')}
        />
      )}
    >
      <div className={styles.healthSummary}>
        <Typography.Text type="secondary" className={styles.healthWindow}>{windowLabel}</Typography.Text>
        <Space size={[14, 8]} wrap aria-label={healthCountsLabel}>
          <Tag color={rateColor}>
            {loading ? '--' : hasRequests ? `${healthData.successRate.toFixed(1)}%` : '--'}
          </Tag>
          <Badge status="success" text={`${successLabel} ${healthData.totalSuccess}`} />
          <Badge status="error" text={`${failureLabel} ${healthData.totalFailure}`} />
        </Space>
      </div>

      {timelineContent}

      {hasTimeline && !loading && (
        <div className={styles.healthLegend}>
          <Typography.Text type="secondary" className={styles.healthLegendLabel}>
            {t('usage_stats.service_health_oldest')}
          </Typography.Text>
          <div className={styles.healthLegendColors} aria-hidden="true">
            <div className={`${styles.healthLegendBlock} ${styles.healthBlockIdle}`} />
            <div className={`${styles.healthLegendBlock} ${styles.healthBlockDanger}`} />
            <div className={`${styles.healthLegendBlock} ${styles.healthBlockWarning}`} />
            <div className={`${styles.healthLegendBlock} ${styles.healthBlockSuccess}`} />
          </div>
          <Typography.Text type="secondary" className={styles.healthLegendLabel}>
            {t('usage_stats.service_health_newest')}
          </Typography.Text>
        </div>
      )}
    </Card>
  );
}
