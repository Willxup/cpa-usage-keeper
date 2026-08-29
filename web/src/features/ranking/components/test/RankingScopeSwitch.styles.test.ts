import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../RankingScopeSwitch.module.scss', import.meta.url), 'utf8');

describe('RankingScopeSwitch styles', () => {
  it('uses the project pill shape without square option chrome', () => {
    expect(styles).toMatch(/\.switch\s*\{[\s\S]*?width:\s*196px;[\s\S]*?border-radius:\s*999px;/);
    expect(styles).toMatch(/\.indicator\s*\{[\s\S]*?border-radius:\s*999px;/);
    expect(styles).toMatch(/\.option\s*\{[\s\S]*?border-radius:\s*999px;/);
    expect(styles).not.toContain('.optionDot');
  });

  it('uses an Auth Files-style neutral selected surface with stronger contrast', () => {
    expect(styles).toMatch(/\.switch\s*\{[\s\S]*?border:\s*1px solid var\(--border-color\);[\s\S]*?background:\s*color-mix\(in srgb, var\(--bg-secondary\) 84%, transparent\);/);
    expect(styles).toMatch(/\.indicator\s*\{[\s\S]*?border:\s*1px solid color-mix\(in srgb, var\(--text-primary\) 18%, var\(--border-color\)\);[\s\S]*?background:\s*var\(--bg-primary\);/);
    expect(styles).toMatch(/\.indicator\s*\{[\s\S]*?box-shadow:[\s\S]*?0 6px 14px rgba\(0, 0, 0, 0\.1\),/);
    expect(styles).toMatch(/\.optionActive\s*\{[\s\S]*?color:\s*var\(--text-primary\);[\s\S]*?font-weight:\s*800;/);
  });
});
