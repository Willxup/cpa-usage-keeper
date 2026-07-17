import { existsSync, readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const mainSource = readFileSync(new URL('../main.tsx', import.meta.url), 'utf8');
const indexHtml = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
const readmeEnglish = readFileSync(new URL('../../../README.md', import.meta.url), 'utf8');
const readmeChinese = readFileSync(new URL('../../../README.zh.md', import.meta.url), 'utf8');
const lightLogoUrl = new URL('../../../assets/keeper-logo-light.svg', import.meta.url);
const darkLogoUrl = new URL('../../../assets/keeper-logo-dark.svg', import.meta.url);
const lightLogo = existsSync(lightLogoUrl) ? readFileSync(lightLogoUrl, 'utf8') : '';
const darkLogo = existsSync(darkLogoUrl) ? readFileSync(darkLogoUrl, 'utf8') : '';

describe('Keeper branding assets', () => {
  it('uses the Keeper SVG as the browser favicon', () => {
    expect(mainSource).toContain("import faviconUrl from './assets/keeper-icon.svg'");
    expect(mainSource).toContain("faviconEl.type = 'image/svg+xml'");
  });

  it('uses KEEPER as the browser tab title', () => {
    expect(indexHtml).toContain('<title>KEEPER</title>');
  });

  it('keeps the theme-aware centered logo in both public READMEs', () => {
    for (const readme of [readmeEnglish, readmeChinese]) {
      expect(readme).toContain('<p align="center">');
      expect(readme).toContain('<picture>');
      expect(readme).toContain('media="(prefers-color-scheme: dark)"');
      expect(readme).toContain('./assets/keeper-logo-dark.svg');
      expect(readme).toContain('./assets/keeper-logo-light.svg');
    }
  });

  it('uses black and white wordmarks for light and dark README themes', () => {
    expect(lightLogo).toContain('fill="#111111"');
    expect(darkLogo).toContain('fill="#ffffff"');
  });
});
