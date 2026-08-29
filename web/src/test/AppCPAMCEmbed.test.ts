import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const appSource = readFileSync(new URL('../App.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const appStyles = readFileSync(new URL('../App.css', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const embedStyles = readFileSync(new URL('../embed/cpamcEmbed.css', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const appFrameBlock = appStyles.match(/\.app-frame\s*\{[\s\S]*?\n\}/)?.[0] ?? '';

describe('App CPAMC embed shell', () => {
  it('loads the scoped CPAMC embed stylesheet and marks the app frame', () => {
    expect(appSource).toContain("import './embed/cpamcEmbed.css';");
    expect(appSource).toMatch(/<div className="app-frame" data-embed=\{isEmbeddedInCPAMC \? 'cpamc' : undefined\}>/);
  });

  it('shares the dynamic page width cap across normal and CPAMC modes', () => {
    expect(appStyles).toMatch(/\.app-frame\s*\{[\s\S]*?--keeper-page-max-width:\s*clamp\(1245px, 86vw, 1600px\);/);
    expect(appStyles).not.toContain('.app-frame:not([data-embed])');
    expect(embedStyles).not.toContain('--keeper-page-max-width');
  });

  it('uses density variables instead of root-level zoom for normal and CPAMC layouts', () => {
    expect(appFrameBlock).toContain('--keeper-density: 1;');
    expect(appFrameBlock).toContain('min-height: 100svh;');
    expect(appFrameBlock).not.toContain('--keeper-ui-zoom');
    expect(appFrameBlock).not.toContain('zoom:');
    expect(appFrameBlock).not.toContain('transform:');
    expect(appFrameBlock).not.toContain('scale(');
    expect(appStyles).not.toContain('calc(100svh / var(--keeper-ui-zoom))');
    expect(embedStyles).not.toContain('--keeper-density');
    expect(embedStyles).not.toContain('zoom:');
    expect(embedStyles).not.toContain('scale(');
  });

  it('inherits the global card, modal, and title contract without embed overrides', () => {
    expect(embedStyles).not.toContain('--keeper-card-');
    expect(embedStyles).not.toMatch(/\.card(?:\s|,|\{)/);
    expect(embedStyles).not.toMatch(/\.modal(?:\s|,|\{)/);
    expect(embedStyles).not.toContain('keeper-card-title');
    expect(embedStyles).not.toContain('font-size:');
  });

  it('preserves the CPAMC embed query when normalizing app paths', () => {
    const replaceStateTargets = Array.from(appSource.matchAll(/window\.history\.replaceState\(null, '', ([\s\S]*?)\);/g)).map((match) => match[1]);

    expect(replaceStateTargets).toHaveLength(4);
    replaceStateTargets.forEach((target) => {
      expect(target).toContain('appPath(');
      expect(target).toContain('+ cpamcEmbedSearch()');
    });
  });
});
