import { describe, expect, it } from 'vitest';
import { getRoleHomePath, getRoleTargetPath, shouldNormalizeRolePath } from '../App';

describe('App usage-page route authorization', () => {
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

  it('allows only known usage routes for administrators', () => {
    expect(getRoleTargetPath('admin', '/')).toBe('/');
    expect(getRoleTargetPath('admin', '/credentials')).toBe('/credentials');
    expect(getRoleTargetPath('admin', '/credentials/')).toBe('/');
    expect(shouldNormalizeRolePath('admin', '/credentials/')).toBe(true);
    expect(getRoleTargetPath('admin', '/request-events')).toBe('/request-events');
    expect(getRoleTargetPath('admin', '/analysis')).toBe('/analysis');
    expect(getRoleTargetPath('admin', '/credentials/settings')).toBe('/');
    expect(getRoleTargetPath('admin', '//example.com/credentials')).toBe('/');
  });

  it('keeps API key viewers isolated from administrator routes', () => {
    expect(getRoleTargetPath('api_key_viewer', '/credentials')).toBe('/key-overview');
    expect(shouldNormalizeRolePath('api_key_viewer', '/credentials')).toBe(true);
    expect(shouldNormalizeRolePath('api_key_viewer', '/key-overview')).toBe(false);
    expect(getRoleTargetPath('api_key_viewer', '/key-analysis')).toBe('/key-analysis');
    expect(shouldNormalizeRolePath('api_key_viewer', '/key-analysis')).toBe(false);
    expect(getRoleTargetPath('api_key_viewer', '/key-analysis/')).toBe('/key-overview');
    expect(getRoleTargetPath('api_key_viewer', '//example.com/key-analysis')).toBe('/key-overview');
  });
});
