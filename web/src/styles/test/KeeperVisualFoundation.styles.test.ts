import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const themes = readFileSync(new URL('../themes.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const reset = readFileSync(new URL('../reset.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const components = readFileSync(new URL('../components.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('Keeper visual foundation', () => {
  it('defines the global card, typography, and control scale', () => {
    expect(themes).toContain('--keeper-card-radius: 24px;');
    expect(themes).toContain('--keeper-card-padding: 20px;');
    expect(themes).toContain('--keeper-card-title-size: 18px;');
    expect(themes).toContain('--keeper-card-subtitle-size: 12px;');
    expect(themes).toContain('--keeper-card-subtitle-weight: 400;');
    expect(themes).toContain('--keeper-body-font-size: 12px;');
    expect(themes).toContain('--keeper-control-font-size: 12px;');
    expect(themes).toContain('--keeper-control-height-sm: 32px;');
    expect(themes).toContain('--keeper-control-height-md: 36px;');
    expect(themes).toContain('--keeper-toolbar-control-height: 42px;');
    expect(reset).toMatch(/body\s*\{[\s\S]*?font-size:\s*var\(--keeper-body-font-size\);/);
  });

  it('makes cards, modals, titles, subtitles, and buttons consume the global contract', () => {
    expect(components).toMatch(/\.keeper-card-surface\s*\{[\s\S]*?border-radius:\s*var\(--keeper-card-radius\);/);
    expect(components).toMatch(/\.card\s*\{[\s\S]*?padding:\s*var\(--keeper-card-padding\);/);
    expect(components).toMatch(/\.card-flush\s*\{[\s\S]*?padding:\s*0;/);
    expect(components).toMatch(/\.keeper-card-title\s*\{[\s\S]*?font-size:\s*var\(--keeper-card-title-size\);[\s\S]*?font-weight:\s*var\(--keeper-card-title-weight\);/);
    expect(components).toMatch(/\.keeper-card-subtitle\s*\{[\s\S]*?font-size:\s*var\(--keeper-card-subtitle-size\);[\s\S]*?font-weight:\s*var\(--keeper-card-subtitle-weight\);/);
    expect(components).toMatch(/\.btn\s*\{[\s\S]*?min-height:\s*var\(--keeper-control-height-md\);[\s\S]*?font-size:\s*var\(--keeper-control-font-size\);/);
    expect(components).toMatch(/&\.btn-sm\s*\{[\s\S]*?min-height:\s*var\(--keeper-control-height-sm\);[\s\S]*?font-size:\s*var\(--keeper-control-font-size\);/);
    expect(components).toMatch(/\.modal\s*\{[\s\S]*?border-radius:\s*var\(--keeper-card-radius\);/);
    expect(components).toMatch(/@media \(max-width:\s*\$breakpoint-mobile\)[\s\S]*?\.modal\s*\{[\s\S]*?border-radius:\s*var\(--keeper-card-radius\);/);
  });
});
