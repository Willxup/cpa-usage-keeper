import { useTranslation } from 'react-i18next';
import type { RankingScope } from '../types';
import styles from './RankingScopeSwitch.module.scss';

export interface RankingScopeSwitchProps {
  value: RankingScope;
  onChange: (scope: RankingScope) => void;
}

const OPTIONS: ReadonlyArray<{ value: RankingScope; labelKey: string }> = [
  { value: 'local', labelKey: 'ranking.scope_local' },
  { value: 'community', labelKey: 'ranking.scope_community' },
];

export function RankingScopeSwitch({ value, onChange }: RankingScopeSwitchProps) {
  const { t } = useTranslation();

  return (
    <div
      className={`${styles.switch} ${value === 'community' ? styles.switchCommunity : ''}`.trim()}
      role="group"
      aria-label={t('ranking.scope_label')}
      data-ranking-scope-switch
      data-ranking-scope={value}
    >
      <span className={styles.indicator} aria-hidden="true" />
      {OPTIONS.map((option) => {
        const active = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            className={`${styles.option} ${active ? styles.optionActive : ''}`.trim()}
            aria-pressed={active}
            onClick={() => onChange(option.value)}
            data-ranking-scope-option={option.value}
          >
            <span>{t(option.labelKey)}</span>
          </button>
        );
      })}
    </div>
  );
}
