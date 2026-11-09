import { describe, expect, it } from 'vitest';
import { readRequestEventsSSE, type RequestEventsSSEFrame } from '../requestEventsSSE';
import { mergeRequestEvents } from '../useRequestEventsFeed';
import type { UsageEvent } from '@/lib/types';

const bytes = new TextEncoder();

function streamChunks(chunks: Uint8Array[]) {
  return new ReadableStream<Uint8Array>({ start(controller) { chunks.forEach((chunk) => controller.enqueue(chunk)); controller.close(); } });
}

describe('request events SSE reader', () => {
  it('accepts complete CRLF/multiline frames across UTF-8 chunks but never applies an EOF half-frame', async () => {
    const input = bytes.encode(': ping\r\nid: 1\r\nevent: append\r\ndata: {"model":\r\ndata: "模型"}\r\n\r\nid: 2\r\nevent: append\r\ndata: {"unfinished":');
    const frames: RequestEventsSSEFrame[] = [];
    await readRequestEventsSSE(streamChunks(Array.from(input, (value) => Uint8Array.of(value))), (frame) => frames.push(frame), () => undefined);
    expect(frames).toEqual([{ event: 'append', data: '{"model":\n"模型"}' }]);
    const crFrames: RequestEventsSSEFrame[] = [];
    await readRequestEventsSSE(streamChunks([bytes.encode('id: 3\revent: checkpoint\rdata: {}\r\r')]), (frame) => crFrames.push(frame), () => undefined);
    expect(crFrames).toEqual([{ event: 'checkpoint', data: '{}' }]);
  });

  it('bounds untrusted frames instead of buffering an unlimited stream', async () => {
    const input = bytes.encode(`data: ${'a'.repeat(1024 * 1024 + 1)}`);
    await expect(readRequestEventsSSE(streamChunks([input]), () => undefined, () => undefined)).rejects.toThrow('size limit');
  });
});

describe('request events ID and timestamp ordering', () => {
  const event = (id: string, timestamp: string, request_id = 'same') => ({ id, timestamp, request_id }) as UsageEvent;
  it('deduplicates by 64-bit ID and follows database text ordering for nanoseconds and shared request IDs', () => {
    const result = mergeRequestEvents([
      event('9007199254740993', '2026-09-21T12:00:00.123456789+08:00'),
      event('9007199254740992', '2026-09-21T12:00:00.123456788+08:00'),
    ], [
      event('9007199254740993', '2026-09-21T12:00:00.123456789+08:00'),
      event('9007199254740995', '2026-09-21T12:00:00.123456789+08:00'),
      event('9007199254740996', '2026-09-21T12:00:00+08:00'),
    ]);
    expect(result.map((item) => item.id)).toEqual(['9007199254740995', '9007199254740993', '9007199254740992', '9007199254740996']);
  });
});
