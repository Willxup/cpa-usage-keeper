import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const modalSource = readFileSync(new URL('./Modal.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const componentsStyles = readFileSync(new URL('../../styles/components.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('Modal scroll lock', () => {
  it('does not mutate body layout while the modal is open', () => {
    expect(modalSource).not.toContain('body.style.position');
    expect(modalSource).not.toContain('body.style.top');
    expect(modalSource).not.toContain('body.style.width');
    expect(modalSource).not.toContain('body.style.overflow');
    expect(modalSource).not.toContain('onWheel={');
    expect(modalSource).not.toContain('onTouchMove={');
    expect(modalSource).toContain("target.closest('.modal-body')");
    expect(modalSource).toContain("overlay.addEventListener('wheel', blockOverlayWheel, { passive: false });");
    expect(modalSource).toContain("overlay.addEventListener('touchmove', blockOverlayTouchMove, { passive: false });");
    expect(modalSource).toContain("contentEl.scrollTo({ top: contentScrollTop, left: 0, behavior: 'auto' });");
    expect(modalSource).toContain('window.scrollTo({ top: scrollY, left: 0, behavior: \'auto\' });');
  });

  it('closes when clicking the overlay outside the modal panel', () => {
    expect(modalSource).toContain('handleOverlayMouseDown');
    expect(modalSource).toContain('event.target !== event.currentTarget');
    expect(modalSource).toContain('onMouseDown={handleOverlayMouseDown}');
  });

  it('notifies the parent close state before waiting for the close animation', () => {
    expect(modalSource).toMatch(/if \(notifyParent\) \{\n\s+onClose\(\);\n\s+\}\n\s+closeTimerRef\.current = window\.setTimeout/);
    expect(modalSource).not.toMatch(/window\.setTimeout\(\(\) => \{[\s\S]*?if \(notifyParent\) \{\n\s+onClose\(\);/);
  });

  it('keeps the last rendered presentation while the close animation runs', () => {
    expect(modalSource).toContain('interface ModalRenderSnapshot');
    expect(modalSource).toContain('const renderSnapshotRef = useRef<ModalRenderSnapshot>');
    expect(modalSource).toContain('const useClosingSnapshot = !open && isVisible;');
    expect(modalSource).toContain('const renderSnapshot = useClosingSnapshot');
    expect(modalSource).toContain("style={{ width: renderSnapshot.width, maxWidth: '100%' }}");
    expect(modalSource).toContain('renderSnapshot.children');
  });

  it('disables modal interactions while the close animation runs', () => {
    expect(componentsStyles).toMatch(/\.modal-overlay-closing[\s\S]*?\.modal[\s\S]*?pointer-events:\s*none;/);
  });
});
