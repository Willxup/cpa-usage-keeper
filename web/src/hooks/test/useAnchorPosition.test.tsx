// @vitest-environment happy-dom
import { act, useRef } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import { useAnchorPosition } from '../useAnchorPosition';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

it('updates changed geometry only and stops measuring after close or unmount', async () => {
  const update = vi.fn();
  function Popover({ open }: { open: boolean }) {
    const anchor = useRef<HTMLDivElement>(null);
    useAnchorPosition(open, anchor, update);
    return <div ref={anchor} />;
  }
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const bounds = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(10, 20, 100, 40));
  let unmounted = false;
  try {
    await act(async () => root.render(<Popover open />));
    await act(async () => { await new Promise(requestAnimationFrame); });
    expect(update).toHaveBeenCalledTimes(1);
    bounds.mockReturnValue(new DOMRect(10, 80, 100, 40));
    await act(async () => { await new Promise(requestAnimationFrame); });
    expect(update).toHaveBeenCalledTimes(2);
    expect(update.mock.lastCall?.[0].y).toBe(80);
    await act(async () => root.render(<Popover open={false} />));
    bounds.mockClear();
    await act(async () => { await new Promise(requestAnimationFrame); });
    expect(bounds).not.toHaveBeenCalled();
    expect(update).toHaveBeenCalledTimes(2);
    await act(async () => root.render(<Popover open />));
    expect(update).toHaveBeenCalledTimes(3);
    await act(async () => root.unmount());
    unmounted = true;
    bounds.mockClear();
    await act(async () => { await new Promise(requestAnimationFrame); });
    expect(bounds).not.toHaveBeenCalled();
  } finally {
    if (!unmounted) await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  }
});
