// @vitest-environment happy-dom

import React, { act, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useScrollBoundaryContainment } from '../useScrollBoundaryContainment';

type ScrollMetrics = {
  clientHeight: number;
  scrollHeight: number;
};

const resizeObservers: TestResizeObserver[] = [];
const mutationObservers: TestMutationObserver[] = [];

class TestResizeObserver implements ResizeObserver {
  private readonly observedTargets = new Set<Element>();

  constructor(private readonly callback: ResizeObserverCallback) {
    resizeObservers.push(this);
  }

  observe(target: Element) {
    this.observedTargets.add(target);
  }

  disconnect() {
    this.observedTargets.clear();
  }

  unobserve(target: Element) {
    this.observedTargets.delete(target);
  }

  isObserving(target: Element) {
    return this.observedTargets.has(target);
  }

  trigger(target: Element) {
    if (!this.observedTargets.has(target)) return;
    this.callback([{ target } as ResizeObserverEntry], this);
  }
}

class TestMutationObserver implements MutationObserver {
  private readonly observations = new Map<Node, MutationObserverInit>();

  constructor(private readonly callback: MutationCallback) {
    mutationObservers.push(this);
  }

  observe(target: Node, options: MutationObserverInit = {}) {
    this.observations.set(target, options);
  }

  disconnect() {
    this.observations.clear();
  }

  takeRecords(): MutationRecord[] {
    return [];
  }

  optionsFor(target: Node) {
    return this.observations.get(target);
  }

  trigger() {
    if (this.observations.size === 0) return;
    this.callback([], this);
  }
}

function ScrollRegion({ metrics, visible = true, childKey = 'content' }: { metrics: ScrollMetrics; visible?: boolean; childKey?: string }) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  useScrollBoundaryContainment(scrollRef, visible);

  if (!visible) return null;

  return (
    <div
      ref={(node) => {
        scrollRef.current = node;
        if (!node) return;
        Object.defineProperties(node, {
          clientHeight: { configurable: true, get: () => metrics.clientHeight },
          scrollHeight: { configurable: true, get: () => metrics.scrollHeight },
        });
      }}
    >
      <div key={childKey}>content</div>
    </div>
  );
}

describe('useScrollBoundaryContainment', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    resizeObservers.length = 0;
    mutationObservers.length = 0;
    vi.stubGlobal('ResizeObserver', TestResizeObserver);
    vi.stubGlobal('MutationObserver', TestMutationObserver);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  });

  it('contains scroll chaining only while the region has vertical overflow', () => {
    const metrics = { clientHeight: 100, scrollHeight: 240 };

    act(() => root.render(<ScrollRegion metrics={metrics} />));

    const scrollRegion = container.firstElementChild as HTMLElement;
    expect(scrollRegion?.getAttribute('data-scroll-boundary-contained')).toBe('true');
    expect(resizeObservers.some((observer) => observer.isObserving(scrollRegion))).toBe(true);

    metrics.scrollHeight = 100;
    resizeObservers.forEach((observer) => observer.trigger(scrollRegion));

    expect(scrollRegion?.hasAttribute('data-scroll-boundary-contained')).toBe(false);
  });

  it('contains a real one-pixel overflow', () => {
    const metrics = { clientHeight: 100, scrollHeight: 101 };

    act(() => root.render(<ScrollRegion metrics={metrics} />));

    expect(container.firstElementChild?.getAttribute('data-scroll-boundary-contained')).toBe('true');
  });

  it('stops observing direct children after they are removed', () => {
    const metrics = { clientHeight: 100, scrollHeight: 240 };

    act(() => root.render(<ScrollRegion metrics={metrics} childKey="first" />));
    const firstChild = container.firstElementChild?.firstElementChild as Element;

    act(() => root.render(<ScrollRegion metrics={metrics} childKey="second" />));
    mutationObservers.forEach((observer) => observer.trigger());
    const secondChild = container.firstElementChild?.firstElementChild as Element;

    expect(resizeObservers.some((observer) => observer.isObserving(secondChild))).toBe(true);
    expect(resizeObservers.some((observer) => observer.isObserving(firstChild))).toBe(false);
  });

  it('observes nested fallback mutations when ResizeObserver is unavailable', () => {
    vi.stubGlobal('ResizeObserver', undefined);
    const metrics = { clientHeight: 100, scrollHeight: 100 };

    act(() => root.render(<ScrollRegion metrics={metrics} />));
    const scrollRegion = container.firstElementChild as HTMLElement;
    const fallbackOptions = mutationObservers
      .map((observer) => observer.optionsFor(scrollRegion))
      .find(Boolean);

    expect(fallbackOptions).toMatchObject({
      attributes: true,
      characterData: true,
      childList: true,
      subtree: true,
    });

    metrics.scrollHeight = 101;
    mutationObservers.forEach((observer) => observer.trigger());

    expect(scrollRegion.getAttribute('data-scroll-boundary-contained')).toBe('true');
  });

  it('starts observing when a conditional scroll region mounts', () => {
    const metrics = { clientHeight: 100, scrollHeight: 240 };

    act(() => root.render(<ScrollRegion metrics={metrics} visible={false} />));
    expect(container.firstElementChild).toBeNull();

    act(() => root.render(<ScrollRegion metrics={metrics} visible />));
    expect(container.firstElementChild?.getAttribute('data-scroll-boundary-contained')).toBe('true');
  });
});
