import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../AnalysisPanel.module.scss', import.meta.url), 'utf8');

const scssRule = (selector: string) => {
  const needle = `\n${selector} {`;
  const match = styles.indexOf(needle);
  const start = match < 0 ? -1 : match + 1;
  expect(start).toBeGreaterThanOrEqual(0);
  const openingBrace = styles.indexOf('{', start + selector.length);
  expect(openingBrace).toBeGreaterThan(start);

  let depth = 1;
  for (let index = openingBrace + 1; index < styles.length; index += 1) {
    if (styles[index] === '{') depth += 1;
    if (styles[index] === '}' && --depth === 0) return styles.slice(start, index + 1);
  }
  throw new Error(`Unclosed SCSS rule: ${selector}`);
};

describe('AnalysisPanel inner element radii', () => {
  it('uses the shared card radius for chart surfaces', () => {
    expect(scssRule('.analysisChartSurface')).toContain('border-radius: var(--keeper-card-radius);');
  });

  it('uses the project pill radius for model and distribution controls', () => {
    expect(scssRule('.topModelsRankItem')).toContain('border-radius: 999px;');
    expect(scssRule('.compositionTab')).toContain('border-radius: 999px;');
  });
});
