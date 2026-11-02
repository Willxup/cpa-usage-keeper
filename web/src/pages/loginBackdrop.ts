// 与主页相同的噪声光带运动，直接绘制全屏四边形，避免为装饰层引入 3D 场景依赖。
const vertexSource = `
  attribute vec2 position;
  varying vec2 uv;
  void main() {
    uv = position * 0.5 + 0.5;
    gl_Position = vec4(position, 0.0, 1.0);
  }
`;

const fragmentSource = `
  #ifdef GL_FRAGMENT_PRECISION_HIGH
    precision highp float;
  #else
    precision mediump float;
  #endif
  varying vec2 uv;
  uniform float time;
  uniform float aspect;
  uniform float dark;
  uniform vec2 pointer;

  float hash(vec2 p) {
    p = fract(p * vec2(123.34, 456.21));
    p += dot(p, p + 45.32);
    return fract(p.x * p.y);
  }

  float noise(vec2 p) {
    vec2 i = floor(p);
    vec2 f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    return mix(mix(hash(i), hash(i + vec2(1.0, 0.0)), f.x),
      mix(hash(i + vec2(0.0, 1.0)), hash(i + 1.0), f.x), f.y);
  }

  float fbm(vec2 p) {
    float value = 0.0;
    float amplitude = 0.5;
    for (int i = 0; i < 4; i++) {
      value += amplitude * noise(p);
      p = p * 2.03 + vec2(17.1, 9.2);
      amplitude *= 0.5;
    }
    return value;
  }

  float pool(vec2 p, vec2 center) {
    vec2 distance = p - center;
    return exp(-dot(distance, distance) * 10.0);
  }

  void main() {
    vec2 p = uv - 0.5;
    p.x *= aspect;
    p += pointer * 0.06;
    float t = time * 0.18;
    float n = fbm(p * 2.0 + vec2(t, -t * 0.55));
    float ribbonA = p.y - 0.22 * sin(p.x * 2.2 + t * 3.0) - (n - 0.5) * 0.58;
    float ribbonB = p.y + 0.30 * sin(p.x * 1.45 - t * 2.1) + 0.24 - (n - 0.5) * 0.38;
    float ribbonC = p.y - 0.16 * cos(p.x * 3.0 + t * 1.7) - 0.30 - (n - 0.5) * 0.30;
    float energy = exp(-abs(ribbonA) * 5.1)
      + exp(-abs(ribbonB) * 7.0) * 0.65
      + exp(-abs(ribbonC) * 8.8) * 0.42;

    // 保持蓝青在左、绿黄在上、珊瑚玫红在右下，颜色边界随光带变形。
    vec2 colorPosition = uv + vec2((n - 0.5) * 0.18, sin(t + p.x) * 0.06);
    float blue = pool(colorPosition, vec2(0.12, 0.50));
    float cyan = pool(colorPosition, vec2(0.32, 0.65));
    float green = pool(colorPosition, vec2(0.50, 0.95));
    float yellow = pool(colorPosition, vec2(0.78, 0.75));
    float coral = pool(colorPosition, vec2(0.92, 0.40));
    float rose = pool(colorPosition, vec2(0.60, 0.10));
    vec3 color = (blue * vec3(37.0, 99.0, 235.0)
      + cyan * vec3(6.0, 182.0, 212.0)
      + green * vec3(16.0, 185.0, 129.0)
      + yellow * vec3(251.0, 191.0, 36.0)
      + coral * vec3(251.0, 90.0, 90.0)
      + rose * vec3(219.0, 39.0, 119.0))
      / (255.0 * (blue + cyan + green + yellow + coral + rose));
    float edge = 1.0 - smoothstep(0.28, 0.85, length((uv - 0.5) * vec2(0.9, 1.1)));
    float alpha = edge * (0.08 + energy * mix(0.30, 0.52, dark));
    gl_FragColor = vec4(color, min(alpha, 0.78));
  }
`;

