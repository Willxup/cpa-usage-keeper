export type ChartSeriesKey = 'blue' | 'cyan' | 'indigo' | 'violet' | 'teal' | 'orange';

type ChartSeriesColor = {
  stroke: string;
  fill: string;
};

export type KeeperChartTheme = {
  textPrimary: string;
  textSecondary: string;
  grid: string;
  axis: string;
  tooltipBackground: string;
  tooltipBorder: string;
  tooltipBody: string;
  averageLine: string;
  series: Record<ChartSeriesKey, ChartSeriesColor>;
};

const LIGHT_SERIES: Record<ChartSeriesKey, ChartSeriesColor> = {
  blue: { stroke: '#2563eb', fill: 'rgba(37, 99, 235, 0.14)' },
  cyan: { stroke: '#0369a1', fill: 'rgba(3, 105, 161, 0.14)' },
  indigo: { stroke: '#4f46e5', fill: 'rgba(79, 70, 229, 0.14)' },
  violet: { stroke: '#7c3aed', fill: 'rgba(124, 58, 237, 0.14)' },
  teal: { stroke: '#0f766e', fill: 'rgba(15, 118, 110, 0.14)' },
  orange: { stroke: '#c2410c', fill: 'rgba(194, 65, 12, 0.14)' },
};

const DARK_SERIES: Record<ChartSeriesKey, ChartSeriesColor> = {
  blue: { stroke: '#5b9cff', fill: 'rgba(91, 156, 255, 0.18)' },
  cyan: { stroke: '#38bdf8', fill: 'rgba(56, 189, 248, 0.18)' },
  indigo: { stroke: '#818cf8', fill: 'rgba(129, 140, 248, 0.18)' },
  violet: { stroke: '#a78bfa', fill: 'rgba(167, 139, 250, 0.18)' },
  teal: { stroke: '#2dd4bf', fill: 'rgba(45, 212, 191, 0.18)' },
  orange: { stroke: '#fb923c', fill: 'rgba(251, 146, 60, 0.18)' },
};

const DARK_THEME: KeeperChartTheme = {
  textPrimary: '#f1f5f9',
  textSecondary: '#aab6c5',
  grid: 'rgba(170, 182, 197, 0.1)',
  axis: 'rgba(170, 182, 197, 0.2)',
  tooltipBackground: '#202b3a',
  tooltipBorder: '#2b394c',
  tooltipBody: '#f1f5f9',
  averageLine: 'rgba(170, 182, 197, 0.72)',
  series: DARK_SERIES,
};

const LIGHT_THEME: KeeperChartTheme = {
  textPrimary: '#192230',
  textSecondary: '#566477',
  grid: 'rgba(86, 100, 119, 0.1)',
  axis: 'rgba(86, 100, 119, 0.2)',
  tooltipBackground: '#ffffff',
  tooltipBorder: '#d7e0ea',
  tooltipBody: '#192230',
  averageLine: 'rgba(86, 100, 119, 0.68)',
  series: LIGHT_SERIES,
};

/**
 * Ant Design Charts/G2 renders to canvas, where CSS custom properties cannot be
 * resolved reliably, so chart configuration receives concrete theme values.
 * Returns stable singletons so callers can rely on referential identity in
 * memo dependency arrays and avoid rebuilding the palette in tight loops.
 */
export const getChartTheme = (isDark: boolean): KeeperChartTheme => (isDark ? DARK_THEME : LIGHT_THEME);

export const getSeriesColor = (isDark: boolean, key: ChartSeriesKey): ChartSeriesColor => getChartTheme(isDark).series[key];
