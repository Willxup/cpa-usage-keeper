import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const appSource = readFileSync(new URL('../App.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const usagePageSource = readFileSync(new URL('../pages/UsagePage.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const keyOverviewPageSource = readFileSync(new URL('../pages/KeyOverviewPage.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const loginPageSource = readFileSync(new URL('../pages/LoginPage.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const appShellSource = readFileSync(new URL('../components/layout/AppShell.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('CPAMC embed shell wiring', () => {
  it('provides the embed flag once from App through context', () => {
    expect(appSource).toContain("import { EmbedContext } from './embed/EmbedContext';");
    expect(appSource).toContain('<EmbedContext.Provider value={isEmbeddedInCPAMC}>');
  });

  it('every page consumes the embed flag and collapses its shell variant', () => {
    for (const [name, source, base] of [
      ['UsagePage', usagePageSource, 'authenticated'],
      ['KeyOverviewPage', keyOverviewPageSource, 'viewer'],
      ['LoginPage', loginPageSource, 'guest'],
    ] as const) {
      expect(source, `${name} should read the embed flag`).toContain("import { useEmbed } from '@/embed/EmbedContext';");
      expect(source, `${name} should call useEmbed`).toContain('useEmbed()');
      expect(source, `${name} should collapse to embed variant`).toContain(`variant={embedded ? 'embed' : '${base}'}`);
    }
  });

  it('the AppShell embed variant always hides chrome', () => {
    expect(appShellSource).toContain("const chromeHidden = variant === 'embed';");
    expect(appShellSource).not.toContain("variant === 'embed' && navMode === 'none'");
  });
});
