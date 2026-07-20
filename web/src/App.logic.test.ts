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

  it('resolves a typed shell variant once from embed state, auth state, and role', () => {
    expect(appSource).toMatch(/const shellVariant: AppShellVariant = isEmbeddedInCPAMC[\s\S]*?\?\s*'embed'[\s\S]*?\?\s*'guest'[\s\S]*?\?\s*'viewer'[\s\S]*?:\s*'authenticated';/);
    expect(appSource).toContain('data-shell-variant={shellVariant}');
    expect(appSource).not.toContain('data-embed');
    expect(appSource).not.toContain('app-frame');
  });

  it('delegates chrome to AppShell variants without mounting a shared footer', () => {
    expect(appSource).not.toContain('AppFooter');
    expect(appSource).not.toMatch(/<footer/);
    expect(appStylesSource).not.toContain('.app-footer');
  });

  it('keeps the App stylesheet to the shell host and viewport reset', () => {
    expect(appStylesSource).toMatch(/\.app-shell-host\s*\{[\s\S]*?min-height:\s*100svh;/);
    expect(appStylesSource).toMatch(/\.app-main\s*\{[\s\S]*?display:\s*flex;/);
    expect(appStylesSource).toMatch(/\.app-main\s*\{[\s\S]*?flex-direction:\s*column;/);
    expect(appStylesSource).not.toContain('background:');
  });
});
