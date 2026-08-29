import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { getRoleHomePath, shouldNormalizeRolePath } from './App';

const appSource = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');
const appStylesSource = readFileSync(new URL('./App.css', import.meta.url), 'utf8');

describe('App role route normalization', () => {
  it('normalizes restored admin sessions away from the API Key viewer route', () => {
    expect(getRoleHomePath('admin')).toBe('/');
    expect(shouldNormalizeRolePath('admin', '/key-overview')).toBe(true);
    expect(shouldNormalizeRolePath('admin', '/')).toBe(false);
  });

  it('normalizes restored API Key viewer sessions to the key overview route', () => {
    expect(getRoleHomePath('api_key_viewer')).toBe('/key-overview');
    expect(shouldNormalizeRolePath('api_key_viewer', '/')).toBe(true);
    expect(shouldNormalizeRolePath('api_key_viewer', '/key-overview')).toBe(false);
  });

  it('clears stale overview auth errors when the session is cleared', () => {
    expect(appSource).toContain("import { useUsageStatsStore } from './stores/useUsageStatsStore';");
    expect(appSource).toMatch(/const clearUsageStats = useUsageStatsStore\(\(state\) => state\.clearUsageStats\);/);
    expect(appSource).toMatch(/const clearSession = useCallback\(\(\) => \{[\s\S]*?clearUsageStats\(\);[\s\S]*?setAuthState\('unauthenticated'\);/);
  });

  it('mounts the shared footer from the app shell', () => {
    expect(appSource).toContain("import './App.css';");
    expect(appSource).toContain("import { AppFooter } from './components/AppFooter';");
    expect(appSource).toMatch(/<div className="app-frame"[^>]*>[\s\S]*<main className="app-main">\{page\}<\/main>[\s\S]*<AppFooter loadVersion=\{authState === 'authenticated'\} \/>[\s\S]*<\/div>/);
  });

  it('lets app pages fill the space above the shared footer', () => {
    expect(appStylesSource).toMatch(/\.app-main\s*\{[\s\S]*?display:\s*flex;/);
    expect(appStylesSource).toMatch(/\.app-main\s*\{[\s\S]*?flex-direction:\s*column;/);
  });
});
