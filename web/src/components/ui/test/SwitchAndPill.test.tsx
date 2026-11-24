// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PillSwitcher } from '../PillSwitcher';
import { SwitchControl } from '../SwitchControl';
import switchStyles from '../SwitchControl.module.scss';

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

  it('toggles from the pill label unless disabled', async () => {
    const props = { label: 'Active only', checked: false, onChange: vi.fn() };
    await act(async () => root.render(<SwitchControl variant="pill" {...props} />));

    const label = container.querySelector('label')!;
    expect(label.classList.contains(switchStyles.pill)).toBe(true);
    await act(async () => label.click());
    expect(props.onChange).toHaveBeenCalledWith(true, expect.any(Object));

    await act(async () => root.render(<SwitchControl variant="pill" {...props} disabled />));
    await act(async () => label.click());
    expect(props.onChange).toHaveBeenCalledTimes(1);
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
