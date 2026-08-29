// @vitest-environment happy-dom

import { describe, expect, it, vi } from 'vitest';
import {
  getUsageTabPath,
  handleUsageTabKeyActivation,
  normalizeUsageTabValue,
  resolveInitialUsageTab,
  resolveUsageTabFromPath,
  shouldHandleUsageNavigation,
  stripAppBasePath,
} from '../usageNavigation';

describe('usage page navigation', () => {
  it('maps only the fixed public page paths to existing tabs', () => {
    const mappings = [
      ['overview', '/overview'],
      ['analysis', '/analysis'],
      ['ranking', '/ranking'],
      ['events', '/request-events'],
      ['auth-files', '/auth-files'],
      ['ai-provider', '/ai-provider'],
      ['settings', '/settings'],
    ] as const;

    for (const [tab, path] of mappings) {
      expect(getUsageTabPath(tab)).toBe(path);
      expect(resolveUsageTabFromPath(path)).toBe(tab);
    }
    expect(resolveUsageTabFromPath('/auth-files/')).toBeNull();
  });

  it('rejects unknown, nested, encoded, and URL-shaped path input', () => {
    expect(resolveUsageTabFromPath('/')).toBeNull();
    expect(resolveUsageTabFromPath('/auth-files/settings')).toBeNull();
    expect(resolveUsageTabFromPath('/auth-files%2Fsettings')).toBeNull();
    expect(resolveUsageTabFromPath('/auth-files/../settings')).toBeNull();
    expect(resolveUsageTabFromPath('/auth-files\\settings')).toBeNull();
    expect(resolveUsageTabFromPath('/%2e%2e/auth-files')).toBeNull();
    expect(resolveUsageTabFromPath('//example.com/auth-files')).toBeNull();
    expect(resolveUsageTabFromPath('https://example.com/auth-files')).toBeNull();
  });

  it('keeps the legacy stored credential tab alias without accepting it as a route', () => {
    expect(normalizeUsageTabValue('credentials')).toBe('auth-files');
    expect(resolveUsageTabFromPath('/credentials')).toBeNull();
  });

  it('lets a valid direct route override storage while keeping root storage behavior', () => {
    expect(resolveInitialUsageTab('/cpa/auth-files', '/cpa', 'analysis')).toBe('auth-files');
    expect(resolveInitialUsageTab('/cpa/', '/cpa', 'analysis')).toBe('analysis');
    expect(resolveInitialUsageTab('/cpa/', '/cpa', 'credentials')).toBe('auth-files');
    expect(resolveInitialUsageTab('/cpa/', '/cpa', 'unknown')).toBe('overview');
  });

  it('strips only the configured app base-path boundary', () => {
    expect(stripAppBasePath('/cpa/auth-files', '/cpa')).toBe('/auth-files');
    expect(stripAppBasePath('/cpa', '/cpa/')).toBe('/');
    expect(stripAppBasePath('/cpattack/auth-files', '/cpa')).toBeNull();
    expect(stripAppBasePath('/auth-files', '/cpa')).toBeNull();
    expect(stripAppBasePath('/auth-files', undefined)).toBe('/auth-files');
  });

  it('intercepts only an unmodified primary-button navigation', () => {
    const primaryClick = {
      button: 0,
      defaultPrevented: false,
      altKey: false,
      ctrlKey: false,
      metaKey: false,
      shiftKey: false,
    };

    expect(shouldHandleUsageNavigation(primaryClick)).toBe(true);
    expect(shouldHandleUsageNavigation({ ...primaryClick, button: 1 })).toBe(false);
    expect(shouldHandleUsageNavigation({ ...primaryClick, ctrlKey: true })).toBe(false);
    expect(shouldHandleUsageNavigation({ ...primaryClick, metaKey: true })).toBe(false);
    expect(shouldHandleUsageNavigation({ ...primaryClick, shiftKey: true })).toBe(false);
    expect(shouldHandleUsageNavigation({ ...primaryClick, defaultPrevented: true })).toBe(false);
  });

  it('activates a focused tab link on Space without allowing page scroll', () => {
    const link = document.createElement('a');
    link.href = '/auth-files';
    document.body.appendChild(link);
    link.focus();

    const activate = vi.fn();
    link.addEventListener('keydown', (event) => {
      handleUsageTabKeyActivation(event, 'auth-files', activate);
    });

    const spaceEvent = new KeyboardEvent('keydown', {
      key: ' ',
      bubbles: true,
      cancelable: true,
    });
    expect(document.activeElement).toBe(link);
    expect(link.dispatchEvent(spaceEvent)).toBe(false);
    expect(spaceEvent.defaultPrevented).toBe(true);
    expect(activate).toHaveBeenCalledOnce();
    expect(activate).toHaveBeenCalledWith('auth-files');

    const enterEvent = new KeyboardEvent('keydown', {
      key: 'Enter',
      bubbles: true,
      cancelable: true,
    });
    expect(link.dispatchEvent(enterEvent)).toBe(true);
    expect(enterEvent.defaultPrevented).toBe(false);
    expect(activate).toHaveBeenCalledOnce();

    link.remove();
  });
});
