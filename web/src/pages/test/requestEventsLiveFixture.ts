import type { UsageEventsResponse } from '@/lib/types';

// 与生产协议一样先发送完整快照并保持连接；测试结束或切换筛选时由 AbortSignal 关闭。
export function requestEventsLiveSnapshot(page: UsageEventsResponse, signal: AbortSignal): Response {
  const data = {
    ...page,
    range_start: '1900-01-01T00:00:00Z',
    range_end: '2100-01-01T00:00:00Z',
    end_exclusive: false,
  };
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(`event: snapshot\ndata: ${JSON.stringify(data)}\n\n`));
      const abort = () => controller.error(new DOMException('Aborted', 'AbortError'));
      if (signal.aborted) abort();
      else signal.addEventListener('abort', abort, { once: true });
    },
  });
  return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
}
