import { useCallback, useMemo } from 'react';
import { BgColorsOutlined, CheckOutlined, GlobalOutlined, SettingOutlined } from '@ant-design/icons';
import { Button, Dropdown, type MenuProps } from 'antd';
import { useTranslation } from 'react-i18next';
import i18n, {
  applyDocumentLanguage,
  isSupportedLanguage,
  persistLanguage,
  type SupportedLanguage,
} from '@/i18n';
import { useThemeStore } from '@/stores';
import type { Theme } from '@/types';

const LANGUAGE_OPTIONS: ReadonlyArray<{ value: SupportedLanguage; label: string }> = [
  { value: 'en', label: 'English' },
  { value: 'zh', label: '简体中文' },
  { value: 'zh-TW', label: '繁體中文' },
];

const THEME_OPTIONS: ReadonlyArray<{ value: Theme; labelKey: string }> = [
  { value: 'light', labelKey: 'usage_stats.theme_light' },
  { value: 'white', labelKey: 'usage_stats.theme_white' },
  { value: 'dark', labelKey: 'usage_stats.theme_dark' },
  { value: 'auto', labelKey: 'usage_stats.theme_auto' },
];

type PreferenceSection = 'all' | 'language' | 'appearance';
type PreferenceMenuItem = NonNullable<MenuProps['items']>[number];

const languageItemKey = (language: SupportedLanguage) => `language:${language}`;
const themeItemKey = (theme: Theme) => `theme:${theme}`;

function PreferenceItemLabel({ label, active }: { label: string; active: boolean }) {
  return (
    <span style={{ display: 'flex', minWidth: 168, alignItems: 'center', justifyContent: 'space-between', gap: 24 }}>
      <span>{label}</span>
      {active && <CheckOutlined aria-hidden="true" />}
    </span>
  );
}

export function PreferencesDropdown({
  className,
  section = 'all',
  showLabel = false,
}: {
  className?: string;
  section?: PreferenceSection;
  showLabel?: boolean;
}) {
  const { t } = useTranslation();
  const theme = useThemeStore((state) => state.theme);
  const setTheme = useThemeStore((state) => state.setTheme);
  const currentLanguage = isSupportedLanguage(i18n.language) ? i18n.language : 'en';
  const labelKey = section === 'language' ? 'usage_stats.preferences_language' : section === 'appearance' ? 'usage_stats.preferences_appearance' : 'usage_stats.preferences_menu';
  const label = t(labelKey);
  const icon = section === 'language' ? <GlobalOutlined /> : section === 'appearance' ? <BgColorsOutlined /> : <SettingOutlined />;

  const handleLanguageChange = useCallback(async (language: SupportedLanguage) => {
    if (currentLanguage === language) return;
    await i18n.changeLanguage(language);
    applyDocumentLanguage(language);
    persistLanguage(language);
  }, [currentLanguage]);

  const languageGroup = useMemo<PreferenceMenuItem>(() => ({
    type: 'group',
    label: t('usage_stats.preferences_language'),
    children: LANGUAGE_OPTIONS.map((option) => ({
      key: languageItemKey(option.value),
      label: <PreferenceItemLabel label={option.label} active={currentLanguage === option.value} />,
    })),
  }), [currentLanguage, t]);

  const appearanceGroup = useMemo<PreferenceMenuItem>(() => ({
    type: 'group',
    label: t('usage_stats.preferences_appearance'),
    children: THEME_OPTIONS.map((option) => ({
      key: themeItemKey(option.value),
      label: <PreferenceItemLabel label={t(option.labelKey)} active={theme === option.value} />,
    })),
  }), [t, theme]);

  const items = useMemo<MenuProps['items']>(() => {
    if (section === 'language') return [languageGroup];
    if (section === 'appearance') return [appearanceGroup];
    return [languageGroup, { type: 'divider' }, appearanceGroup];
  }, [appearanceGroup, languageGroup, section]);

  const handleMenuClick: MenuProps['onClick'] = ({ key }) => {
    if (key.startsWith('language:')) {
      const language = key.slice('language:'.length);
      if (isSupportedLanguage(language)) {
        void handleLanguageChange(language);
      }
      return;
    }

    if (key.startsWith('theme:')) {
      const nextTheme = key.slice('theme:'.length) as Theme;
      if (THEME_OPTIONS.some((option) => option.value === nextTheme)) {
        setTheme(nextTheme);
      }
    }
  };

  return (
    <Dropdown
      trigger={['click']}
      placement="bottomRight"
      autoFocus
      destroyOnHidden
      menu={{
        items,
        multiple: true,
        selectedKeys: [languageItemKey(currentLanguage), themeItemKey(theme)],
        onClick: handleMenuClick,
      }}
    >
      <Button
        className={className}
        type="text"
        shape={showLabel ? 'default' : 'circle'}
        icon={icon}
        aria-label={label}
        title={label}
      >
        {showLabel ? label : null}
      </Button>
    </Dropdown>
  );
}
