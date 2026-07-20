import { type PropsWithChildren, useMemo } from 'react';
import { App as AntdApp, ConfigProvider, theme as antdTheme, type ThemeConfig } from 'antd';
import enUS from 'antd/locale/en_US';
import zhCN from 'antd/locale/zh_CN';
import zhTW from 'antd/locale/zh_TW';
import { useTranslation } from 'react-i18next';
import { useThemeStore } from '@/stores';

const localeByLanguage = {
  en: enUS,
  zh: zhCN,
  'zh-TW': zhTW,
} as const;

const themeColors = {
  light: {
    primary: '#2563eb',
    success: '#15803d',
    warning: '#b45309',
    danger: '#be123c',
    textPrimary: '#192230',
    textSecondary: '#566477',
  },
  dark: {
    primary: '#5b9cff',
    success: '#34d399',
    warning: '#fbbf24',
    danger: '#fb7185',
    textPrimary: '#f1f5f9',
    textSecondary: '#aab6c5',
  },
} as const;

const getThemeConfig = (resolvedTheme: 'light' | 'dark'): ThemeConfig => {
  const isDark = resolvedTheme === 'dark';
  const colors = themeColors[resolvedTheme];

  return {
    algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
    cssVar: { prefix: 'keeper' },
    hashed: true,
    token: {
      colorPrimary: colors.primary,
      colorInfo: colors.primary,
      colorSuccess: colors.success,
      colorWarning: colors.warning,
      colorError: colors.danger,
      colorBgLayout: 'var(--surface-content)',
      colorBgContainer: 'var(--surface-raised)',
      colorBgElevated: 'var(--surface-overlay)',
      colorText: colors.textPrimary,
      colorTextSecondary: colors.textSecondary,
      colorBorder: 'var(--border-structural)',
      colorBorderSecondary: 'var(--border-subtle)',
      borderRadius: 6,
      borderRadiusLG: 8,
      controlHeight: 36,
      fontFamily: 'var(--app-font-family)',
      boxShadow: 'var(--elevation-raised)',
      boxShadowSecondary: 'var(--elevation-overlay)',
    },
    components: {
      Layout: {
        bodyBg: 'var(--surface-content)',
        headerBg: 'var(--surface-header)',
        siderBg: 'var(--surface-nav)',
      },
      Menu: {
        itemBorderRadius: 6,
        itemMarginInline: 8,
        itemSelectedBg: 'var(--interactive-selected)',
        itemSelectedColor: 'var(--interactive-primary)',
        itemHoverBg: 'var(--bg-hover)',
      },
      Card: {
        borderRadiusLG: 8,
        headerHeight: 48,
        bodyPadding: 24,
        bodyPaddingSM: 16,
      },
      Table: {
        headerBg: 'var(--surface-inset)',
        headerColor: 'var(--text-primary)',
        rowHoverBg: 'var(--bg-hover)',
        cellPaddingBlockSM: 8,
        cellPaddingInlineSM: 16,
      },      Tabs: {
        horizontalItemGutter: 24,
      },
      Button: {
        fontWeight: 600,
      },
      Tag: {
        // Ant Design derives the default Tag background by blending colors.
        // Our surface tokens are CSS variables, so provide the resolved
        // semantic colors directly instead of letting that blend fall to black.
        defaultBg: 'var(--surface-inset)',
        defaultColor: 'var(--text-secondary)',
      },
      Form: {
        labelFontSize: 14,
        itemMarginBottom: 24,
      },
    },
  };
};

export function AntdProvider({ children }: PropsWithChildren) {
  const { i18n } = useTranslation();
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const themeConfig = useMemo(() => getThemeConfig(resolvedTheme), [resolvedTheme]);
  const locale = localeByLanguage[i18n.resolvedLanguage as keyof typeof localeByLanguage] ?? enUS;

  return (
    <ConfigProvider
      locale={locale}
      theme={themeConfig}
      componentSize="middle"
      getPopupContainer={() => document.body}
    >
      <AntdApp>{children}</AntdApp>
    </ConfigProvider>
  );
}
