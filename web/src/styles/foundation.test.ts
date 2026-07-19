import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const readSource = (url: URL) => readFileSync(url, 'utf8').replace(/\r\n/g, '\n');

const provider = readSource(new URL('../theme/AntdProvider.tsx', import.meta.url));
const themes = readSource(new URL('./themes.scss', import.meta.url));
const variables = readSource(new URL('./variables.scss', import.meta.url));
const mixins = readSource(new URL('./mixins.scss', import.meta.url));
const reset = readSource(new URL('./reset.scss', import.meta.url));
const indexCss = readSource(new URL('../index.css', import.meta.url));

describe('layout and typography foundation', () => {
  it('uses one application font variable for Ant Design, body, and native controls', () => {
    expect(themes).toContain("'Noto Sans SC', 'Noto Sans TC', 'Inter'")
    expect(provider).toContain("fontFamily: 'var(--app-font-family)'")
    expect(variables).toContain('$font-family: var(--app-font-family);')
    expect(reset).toContain('font-family: $font-family;')
    expect(indexCss).toContain('font-family: var(--app-font-family);')
    expect(reset).toMatch(/button,[\s\S]*?select\s*\{[\s\S]*?font:\s*inherit;/)
    expect(indexCss).toMatch(/button,[\s\S]*?textarea\s*\{[\s\S]*?font:\s*inherit;/)
    expect(themes).toContain('--app-data-font-family:')
  })

  it('defines semantic page, surface, toolbar, and typography variables', () => {
    ;[
      '--page-max-width: 1440px;',
      '--app-shell-max-width: calc(var(--page-max-width) + 232px);',
      '--page-gutter: 24px;',
      '--page-gutter-mobile: 12px;',
      '--page-stack: 20px;',
      '--section-stack: 16px;',
      '--surface-inset-standard: 24px;',
      '--surface-inset-compact: 16px;',
      '--table-inset: 24px;',
      '--toolbar-gap: 12px;',
      '--control-gap: 8px;',
      '--type-page-title-size: 20px;',
      '--type-section-title-size: 18px;',
      '--type-subsection-title-size: 16px;',
      '--type-body-size: 14px;',
      '--type-caption-size: 12px;',
      '--type-metric-size: 28px;',
    ].forEach((token) => expect(themes).toContain(token))
  })

  it('names responsive ranges explicitly while preserving historical aliases', () => {
    expect(variables).toContain('$breakpoint-mobile-max: 768px;')
    expect(variables).toContain('$breakpoint-tablet-min: $breakpoint-mobile-max + 1px;')
    expect(variables).toContain('$breakpoint-tablet-max: 1024px;')
    expect(variables).toContain('$breakpoint-desktop-min: $breakpoint-tablet-max + 1px;')
    expect(variables).toContain('$breakpoint-wide-min: 1280px;')
    expect(variables).toContain('$breakpoint-login-split-min: 960px;')
    expect(variables).toContain('$breakpoint-mobile: $breakpoint-mobile-max;')
    expect(variables).toContain('$breakpoint-tablet: $breakpoint-tablet-max;')
    expect(variables).toContain('$breakpoint-desktop: $breakpoint-wide-min;')
    expect(mixins).toContain('@mixin desktop')
    expect(mixins).toContain('@media (min-width: #{$breakpoint-desktop-min})')
    expect(mixins).toContain('@mixin login-split')
  })

  it('uses standard Ant Design density rather than the compact algorithm', () => {
    expect(provider).not.toContain('compactAlgorithm')
    expect(provider).toContain('controlHeight: 36')
    expect(provider).toContain('labelFontSize: 14')
  })

  it('sets standard Card and small Table density through Ant Design tokens', () => {
    expect(provider).toContain('bodyPadding: 24')
    expect(provider).toContain('bodyPaddingSM: 16')
    expect(provider).toContain('cellPaddingBlockSM: 8')
    expect(provider).toContain('cellPaddingInlineSM: 16')
  })

  it('keeps default Ant Design tags neutral on light and dark surfaces', () => {
    expect(provider).toMatch(/Tag:\s*\{[\s\S]*?defaultBg:\s*'var\(--surface-inset\)'/)
    expect(provider).toMatch(/Tag:\s*\{[\s\S]*?defaultColor:\s*'var\(--text-secondary\)'/)
    expect(themes).toMatch(/:root\s*\{[\s\S]*?--surface-inset:\s*#edf2f7;/)
    expect(themes).toMatch(/\[data-theme='dark'\]\s*\{[\s\S]*?--surface-inset:\s*#111824;/)
  })
});
