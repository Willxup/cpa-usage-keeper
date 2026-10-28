import { useTranslation } from 'react-i18next';
import type { useAnalysisHighlight } from '../analysis/analysisRanking';
import panelStyles from '../analysis/AnalysisPanel.module.scss';
import styles from './ModelSpeedChart.module.scss';

export function ModelSpeedLegend({ items, highlight, onInteract }: {
  items: readonly { model: string; color: string; mean: number | null }[];
  highlight?: ReturnType<typeof useAnalysisHighlight>;
  onInteract?: () => void;
}) {
  const { t } = useTranslation();
  if (items.length === 0) return null;
  return <ul className={styles.legend} data-model-speed-legend aria-label={t('usage_stats.model_speed_models')}>
    {items.map((item) => {
      const content = <>
        <span className={panelStyles.analysisLegendDot} style={{ backgroundColor: item.color }} aria-hidden="true" />
        <span className={panelStyles.analysisLegendLabel}>{item.model}</span>
        <strong className={styles.legendValue}>{item.mean === null ? t('usage_stats.no_data') : `${item.mean.toFixed(1)} t/s`}</strong>
      </>;
      const className = `${panelStyles.analysisLegendItem} ${styles.legendItem}`;
      if (!highlight) return <li key={item.model} title={item.model} className={className}>{content}</li>;
      const events = highlight.events(item.model);
      return <li key={item.model} title={item.model} className={styles.interactiveLegendItem}>
        <button type="button"
          className={`${className} ${styles.legendButton}`}
          aria-pressed={highlight.selected === item.model}
          data-highlighted={highlight.active === item.model || undefined}
          data-muted={Boolean(highlight.active && highlight.active !== item.model) || undefined}
          {...events}
          onPointerEnter={(event) => { onInteract?.(); events.onPointerEnter(event); }}
          onFocus={() => { onInteract?.(); events.onFocus(); }}
          onClick={() => { onInteract?.(); events.onClick(); }}
        >{content}</button>
      </li>;
    })}
  </ul>;
}
