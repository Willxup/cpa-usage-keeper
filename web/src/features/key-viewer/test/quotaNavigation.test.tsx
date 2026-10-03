// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';
import i18n from '@/i18n';
import { KeyViewerShell } from '../KeyViewerShell';
it('only displays the quota navigation when enabled for the session', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  const container = document.createElement('div');
  const root = createRoot(container);
  try {
    for (const enabled of [false, true]) {
      await act(async () =>
        root.render(
          <KeyViewerShell
            activePage="overview"
            apiKey={{ display_key: 'masked', quota_enabled: enabled }}
            onNavigate={() => undefined}
            onRefresh={() => undefined}
          >
            Content
          </KeyViewerShell>,
        ),
      );
      expect(container.textContent?.includes('Provider quota')).toBe(enabled);
    }
  } finally {
    await act(async () => root.unmount());
  }
});
