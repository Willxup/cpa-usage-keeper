import { useMemo, useState } from 'react';
import { Alert, Button } from 'antd';
import { useTranslation } from 'react-i18next';
import styles from './PricingCoverageNotice.module.scss';

interface PricingCoverageNoticeProps {
  models: string[];
  onConfigure?: () => void;
}

export function normalizeUnpricedModels(models: string[]): string[] {
  return Array.from(new Set(models.map((model) => model.trim()).filter(Boolean)))
    .sort((left, right) => left.localeCompare(right));
}

export function PricingCoverageNotice({ models, onConfigure }: PricingCoverageNoticeProps) {
  const { t } = useTranslation();
  const normalizedModels = useMemo(() => normalizeUnpricedModels(models), [models]);
  const signature = normalizedModels.join('\u0000');
  const [dismissedSignature, setDismissedSignature] = useState('');

  if (!signature || dismissedSignature === signature) {
    return null;
  }

  return (
    <aside className={styles.notice} aria-label={t('usage_stats.pricing_coverage_title')}>
      <Alert
        type="warning"
        showIcon
        closable
        title={t('usage_stats.pricing_coverage_title')}
        description={(
          <div className={styles.description}>
            <p>{t('usage_stats.pricing_coverage_description', { count: normalizedModels.length })}</p>
            <div className={styles.modelList}>
              {normalizedModels.map((model) => <code key={model}>{model}</code>)}
            </div>
          </div>
        )}
        action={onConfigure ? (
          <Button size="small" onClick={onConfigure}>
            {t('usage_stats.pricing_coverage_action')}
          </Button>
        ) : undefined}
        onClose={() => setDismissedSignature(signature)}
      />
    </aside>
  );
}
