import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { getRoleHomePath, shouldNormalizeRolePath } from './App';

const appSource = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');

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
});
