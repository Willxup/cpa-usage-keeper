import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import i18n, { isSupportedLanguage, persistLanguage, type SupportedLanguage } from '@/i18n';
import styles from './LanguageSwitcher.module.scss';

const LANGUAGE_OPTIONS: ReadonlyArray<{ value: SupportedLanguage; label: string }> = [
  { value: 'en', label: 'EN' },
  { value: 'zh', label: '中' },
  { value: 'zh-TW', label: '繁' }
];

export function LanguageSwitcher({ className = '' }: { className?: string }) {
  const { t } = useTranslation();
  const currentLanguage = isSupportedLanguage(i18n.language) ? i18n.language : 'en';

  const handleLanguageChange = useCallback(async (language: SupportedLanguage) => {
    if (currentLanguage === language) return;
    await i18n.changeLanguage(language);
    persistLanguage(language);
  }, [currentLanguage]);

  const switcherClassName = `${styles.languageSwitcher} ${className}`.trim();

  return (
    <div className={switcherClassName} role="group" aria-label={t('usage_stats.language_switch')}>
      {LANGUAGE_OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          className={`${styles.languagePill} ${currentLanguage === option.value ? styles.languagePillActive : ''}`.trim()}
          onClick={() => void handleLanguageChange(option.value)}
          aria-label={`${t('usage_stats.language_switch')} ${option.label}`}
          aria-pressed={currentLanguage === option.value}
          title={t('usage_stats.language_switch')}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
