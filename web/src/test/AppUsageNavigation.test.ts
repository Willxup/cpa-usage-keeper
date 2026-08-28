import { describe, expect, it } from 'vitest';
import { getRoleTargetPath, shouldNormalizeRolePath } from '../App';

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
  });

  it('keeps Ranking unavailable in CPAMC embed mode', () => {
    expect(getRoleTargetPath('admin', '/ranking', true)).toBe('/');
    expect(shouldNormalizeRolePath('admin', '/ranking', true)).toBe(true);
    expect(getRoleTargetPath('admin', '/analysis', true)).toBe('/analysis');
  });
});
