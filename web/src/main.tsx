import { StrictMode, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { I18nextProvider } from 'react-i18next';
import App from './App';
import i18n from './i18n';
import './styles/reset.scss';
import './styles/variables.scss';
import './styles/themes.scss';
import './styles/layout.scss';
import './styles/components.scss';
import './styles/global.scss';
import { useThemeStore } from './stores';

function Root() {
  const initializeTheme = useThemeStore((state) => state.initializeTheme);

  useEffect(() => initializeTheme(), [initializeTheme]);

  return <App />;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nextProvider i18n={i18n}>
      <Root />
    </I18nextProvider>
  </StrictMode>
);
