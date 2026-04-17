import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import styles from '@/pages/UsagePage.module.scss';

const formatDisplayName = (value: string, allTrafficLabel: string): string => {
  const normalized = value.trim();
  if (!normalized) return '-';
  if (normalized === 'all') return allTrafficLabel;
  return normalized;
};

export interface ChartLineSelectorProps {
  chartLines: string[];
  modelNames: string[];
  maxLines?: number;
  onChange: (lines: string[]) => void;
}

export function ChartLineSelector({
  chartLines,
  modelNames,
  maxLines = 9,
  onChange
}: ChartLineSelectorProps) {
  const { t } = useTranslation();

  const handleAdd = () => {
    if (chartLines.length >= maxLines) return;
    const unusedModel = modelNames.find((m) => !chartLines.includes(m));
    if (unusedModel) {
      onChange([...chartLines, unusedModel]);
    } else {
      onChange([...chartLines, 'all']);
    }
  };

  const handleRemove = (index: number) => {
    if (chartLines.length <= 1) return;
    const newLines = [...chartLines];
    newLines.splice(index, 1);
    onChange(newLines);
  };

  const handleChange = (index: number, value: string) => {
    const newLines = [...chartLines];
    newLines[index] = value;
    onChange(newLines);
  };

  const allTrafficLabel = t('usage_stats.chart_line_all_traffic');
  const options = useMemo(
    () => [
      { value: 'all', label: allTrafficLabel },
      ...modelNames.map((name) => ({ value: name, label: formatDisplayName(name, allTrafficLabel) }))
    ],
    [allTrafficLabel, modelNames]
  );

  return (
    <Card
      title={t('usage_stats.chart_line_title')}
      extra={
        <div className={styles.chartLineHeader}>
          <span className={styles.chartLineCount}>
            {chartLines.length}/{maxLines}
          </span>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleAdd}
            disabled={chartLines.length >= maxLines}
          >
            {t('usage_stats.chart_line_add')}
          </Button>
        </div>
      }
    >
      <div className={styles.chartLineList}>
        {chartLines.map((line, index) => (
          <div key={index} className={styles.chartLineItem}>
            <span className={styles.chartLineLabel}>
              {t('usage_stats.chart_line_label', { index: index + 1 })}
            </span>
            <Select
              value={line}
              options={options}
              onChange={(value) => handleChange(index, value)}
            />
            {chartLines.length > 1 && (
              <Button variant="danger" size="sm" onClick={() => handleRemove(index)}>
                {t('usage_stats.chart_line_delete')}
              </Button>
            )}
          </div>
        ))}
      </div>
      <p className={styles.chartLineHint}>
        {t('usage_stats.chart_line_hint')}
      </p>
    </Card>
  );
}
