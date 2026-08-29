/**
 * Chart.js configuration utilities for usage statistics
 * Extracted from UsagePage.tsx for reusability
 */

import type { ChartOptions } from 'chart.js';

export const USAGE_CHART_REQUESTS_LINE_COLOR = '#ff5a40';

export interface UsageChartGradientColor {
  base: string;
  light: string;
}

// 共用 Analysis 柱形图的纵向渐变，保证不同业务图表的柱体质感一致。
export const toUsageChartGradientFill = (
  context: { chart: { ctx: CanvasRenderingContext2D; chartArea?: { top: number; bottom: number } } },
  color: UsageChartGradientColor,
) => {
  const { chart } = context;
  if (!chart.chartArea) return color.base;
  const gradient = chart.ctx.createLinearGradient(0, chart.chartArea.top, 0, chart.chartArea.bottom);
  gradient.addColorStop(0, color.light);
  gradient.addColorStop(1, color.base);
  return gradient;
};

export interface UsageChartTheme {
  textPrimary: string;
  textSecondary: string;
  grid: string;
  axis: string;
  averageLine: string;
  tooltipBg: string;
  tooltipBorder: string;
  tooltipBody: string;
}

// Analysis 与其他业务图表共用同一组画布颜色，避免浅色和深色 Tooltip 各自漂移。
export const getUsageChartTheme = (isDark: boolean): UsageChartTheme => ({
  textPrimary: isDark ? '#f5f1e8' : '#111827',
  textSecondary: isDark ? 'rgba(255, 255, 255, 0.72)' : 'rgba(17, 24, 39, 0.72)',
  grid: isDark ? 'rgba(255, 255, 255, 0.06)' : 'rgba(17, 24, 39, 0.06)',
  axis: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
  averageLine: isDark ? 'rgba(203, 213, 225, 0.62)' : 'rgba(71, 85, 105, 0.62)',
  tooltipBg: isDark ? 'rgba(17, 24, 39, 0.94)' : 'rgba(255, 255, 255, 0.98)',
  tooltipBorder: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
  tooltipBody: isDark ? 'rgba(255, 255, 255, 0.86)' : '#374151',
});

export const buildUsageChartTooltipStyle = (chartTheme: UsageChartTheme) => ({
  backgroundColor: chartTheme.tooltipBg,
  titleColor: chartTheme.textPrimary,
  bodyColor: chartTheme.tooltipBody,
  footerColor: chartTheme.tooltipBody,
  borderColor: chartTheme.tooltipBorder,
  borderWidth: 1,
  padding: 10,
  titleSpacing: 2,
  titleMarginBottom: 6,
  bodySpacing: 2,
  footerSpacing: 2,
  footerMarginTop: 6,
  displayColors: true,
  usePointStyle: true,
});

/**
 * Static sparkline chart options (no dependencies on theme/mobile)
 */
export const sparklineOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { display: false }, tooltip: { enabled: false } },
  scales: { x: { display: false }, y: { display: false } },
  elements: { line: { tension: 0.45 }, point: { radius: 0 } }
};

export interface ChartConfigOptions {
  period: 'hour' | 'day';
  labels: string[];
  isDark: boolean;
  isMobile: boolean;
  valueFormatter?: (value: number) => string;
  tooltipValueFormatter?: (value: number) => string;
}

/**
 * Build chart options with theme and responsive awareness
 */
export function buildChartOptions({
  period,
  labels,
  isDark,
  isMobile,
  valueFormatter,
  tooltipValueFormatter
}: ChartConfigOptions): ChartOptions<'line'> {
  const pointRadius = isMobile ? 2 : 4;
  const tickFontSize = isMobile ? 10 : 12;
  const maxTickLabelCount = isMobile ? (period === 'hour' ? 8 : 6) : period === 'hour' ? 12 : 10;
  const chartTheme = getUsageChartTheme(isDark);

  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
      mode: 'index',
      intersect: false
    },
    plugins: {
      legend: { display: false },
      tooltip: {
        ...buildUsageChartTooltipStyle(chartTheme),
        callbacks: (valueFormatter || tooltipValueFormatter)
          ? {
              label: (context) => {
                const label = context.dataset.label ? `${context.dataset.label}: ` : '';
                const formatter = tooltipValueFormatter ?? valueFormatter;
                return `${label}${formatter ? formatter(Number(context.parsed.y ?? 0)) : ''}`;
              }
            }
          : undefined
      }
    },
    scales: {
      x: {
        grid: {
          color: chartTheme.grid,
          drawTicks: false
        },
        border: {
          color: chartTheme.axis
        },
        ticks: {
          color: chartTheme.textSecondary,
          font: { size: tickFontSize },
          maxRotation: isMobile ? 0 : 45,
          minRotation: 0,
          autoSkip: true,
          maxTicksLimit: maxTickLabelCount,
          callback: (value) => {
            const index = typeof value === 'number' ? value : Number(value);
            const raw =
              Number.isFinite(index) && labels[index] ? labels[index] : typeof value === 'string' ? value : '';

            if (period === 'hour') {
              const [md, time] = raw.split(' ');
              if (!time) return raw;
              if (time.startsWith('00:')) {
                return md ? [md, time] : time;
              }
              return time;
            }

            if (isMobile) {
              const parts = raw.split('-');
              if (parts.length === 3) {
                return `${parts[1]}-${parts[2]}`;
              }
            }
            return raw;
          }
        }
      },
      y: {
        beginAtZero: true,
        grid: {
          color: chartTheme.grid
        },
        border: {
          color: chartTheme.axis
        },
        ticks: {
          color: chartTheme.textSecondary,
          font: { size: tickFontSize },
          callback: valueFormatter
            ? (value) => valueFormatter(Number(value))
            : undefined
        }
      }
    },
    elements: {
      line: {
        tension: 0.35,
        borderWidth: isMobile ? 1.5 : 2
      },
      point: {
        borderWidth: 2,
        radius: pointRadius,
        hoverRadius: 4
      }
    }
  };
}

/**
 * Calculate minimum chart width for hourly data on mobile devices
 */
export function getHourChartMinWidth(labelCount: number, isMobile: boolean): string | undefined {
  if (!isMobile || labelCount <= 0) return undefined;
  const perPoint = 56;
  const minWidth = Math.min(labelCount * perPoint, 3000);
  return `${minWidth}px`;
}
