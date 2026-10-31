// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { mountLoginBackdrop } from '../loginBackdrop';

let host: HTMLDivElement;
let canvas: HTMLCanvasElement;
let hidden: boolean;
let cleanup: (() => void) | undefined;
let nextFrame: number;
let frames: Map<number, FrameRequestCallback>;
let motion: EventTarget & { matches: boolean };
const gl = {
  createProgram: vi.fn(() => ({})), createBuffer: vi.fn(() => ({})), createShader: vi.fn(() => ({})),
  deleteProgram: vi.fn(), deleteBuffer: vi.fn(), deleteShader: vi.fn(),
  shaderSource: vi.fn(), compileShader: vi.fn(), getShaderParameter: vi.fn(() => true), attachShader: vi.fn(),
  linkProgram: vi.fn(), getProgramParameter: vi.fn(() => true), useProgram: vi.fn(), bindBuffer: vi.fn(), bufferData: vi.fn(),
  getAttribLocation: vi.fn(() => 0), enableVertexAttribArray: vi.fn(), vertexAttribPointer: vi.fn(),
  getUniformLocation: vi.fn(() => ({})), viewport: vi.fn(), uniform1f: vi.fn(), uniform2f: vi.fn(), drawArrays: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
  gl.getShaderParameter.mockReturnValue(true);
  host = document.createElement('div');
  canvas = document.createElement('canvas');
  host.appendChild(canvas);
  document.body.appendChild(host);
  Object.defineProperties(host, { clientWidth: { value: 1920 }, clientHeight: { value: 1080 } });
  vi.spyOn(canvas, 'getContext').mockReturnValue(gl as unknown as WebGLRenderingContext);
  hidden = false;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  frames = new Map();
  nextFrame = 0;
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    frames.set(++nextFrame, callback);
    return nextFrame;
  });
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id));
  motion = Object.assign(new EventTarget(), { matches: false });
  vi.spyOn(window, 'matchMedia').mockReturnValue(motion as unknown as MediaQueryList);
});

afterEach(() => {
  cleanup?.();
  cleanup = undefined;
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const advanceFrame = (time: number) => {
  const pending = [...frames.values()];
  frames.clear();
  pending.forEach(callback => callback(time));
};

it('runs a bounded canvas and stops animation for hidden tabs, reduced motion and unmount', () => {
  cleanup = mountLoginBackdrop(host, canvas);
  expect(host.dataset.renderer).toBe('webgl');
  expect([canvas.width, canvas.height]).toEqual([1280, 720]);
  const initialDraws = gl.drawArrays.mock.calls.length;
  advanceFrame(100);
  advanceFrame(140);
  expect(gl.drawArrays.mock.calls.length).toBeGreaterThan(initialDraws);
  expect(frames.size).toBe(1);

  hidden = true;
  document.dispatchEvent(new Event('visibilitychange'));
  expect(host.dataset.paused).toBe('true');
  expect(frames.size).toBe(0);
  hidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  expect(frames.size).toBe(1);

  motion.matches = true;
  motion.dispatchEvent(new Event('change'));
  expect(frames.size).toBe(0);
  motion.matches = false;
  motion.dispatchEvent(new Event('change'));
  expect(frames.size).toBe(1);

  cleanup();
  cleanup = undefined;
  expect(frames.size).toBe(0);
  expect(gl.deleteProgram).toHaveBeenCalledTimes(1);
  expect(gl.deleteBuffer).toHaveBeenCalledTimes(1);
  expect(gl.deleteShader).toHaveBeenCalledTimes(2);
  const finalDraws = gl.drawArrays.mock.calls.length;
  document.dispatchEvent(new Event('visibilitychange'));
  motion.dispatchEvent(new Event('change'));
  expect(gl.drawArrays).toHaveBeenCalledTimes(finalDraws);
  expect(frames.size).toBe(0);
});

it('restores the CSS fallback on context loss and rebuilds the renderer after recovery', () => {
  cleanup = mountLoginBackdrop(host, canvas);
  const lost = new Event('webglcontextlost', { cancelable: true });
  canvas.dispatchEvent(lost);
  expect(lost.defaultPrevented).toBe(true);
  expect(host.dataset.renderer).toBeUndefined();
  expect(frames.size).toBe(0);
  canvas.dispatchEvent(new Event('webglcontextrestored'));
  expect(host.dataset.renderer).toBe('webgl');
  expect(gl.createProgram).toHaveBeenCalledTimes(2);
  expect(frames.size).toBe(1);
});

it('leaves the CSS fallback active if shader compilation fails', () => {
  gl.getShaderParameter.mockReturnValue(false);
  cleanup = mountLoginBackdrop(host, canvas);
  expect(host.dataset.renderer).toBeUndefined();
  expect(frames.size).toBe(0);
  expect(gl.deleteProgram).toHaveBeenCalledTimes(1);
  expect(gl.deleteBuffer).toHaveBeenCalledTimes(1);
  expect(gl.deleteShader).toHaveBeenCalledTimes(1);
});

it('renders a still frame without a continuous loop when reduced motion is already enabled', () => {
  motion.matches = true;
  cleanup = mountLoginBackdrop(host, canvas);
  expect(host.dataset.renderer).toBe('webgl');
  expect(gl.drawArrays).toHaveBeenCalled();
  expect(frames.size).toBe(0);
});
