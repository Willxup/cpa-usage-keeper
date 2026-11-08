export type RequestEventsSSEFrame = { event: string; data: string };

const MAX_FRAME_CHARS = 1024 * 1024;

// 解码器跨网络 chunk 保留 UTF-8；只有空行完成的帧才能交给调用方处理。
export async function readRequestEventsSSE(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: RequestEventsSSEFrame) => void,
  onBytes: () => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder('utf-8', { fatal: true });
  let buffer = '';
  let event = '';
  let data: string[] = [];
  let frameSize = 0;
  const line = (value: string) => {
    frameSize += value.length + 1;
    if (frameSize > MAX_FRAME_CHARS) throw new Error('Live event frame exceeds size limit');
    if (value === '') {
      if (data.length > 0) onFrame({ event: event || 'message', data: data.join('\n') });
      event = '';
      data = [];
      frameSize = 0;
      return;
    }
    if (value.startsWith(':')) return;
    const separator = value.indexOf(':');
    const field = separator < 0 ? value : value.slice(0, separator);
    let fieldValue = separator < 0 ? '' : value.slice(separator + 1);
    if (fieldValue.startsWith(' ')) fieldValue = fieldValue.slice(1);
    if (field === 'data') data.push(fieldValue);
    else if (field === 'event') event = fieldValue;
  };
  try {
    while (true) {
      const result = await reader.read();
      if (result.done) break;
      onBytes();
      buffer += decoder.decode(result.value, { stream: true });
      let index = 0;
      for (let i = 0; i < buffer.length; i++) {
        const char = buffer[i];
        if (char !== '\r' && char !== '\n') continue;
        if (char === '\r' && i === buffer.length - 1) break;
        line(buffer.slice(index, i));
        if (char === '\r' && buffer[i + 1] === '\n') i++;
        index = i + 1;
      }
      buffer = buffer.slice(index);
      if (buffer.length + frameSize > MAX_FRAME_CHARS) throw new Error('Live event frame exceeds size limit');
    }
    // 独立 CR 也是合法行结束符；除完整空行外的 EOF 半帧仍不能处理。
    buffer += decoder.decode();
    if (buffer.endsWith('\r')) line(buffer.slice(0, -1));
  } finally {
    reader.releaseLock();
  }
}
