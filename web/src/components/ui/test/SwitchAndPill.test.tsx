// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PillSwitcher } from '../PillSwitcher';
import { SwitchControl } from '../SwitchControl';

describe('SwitchControl and PillSwitcher primitives', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it('renders PillSwitcher options with aria-pressed and custom data attributes', async () => {
    const onChange = vi.fn();
    await act(async () => {
      root.render(
        <PillSwitcher
          ariaLabel="Window"
          value="week"
          onChange={onChange}
          options={[
            { value: 'day', label: 'Day', dataAttributes: { 'data-window': 'day' } },
            { value: 'week', label: 'Week', dataAttributes: { 'data-window': 'week' } },
          ]}
        />,
      );
    });

    const buttons = container.querySelectorAll('button');
    expect(buttons).toHaveLength(2);
    expect(buttons[0].getAttribute('aria-pressed')).toBe('false');
    expect(buttons[1].getAttribute('aria-pressed')).toBe('true');
    expect(buttons[0].getAttribute('data-window')).toBe('day');

    await act(async () => {
      buttons[0].click();
    });
    expect(onChange).toHaveBeenCalledWith('day');
  });

  it('renders SwitchControl checkbox state, label, and input attributes', async () => {
    const onChange = vi.fn();
    await act(async () => {
      root.render(
        <SwitchControl
          checked={false}
          label="Active only"
          onChange={onChange}
          inputProps={{ 'data-column-id': 'tokens' }}
        />,
      );
    });

    const input = container.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    expect(input.checked).toBe(false);
    expect(input.getAttribute('data-column-id')).toBe('tokens');

    await act(async () => {
      input.click();
    });
    expect(onChange).toHaveBeenCalledWith(true, expect.any(Object));
  });
});
