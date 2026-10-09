// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { resolve } from 'node:path';
import { compile } from 'sass';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { Input } from '../Input';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const componentsCSS = compile(resolve(process.cwd(), 'src/styles/components.scss')).css;

describe('Input', () => {
  let container: HTMLDivElement;
  let root: Root;
  let stylesheet: HTMLStyleElement;

  beforeEach(() => {
    document.documentElement.style.setProperty('--failure-badge-text', '#8a3a30');
    stylesheet = document.createElement('style');
    stylesheet.textContent = componentsCSS;
    document.head.appendChild(stylesheet);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    stylesheet.remove();
    document.documentElement.style.removeProperty('--failure-badge-text');
  });

  it('renders field validation errors with the compact shared form typography', async () => {
    await act(async () => {
      root.render(<Input label="Display name" error="Display name is required" />);
    });

    const error = container.querySelector<HTMLElement>('.error-box');
    expect(error).not.toBeNull();
    expect(getComputedStyle(error!).fontSize).toBe('12px');
    expect(getComputedStyle(error!).lineHeight).toBe('1.45');
  });

  it('makes disabled and invalid field states visually distinct', async () => {
    await act(async () => {
      root.render(<Input label="Display name" error="Display name is required" disabled />);
    });

    const input = container.querySelector<HTMLInputElement>('input')!;
    expect(input.disabled).toBe(true);
    expect(input.getAttribute('aria-invalid')).toBe('true');
    expect(getComputedStyle(input).cursor).toBe('not-allowed');
    expect(getComputedStyle(input).opacity).toBe('0.6');
    expect(getComputedStyle(input).borderColor).toBe('#8a3a30');
  });
});
