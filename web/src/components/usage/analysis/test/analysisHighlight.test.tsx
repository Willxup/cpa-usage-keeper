// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import { useAnalysisHighlight } from '../analysisRanking';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function HighlightProbe({ scope, resetOnScopeChange }: { scope: string; resetOnScopeChange?: boolean }) {
  const highlight = useAnalysisHighlight(scope, { resetOnScopeChange });
  return <button {...highlight.events('model')} aria-pressed={highlight.selected === 'model'} />;
}

describe('analysis highlight scope isolation', () => {
  it.each([
    { resetOnScopeChange: undefined, restored: 'true' },
    { resetOnScopeChange: true, restored: 'false' },
  ])('resets scope only when explicitly enabled: $resetOnScopeChange', async ({ resetOnScopeChange, restored }) => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    const render = (scope: string) => act(async () => root.render(<HighlightProbe scope={scope} resetOnScopeChange={resetOnScopeChange} />));
    try {
      await render('a');
      const button = container.querySelector('button')!;
      await act(async () => button.click());
      expect(button.getAttribute('aria-pressed')).toBe('true');
      await render('b');
      expect(button.getAttribute('aria-pressed')).toBe('false');
      await render('a');
      expect(button.getAttribute('aria-pressed')).toBe(restored);
    } finally {
      await act(async () => root.unmount());
      container.remove();
    }
  });
});
