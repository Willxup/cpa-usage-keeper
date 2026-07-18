import { useLayoutEffect, type RefObject } from 'react';

const SCROLL_BOUNDARY_ATTRIBUTE = 'data-scroll-boundary-contained';

const hasVerticalOverflow = (element: HTMLElement) => (
  element.scrollHeight > element.clientHeight
);

const syncScrollBoundaryAttribute = (element: HTMLElement) => {
  if (hasVerticalOverflow(element)) {
    element.setAttribute(SCROLL_BOUNDARY_ATTRIBUTE, 'true');
    return;
  }
  element.removeAttribute(SCROLL_BOUNDARY_ATTRIBUTE);
};

export function useScrollBoundaryContainment<T extends HTMLElement>(
  scrollRef: RefObject<T | null>,
  active = true,
) {
  useLayoutEffect(() => {
    if (!active) return;
    const element = scrollRef.current;
    if (!element) return;

    const update = () => syncScrollBoundaryAttribute(element);
    const resizeObserver = typeof ResizeObserver === 'undefined'
      ? null
      : new ResizeObserver(update);
    const observedChildren = new Set<Element>();
    const syncObservedChildren = () => {
      if (!resizeObserver) return;
      const currentChildren = new Set(element.children);
      for (const child of observedChildren) {
        if (currentChildren.has(child)) continue;
        resizeObserver.unobserve(child);
        observedChildren.delete(child);
      }
      for (const child of element.children) {
        if (observedChildren.has(child)) continue;
        observedChildren.add(child);
        resizeObserver.observe(child);
      }
    };

    update();
    resizeObserver?.observe(element);
    syncObservedChildren();

    const mutationObserver = typeof MutationObserver === 'undefined'
      ? null
      : new MutationObserver(() => {
        syncObservedChildren();
        update();
      });
    mutationObserver?.observe(element, resizeObserver
      ? { childList: true }
      : {
          attributes: true,
          attributeFilter: ['class', 'style'],
          characterData: true,
          childList: true,
          subtree: true,
        });
    window.addEventListener('resize', update);

    return () => {
      window.removeEventListener('resize', update);
      mutationObserver?.disconnect();
      resizeObserver?.disconnect();
      element.removeAttribute(SCROLL_BOUNDARY_ATTRIBUTE);
    };
  }, [active, scrollRef]);
}
