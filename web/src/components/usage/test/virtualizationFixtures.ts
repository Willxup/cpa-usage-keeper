export class TestResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}

  observe(target: Element) {
    const contentRect = target.getBoundingClientRect();
    this.callback([{
      target,
      contentRect,
      borderBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      contentBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      devicePixelContentBoxSize: [],
    }], this);
  }

  disconnect() {}
  unobserve() {}
}
