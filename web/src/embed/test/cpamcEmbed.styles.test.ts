import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const embedStylesSource = readFileSync(new URL('../cpamcEmbed.css', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('CPAMC embed styles', () => {
  it('keeps page overrides scoped under the CPAMC embed root', () => {
    expect(embedStylesSource).toContain(".app-frame[data-embed='cpamc']");
    expect(embedStylesSource).not.toContain('back-to-cpa');
    expect(embedStylesSource).not.toMatch(/^\.app-footer\s*\{/m);
    expect(embedStylesSource).not.toMatch(/^\[data-keeper-page=/m);

    const unscopedRule = embedStylesSource
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line.endsWith('{'))
      .filter((line) => !line.startsWith('@'))
      .find((line) => !line.includes(".app-frame[data-embed='cpamc']"));

    expect(unscopedRule).toBeUndefined();
  });
});
