import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { getRoleTargetPath, shouldNormalizeRolePath } from '../App';

const appSource = readFileSync(new URL('../App.tsx', import.meta.url), 'utf8');

describe('App usage-page route authorization', () => {
  it('allows only known usage routes for administrators', () => {
    expect(getRoleTargetPath('admin', '/')).toBe('/');
    expect(getRoleTargetPath('admin', '/auth-files')).toBe('/auth-files');
    expect(getRoleTargetPath('admin', '/auth-files/')).toBe('/');
    expect(shouldNormalizeRolePath('admin', '/auth-files/')).toBe(true);
    expect(getRoleTargetPath('admin', '/request-events')).toBe('/request-events');
    expect(getRoleTargetPath('admin', '/auth-files/settings')).toBe('/');
    expect(getRoleTargetPath('admin', '//example.com/auth-files')).toBe('/');
  });

  it('keeps API key viewers isolated from administrator routes', () => {
    expect(getRoleTargetPath('api_key_viewer', '/auth-files')).toBe('/key-overview');
    expect(shouldNormalizeRolePath('api_key_viewer', '/auth-files')).toBe(true);
    expect(shouldNormalizeRolePath('api_key_viewer', '/key-overview')).toBe(false);
    expect(getRoleTargetPath('api_key_viewer', '/key-analysis')).toBe('/key-analysis');
    expect(shouldNormalizeRolePath('api_key_viewer', '/key-analysis')).toBe(false);
    expect(getRoleTargetPath('api_key_viewer', '/key-analysis/')).toBe('/key-overview');
    expect(getRoleTargetPath('api_key_viewer', '//example.com/key-analysis')).toBe('/key-overview');
  });

  it('keeps Ranking unavailable in CPAMC embed mode', () => {
    expect(getRoleTargetPath('admin', '/ranking', true)).toBe('/');
    expect(shouldNormalizeRolePath('admin', '/ranking', true)).toBe(true);
    expect(getRoleTargetPath('admin', '/analysis', true)).toBe('/analysis');
  });

  it('does not create viewer history entries without a popstate consumer', () => {
    expect(appSource).not.toContain('window.history.pushState');
    expect(appSource).toMatch(/handleKeyViewerNavigate[\s\S]*?window\.history\.replaceState/);
  });

  it('preserves an allowed viewer path after API key login', () => {
    const loginHandler = appSource.slice(
      appSource.indexOf('const handleAPIKeyLogin'),
      appSource.indexOf('const handleKeyViewerNavigate'),
    );

    expect(loginHandler).toContain('stripAppBasePath(window.location.pathname');
    expect(loginHandler).toContain('getRoleTargetPath(session.role, currentPath, isEmbeddedInCPAMC)');
    expect(loginHandler).toContain('setKeyViewerPath(targetPath);');
    expect(loginHandler).toContain('appPath(targetPath)');
  });
});
