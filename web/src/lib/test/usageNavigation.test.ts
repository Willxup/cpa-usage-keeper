// @vitest-environment happy-dom

import { describe, expect, it, vi } from 'vitest';
import {
  getUsageTabPath,
  handleUsageTabKeyActivation,
  resolveInitialUsageTab,
  resolveUsageTabFromPath,
  shouldHandleUsageNavigation,
  stripAppBasePath,
} from '../usageNavigation';

describe('usage page navigation', () => {
  it('maps only the fixed public page paths to existing tabs', () => {
    const mappings = [
      ['overview', '/overview'],
      ['realtime', '/realtime'],
      ['analysis', '/analysis'],
      ['events', '/request-events'],
      ['credentials', '/credentials'],
      ['ai-provider', '/ai-provider'],
      ['settings', '/settings'],
    ] as const;

    for (const [tab, path] of mappings) {
      expect(getUsageTabPath(tab)).toBe(path);
      expect(resolveUsageTabFromPath(path)).toBe(tab);
    }
    expect(resolveUsageTabFromPath('/credentials/')).toBeNull();
  });

  it('rejects unknown, nested, encoded, and URL-shaped path input', () => {
    expect(resolveUsageTabFromPath('/')).toBeNull();
    expect(resolveUsageTabFromPath('/credentials/settings')).toBeNull();
    expect(resolveUsageTabFromPath('/credentials%2Fsettings')).toBeNull();
    expect(resolveUsageTabFromPath('/credentials/../settings')).toBeNull();
    expect(resolveUsageTabFromPath('/credentials\\settings')).toBeNull();
    expect(resolveUsageTabFromPath('/%2e%2e/credentials')).toBeNull();
    expect(resolveUsageTabFromPath('//example.com/credentials')).toBeNull();
    expect(resolveUsageTabFromPath('https://example.com/credentials')).toBeNull();
  });

  it('lets a valid direct route override storage while keeping root storage behavior', () => {
    expect(resolveInitialUsageTab('/cpa/credentials', '/cpa', 'analysis')).toBe('credentials');
    expect(resolveInitialUsageTab('/cpa/', '/cpa', 'analysis')).toBe('analysis');
    expect(resolveInitialUsageTab('/cpa/', '/cpa', 'credentials')).toBe('credentials');
    expect(resolveInitialUsageTab('/cpa/', '/cpa', 'unknown')).toBe('overview');
  });

  it('strips only the configured app base-path boundary', () => {
    expect(stripAppBasePath('/cpa/credentials', '/cpa')).toBe('/credentials');
    expect(stripAppBasePath('/cpa', '/cpa/')).toBe('/');
    expect(stripAppBasePath('/cpattack/credentials', '/cpa')).toBeNull();
    expect(stripAppBasePath('/credentials', '/cpa')).toBeNull();
    expect(stripAppBasePath('/credentials', undefined)).toBe('/credentials');
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
    link.href = '/credentials';
    document.body.appendChild(link);
    link.focus();

    const activate = vi.fn();
    link.addEventListener('keydown', (event) => {
      handleUsageTabKeyActivation(event, 'credentials', activate);
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
    expect(activate).toHaveBeenCalledWith('credentials');

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
