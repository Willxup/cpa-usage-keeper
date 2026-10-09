import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import { getLoginErrorForMode, LoginPage } from '../LoginPage';

vi.mock('react-i18next', async (importOriginal) => ({
  ...await importOriginal<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock('@/components/ui/LanguageSwitcher', () => ({ LanguageSwitcher: () => null }));

describe('LoginPage mode-specific errors', () => {
  it('shows only the active login mode error', () => {
    expect(getLoginErrorForMode('admin', { adminError: 'bad password', apiKeyError: 'bad api key' })).toBe('bad password');
    expect(getLoginErrorForMode('api_key', { adminError: 'bad password', apiKeyError: 'bad api key' })).toBe('bad api key');
  });

  it('does not leak API Key failures onto the admin tab or admin failures onto the API Key tab', () => {
    expect(getLoginErrorForMode('admin', { adminError: '', apiKeyError: 'bad api key' })).toBe('');
    expect(getLoginErrorForMode('api_key', { adminError: 'bad password', apiKeyError: '' })).toBe('');
  });

});

it('exposes all theme options in a labelled control', () => {
  const html = renderToStaticMarkup(<LoginPage onPasswordSubmit={vi.fn()} onAPIKeySubmit={vi.fn()} />);
  expect(html).toContain('role="tablist" aria-label="usage_stats.theme_switch"');
  for (const theme of ['light', 'dark', 'auto']) {
    expect(html).toContain(`usage_stats.theme_${theme}`);
  }
});