function createRenderer(canvas: HTMLCanvasElement) {
  const gl = canvas.getContext('webgl', { alpha: true, antialias: false, depth: false, premultipliedAlpha: false, powerPreference: 'low-power' });
  if (!gl) return null;
  const shaders: WebGLShader[] = [];
  const program = gl.createProgram();
  const buffer = gl.createBuffer();
  const dispose = () => {
    gl.deleteBuffer(buffer);
    gl.deleteProgram(program);
    shaders.forEach(shader => gl.deleteShader(shader));
  };
  try {
    if (!program || !buffer) throw new Error('Background resources unavailable');
    for (const [type, source] of [[gl.VERTEX_SHADER, vertexSource], [gl.FRAGMENT_SHADER, fragmentSource]] as const) {
      const shader = gl.createShader(type);
      if (!shader) throw new Error('Background shader unavailable');
      shaders.push(shader);
      gl.shaderSource(shader, source);
      gl.compileShader(shader);
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error('Background shader compilation failed');
      gl.attachShader(program, shader);
    }
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error('Background shader linking failed');
    gl.useProgram(program);
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
    const position = gl.getAttribLocation(program, 'position');
    gl.enableVertexAttribArray(position);
    gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
    const uniforms = Object.fromEntries(['time', 'aspect', 'dark', 'pointer'].map(name => [name, gl.getUniformLocation(program, name)]));
    return {
      render(seconds: number, width: number, height: number, pointer: readonly number[]) {
        // 柔和光带不需要按屏幕 DPR 放大；限制像素数和帧率，兼顾手机功耗。
        const scale = Math.min(1, 1280 / width, 900 / height);
        const bufferWidth = Math.max(1, Math.round(width * scale));
        const bufferHeight = Math.max(1, Math.round(height * scale));
        if (canvas.width !== bufferWidth || canvas.height !== bufferHeight) {
          canvas.width = bufferWidth;
          canvas.height = bufferHeight;
          gl.viewport(0, 0, bufferWidth, bufferHeight);
        }
        gl.uniform1f(uniforms.time, seconds);
        gl.uniform1f(uniforms.aspect, width / height);
        gl.uniform1f(uniforms.dark, document.documentElement.dataset.theme === 'dark' ? 1 : 0);
        gl.uniform2f(uniforms.pointer, pointer[0], pointer[1]);
        gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      },
      dispose,
    };
  } catch {
    dispose();
    return null;
  }
}

export function mountLoginBackdrop(host: HTMLDivElement, canvas: HTMLCanvasElement) {
  let renderer: ReturnType<typeof createRenderer> = null;
  try { renderer = createRenderer(canvas); } catch { /* WebGL 被禁用时继续使用 CSS 背景。 */ }
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
  let frame = 0;
  let previousTime = 0;
  let seconds = 1.6;
  let width = Math.max(host.clientWidth, 1);
  let height = Math.max(host.clientHeight, 1);
  const pointer = [0, 0];
  const targetPointer = [0, 0];
  const render = () => renderer?.render(seconds, width, height, pointer);
  const animate = (now: number) => {
    if (now - previousTime >= 1000 / 30) {
      seconds += previousTime ? Math.min((now - previousTime) / 1000, 0.1) : 0;
      previousTime = now;
      pointer[0] += (targetPointer[0] - pointer[0]) * 0.07;
      pointer[1] += (targetPointer[1] - pointer[1]) * 0.07;
      render();
    }
    frame = requestAnimationFrame(animate);
  };
  const syncAnimation = () => {
    cancelAnimationFrame(frame);
    host.dataset.paused = String(document.hidden);
    previousTime = 0;
    if (!renderer || document.hidden) return;
    render();
    if (!reducedMotion.matches) frame = requestAnimationFrame(animate);
  };
  const resize = () => {
    width = Math.max(host.clientWidth, 1);
    height = Math.max(host.clientHeight, 1);
    render();
  };
  const onPointerMove = (event: PointerEvent) => {
    if (reducedMotion.matches) return;
    targetPointer[0] = event.clientX / window.innerWidth * 2 - 1;
    targetPointer[1] = 1 - event.clientY / window.innerHeight * 2;
  };
  // GPU 上下文丢失时立即恢复 CSS 动画；恢复后重建资源，不干扰登录表单。
  const onContextLost = (event: Event) => {
    event.preventDefault();
    cancelAnimationFrame(frame);
    renderer?.dispose();
    renderer = null;
    delete host.dataset.renderer;
  };
  const onContextRestored = () => {
    try { renderer = createRenderer(canvas); } catch { renderer = null; }
    if (renderer) host.dataset.renderer = 'webgl';
    syncAnimation();
  };
  const resizeObserver = new ResizeObserver(resize);
  const themeObserver = new MutationObserver(render);
  resizeObserver.observe(host);
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  canvas.addEventListener('webglcontextlost', onContextLost);
  canvas.addEventListener('webglcontextrestored', onContextRestored);
  document.addEventListener('visibilitychange', syncAnimation);
  reducedMotion.addEventListener('change', syncAnimation);
  window.addEventListener('pointermove', onPointerMove, { passive: true });
  if (renderer) host.dataset.renderer = 'webgl';
  syncAnimation();

  return () => {
    cancelAnimationFrame(frame);
    resizeObserver.disconnect();
    themeObserver.disconnect();
    document.removeEventListener('visibilitychange', syncAnimation);
    reducedMotion.removeEventListener('change', syncAnimation);
    window.removeEventListener('pointermove', onPointerMove);
    canvas.removeEventListener('webglcontextlost', onContextLost);
    canvas.removeEventListener('webglcontextrestored', onContextRestored);
    renderer?.dispose();
    delete host.dataset.renderer;
  };
}
