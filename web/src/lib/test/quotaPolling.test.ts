// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { startQuotaPolling } from '../quotaPolling';
describe('cached quota polling', () => {
  it('queues visibility refresh until an aborted request settles', async () => {
    vi.useFakeTimers();
    let hidden = false;
    vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
    let finish: () => void = () => {};
    const refresh = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const stop = startQuotaPolling(refresh);
    hidden = true;
    document.dispatchEvent(new Event('visibilitychange'));
    hidden = false;
    document.dispatchEvent(new Event('visibilitychange'));
    expect(refresh).toHaveBeenCalledTimes(1);
    finish();
    await Promise.resolve();
    expect(refresh).toHaveBeenCalledTimes(2);
    stop();
    finish();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });
  it('polls every ten seconds, pauses hidden tabs, refreshes on visibility, and never overlaps', async () => {
    vi.useFakeTimers();
    let hidden = false;
    vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
    let finish: () => void = () => {};
    const refresh = vi.fn(
      (_signal: AbortSignal) =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const stop = startQuotaPolling(refresh);
    expect(refresh).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(30000);
    expect(refresh).toHaveBeenCalledTimes(1);
    finish();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(10000);
    expect(refresh).toHaveBeenCalledTimes(2);
    hidden = true;
    document.dispatchEvent(new Event('visibilitychange'));
    expect(refresh.mock.calls[1][0].aborted).toBe(true);
    finish();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(30000);
    expect(refresh).toHaveBeenCalledTimes(2);
    hidden = false;
    document.dispatchEvent(new Event('visibilitychange'));
    expect(refresh).toHaveBeenCalledTimes(3);
    stop();
    finish();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(10000);
    expect(refresh).toHaveBeenCalledTimes(3);
  });
});
