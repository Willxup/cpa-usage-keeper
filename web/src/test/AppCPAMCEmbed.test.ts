import { existsSync, readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const appSource = readFileSync(new URL('../App.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const appStyles = readFileSync(new URL('../App.css', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const embedScript = readFileSync(new URL('../embed/cpamcEmbed.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const embedStylesPath = new URL('../embed/cpamcEmbed.css', import.meta.url);
const pageLayoutStyles = readFileSync(new URL('../components/layout/PageLayout.module.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const shellStyles = readFileSync(new URL('../components/layout/AppShell.module.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const themeStyles = readFileSync(new URL('../styles/themes.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('App CPAMC embed shell', () => {
  it('resolves the CPAMC embed mode into the typed embed variant instead of a chrome patch', () => {
    expect(appSource).toContain("import { cpamcEmbedSearch, isCPAMCEmbed, notifyCPAMCEmbedReady } from './embed/cpamcEmbed';");
    expect(appSource).toContain('const isEmbeddedInCPAMC = isCPAMCEmbed();');
    expect(appSource).toMatch(/const shellVariant: AppShellVariant = isEmbeddedInCPAMC[\s\S]*?\?\s*'embed'/);
    expect(appSource).toContain('data-shell-variant={shellVariant}');
    expect(appSource).not.toContain('data-embed');
    expect(appSource).not.toContain('cpamcEmbed.css');
    expect(existsSync(embedStylesPath)).toBe(false);
  });

  it('lets the AppShell embed variant own embed chrome and density', () => {
    expect(shellStyles).toContain(".appShell[data-variant='embed'][data-nav='none']");
    expect(shellStyles).toContain(".appShell[data-density='compact']");
  });

  it('keeps one shared page width constraint for normal and CPAMC modes', () => {
    expect(pageLayoutStyles).toContain('width: min(var(--page-max-width), 100%);');
    expect(themeStyles).toContain('--page-max-width: 1440px;');
    expect(appStyles).not.toContain('--keeper-page-max-width');
    expect(appStyles).not.toContain('.app-frame:not([data-embed])');
  });

  it('uses native layout without legacy density or root zoom variables', () => {
    expect(appStyles).not.toContain('--keeper-density');
    expect(appStyles).not.toContain('--keeper-ui-zoom');
    expect(appStyles).not.toContain('zoom:');
    expect(appStyles).not.toContain('transform:');
    expect(appStyles).not.toContain('scale(');
    expect(appStyles).not.toContain('calc(100svh / var(--keeper-ui-zoom))');
  });

  it('keeps the CPAMC embed detection, canonical query, and ready postMessage contract', () => {
    expect(embedScript).toContain("params.getAll(name).includes(CPAMC_EMBED_QUERY_VALUE)");
    expect(embedScript).toContain("isCPAMCEmbed(search) ? '?embed=cpamc' : ''");
    expect(embedScript).toContain("window.parent.postMessage({ type: CPAMC_READY_MESSAGE }, '*')");
  });

  it('preserves the CPAMC embed query when normalizing app paths', () => {
    const replaceStateTargets = Array.from(appSource.matchAll(/window\.history\.replaceState\(null, '', ([\s\S]*?)\);/g)).map((match) => match[1]);

    expect(replaceStateTargets).toHaveLength(3);
    replaceStateTargets.forEach((target) => {
      expect(target).toContain('appPath(');
      expect(target).toContain('+ cpamcEmbedSearch()');
    });
  });
});
