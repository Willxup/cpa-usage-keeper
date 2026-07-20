import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { getChartTheme } from '@/lib/chartTheme';

const themes = readFileSync(new URL('./themes.scss', import.meta.url), 'utf8');

const luminance = (hex: string): number => {
  const channels = hex.slice(1).match(/.{2}/g)?.map((value) => Number.parseInt(value, 16) / 255) ?? [];
  const [red, green, blue] = channels.map((value) => (
    value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  ));
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
};

const contrast = (foreground: string, background: string): number => {
  const foregroundLuminance = luminance(foreground);
  const backgroundLuminance = luminance(background);
  return (Math.max(foregroundLuminance, backgroundLuminance) + 0.05)
    / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
};

describe('Keeper semantic color system', () => {
  it('keeps interaction, status, and metric identity as separate token roles', () => {
    [
      '--interactive-primary:',
      '--interactive-selected:',
      '--status-success:',
      '--status-warning:',
      '--status-danger:',
      '--data-requests:',
      '--data-tokens:',
      '--data-rpm:',
      '--data-tpm:',
      '--data-cache:',
      '--data-cost:',
    ].forEach((token) => expect(themes).toContain(token));
  });

  it('keeps normal text at WCAG AA contrast on raised surfaces', () => {
    const lightSurface = '#ffffff';
    const darkSurface = '#17202c';

    ['#192230', '#566477', '#64748b'].forEach((color) => {
      expect(contrast(color, lightSurface)).toBeGreaterThanOrEqual(4.5);
    });
    ['#f1f5f9', '#aab6c5', '#7e8b9d'].forEach((color) => {
      expect(contrast(color, darkSurface)).toBeGreaterThanOrEqual(4.5);
    });
  });

  it('keeps control boundaries and chart strokes above non-text contrast', () => {
    expect(contrast('#8190a5', '#ffffff')).toBeGreaterThanOrEqual(3);
    expect(contrast('#5b6d87', '#17202c')).toBeGreaterThanOrEqual(3);

    Object.values(getChartTheme(false).series).forEach(({ stroke }) => {
      expect(contrast(stroke, '#ffffff')).toBeGreaterThanOrEqual(3);
    });
    Object.values(getChartTheme(true).series).forEach(({ stroke }) => {
      expect(contrast(stroke, '#17202c')).toBeGreaterThanOrEqual(3);
    });
  });

  it('keeps dark surfaces ordered from canvas to overlay', () => {
    const surfaces = ['#080b10', '#0d131c', '#111925', '#17202c', '#202b3a'];
    const values = surfaces.map(luminance);
    values.slice(1).forEach((value, index) => expect(value).toBeGreaterThan(values[index]));
  });
});
