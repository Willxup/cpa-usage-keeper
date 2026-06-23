import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { IconDiamond, IconDollarSign, IconSatellite } from '@/components/ui/icons';
import { formatCompactNumber, formatUsd } from '@/utils/usage';
import type { UsageOverviewPayload } from './hooks/useUsageData';
import styles from '@/pages/UsagePage.module.scss';

const DAILY_AVERAGE_TRANSITION_MS = 220;

interface DailyAverageMetrics {
  requests: number;
  tokens: number;
  cost: number;
  rangeDays: number;
  costAvailable: boolean;
}

interface DailyAveragePanelProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
  reserveVisible?: boolean;
}

interface DailyAverageMetricItem {
  key: string;
  label: string;
  value: string;
  icon: ReactNode;
  accent: string;
  className?: string;
}

const isFiniteMetric = (value: unknown): value is number => (
  typeof value === 'number' && Number.isFinite(value)
);

const formatAverageCount = (value: number): string => {
  if (Math.abs(value) < 1_000) {
    return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value);
  }
  return formatCompactNumber(value);
};

const formatRangeDays = (value: number): string => (
  new Intl.NumberFormat(undefined, { maximumFractionDigits: value >= 10 ? 0 : 1 }).format(value)
);

export function buildDailyAverageMetrics(usage: UsageOverviewPayload | null): DailyAverageMetrics | null {
  const summary = usage?.summary;
  if (!summary) {
    return null;
  }

  const requests = summary.daily_average_requests;
  const tokens = summary.daily_average_tokens;
  const cost = summary.daily_average_cost;
  const rangeDays = summary.daily_average_range_days;
  if (!isFiniteMetric(requests) || !isFiniteMetric(tokens) || !isFiniteMetric(cost) || !isFiniteMetric(rangeDays)) {
    return null;
  }
  return {
    requests,
    tokens,
    cost,
    rangeDays,
    costAvailable: summary.cost_available === true,
  };
}

export function DailyAveragePanel({ usage, loading, reserveVisible = false }: DailyAveragePanelProps) {
  const { t } = useTranslation();
  const metrics = useMemo(() => buildDailyAverageMetrics(usage), [usage]);
  const shouldRenderPanel = Boolean(metrics) || reserveVisible;
  const [renderPanel, setRenderPanel] = useState(shouldRenderPanel);
  const [visible, setVisible] = useState(false);
  const renderPanelRef = useRef(renderPanel);

  useEffect(() => {
    renderPanelRef.current = renderPanel;
  }, [renderPanel]);

  useEffect(() => {
    let frame: number | null = null;
    let enterFrame: number | null = null;
    let timer: number | null = null;

    if (shouldRenderPanel) {
      frame = window.requestAnimationFrame(() => {
        const hadPanel = renderPanelRef.current;
        setRenderPanel(true);
        if (hadPanel) {
          setVisible(true);
          return;
        }
        setVisible(false);
        enterFrame = window.requestAnimationFrame(() => setVisible(true));
      });
      return () => {
        if (frame !== null) window.cancelAnimationFrame(frame);
        if (enterFrame !== null) window.cancelAnimationFrame(enterFrame);
      };
    }

    frame = window.requestAnimationFrame(() => setVisible(false));
    timer = window.setTimeout(() => setRenderPanel(false), DAILY_AVERAGE_TRANSITION_MS);
    return () => {
      if (frame !== null) window.cancelAnimationFrame(frame);
      if (timer !== null) window.clearTimeout(timer);
    };
  }, [shouldRenderPanel]);

  if (!renderPanel) {
    return null;
  }

  const metricItems: DailyAverageMetricItem[] = [
    {
      key: 'requests',
      label: t('usage_stats.avg_requests'),
      value: loading || !metrics ? '-' : formatAverageCount(metrics.requests),
      icon: <IconSatellite size={15} />,
      accent: '#3b82f6',
    },
    {
      key: 'tokens',
      label: t('usage_stats.avg_tokens'),
      value: loading || !metrics ? '-' : formatCompactNumber(metrics.tokens),
      icon: <IconDiamond size={15} />,
      accent: '#8b5cf6',
    },
    {
      key: 'cost',
      label: t('usage_stats.avg_cost'),
      value: loading || !metrics ? '-' : formatUsd(metrics.cost),
      icon: <IconDollarSign size={15} />,
      accent: '#f59e0b',
      className: styles.dailyAverageMetricCost,
    },
  ];

  return (
    <section
      className={`${styles.dailyAveragePanel} ${visible ? styles.dailyAveragePanelVisible : styles.dailyAveragePanelEntering}`.trim()}
      aria-label={t('usage_stats.daily_average')}
    >
      <div className={styles.dailyAverageIdentity}>
        <span className={styles.dailyAverageTitle}>{t('usage_stats.daily_average')}</span>
        {metrics && (
          <span className={styles.dailyAverageRangePill}>
            {t('usage_stats.daily_average_range', { days: formatRangeDays(metrics.rangeDays) })}
          </span>
        )}
      </div>
      <div className={styles.dailyAverageMetrics}>
        {metricItems.map((item) => (
          <div
            key={item.key}
            className={`${styles.dailyAverageMetric} ${item.className ?? ''}`.trim()}
            style={{ '--metric-accent': item.accent } as CSSProperties}
          >
            <span className={styles.dailyAverageMetricIcon}>{item.icon}</span>
            <span className={styles.dailyAverageMetricText}>
              <span className={styles.dailyAverageMetricLabel}>{item.label}</span>
              <strong className={styles.dailyAverageMetricValue}>{item.value}</strong>
            </span>
          </div>
        ))}
      </div>
      {metrics && !metrics.costAvailable && (
        <span className={styles.dailyAverageCostHint}>{t('usage_stats.cost_need_price')}</span>
      )}
    </section>
  );
}
