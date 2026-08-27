import type { Canvas } from './types';
import { getCanvas } from './api';

const POLL_INTERVAL_MS = 5000;
/**
 * How long the connection may go without receiving any bytes — including a
 * heartbeat comment line — before we treat it as dead and fall back to
 * polling. Must stay comfortably above the server's heartbeat interval
 * (internal/api/handlers.go's sseHeartbeatInterval, 20s) so a healthy
 * connection never looks stalled.
 */
const SSE_STALL_MS = 45000;
const SSE_RETRY_DELAY_MS = 3000;

/**
 * Subscribes to live canvas updates over SSE, falling back to 5s polling
 * when the connection looks dead, and switching back to SSE once it
 * reconnects. Returns an unsubscribe function.
 *
 * This parses the SSE stream by hand (fetch + ReadableStream) instead of
 * using the native EventSource, because EventSource silently drops comment
 * lines (`: ping`) before they ever reach application code — there is no
 * event for them. Since the server's heartbeat is exactly such a comment
 * line, only manual parsing lets receiving one reset the stall timer.
 */
export function subscribeLive(id: string, onSnapshot: (canvas: Canvas) => void): () => void {
  let stopped = false;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let stallTimer: ReturnType<typeof setTimeout> | null = null;
  let abortController: AbortController | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;

  const clearStall = () => {
    if (stallTimer) clearTimeout(stallTimer);
    stallTimer = null;
  };

  const startPolling = () => {
    if (pollTimer) return;
    pollTimer = setInterval(() => {
      getCanvas(id).then(onSnapshot).catch(() => {});
    }, POLL_INTERVAL_MS);
  };

  const stopPolling = () => {
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = null;
  };

  // Any bytes at all from the connection — a real snapshot frame or a bare
  // heartbeat comment — count as proof of life and push the stall deadline
  // back out.
  const armStall = () => {
    clearStall();
    stallTimer = setTimeout(startPolling, SSE_STALL_MS);
  };

  const connect = async () => {
    if (stopped || typeof fetch === 'undefined') {
      startPolling();
      return;
    }

    abortController = new AbortController();
    try {
      const res = await fetch(`/api/canvas/${id}/events`, {
        credentials: 'same-origin',
        signal: abortController.signal,
      });
      if (!res.ok || !res.body) throw new Error(`SSE connect failed: ${res.status}`);

      stopPolling();
      armStall();

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (!stopped) {
        const { value, done } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        armStall();

        let sepIndex: number;
        while ((sepIndex = buffer.indexOf('\n\n')) !== -1) {
          const frame = buffer.slice(0, sepIndex);
          buffer = buffer.slice(sepIndex + 2);

          const dataLine = frame.split('\n').find((line) => line.startsWith('data:'));
          if (!dataLine) continue; // heartbeat comment or other non-data frame
          try {
            onSnapshot(JSON.parse(dataLine.slice(5).trim()) as Canvas);
          } catch {
            // Ignore a malformed frame; the next one resyncs the view.
          }
        }
      }
    } catch {
      // Connection failed, was aborted, or the server closed it — fall
      // through to polling and a delayed retry below.
    }

    if (stopped) return;
    startPolling();
    retryTimer = setTimeout(connect, SSE_RETRY_DELAY_MS);
  };

  connect();

  return () => {
    stopped = true;
    clearStall();
    stopPolling();
    if (retryTimer) clearTimeout(retryTimer);
    abortController?.abort();
  };
}
