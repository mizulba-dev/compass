import { createCanvas } from './api';

function canvasIdFromPath(): string | null {
  const match = window.location.pathname.match(/^\/c\/([^/]+)$/);
  return match ? match[1] : null;
}

let bootstrapPromise: Promise<string> | null = null;

/**
 * Resolves the canvas id for this page load exactly once, no matter how
 * many callers ask concurrently — a WebMCP tool call that fires before the
 * React app has even mounted, and the app's own effect after mounting must
 * land on the same id and never create two canvases.
 *
 * If the URL already names a canvas (`/c/:id`), resolves immediately.
 * Otherwise creates a new canvas and adopts its URL as the share link
 * (`history.replaceState`), so a root `/` visit and the WebMCP tools that
 * might run against it before mount both wait on this single promise.
 */
export function resolveCanvasId(): Promise<string> {
  if (bootstrapPromise) return bootstrapPromise;

  const fromPath = canvasIdFromPath();
  if (fromPath) {
    bootstrapPromise = Promise.resolve(fromPath);
    return bootstrapPromise;
  }

  bootstrapPromise = createCanvas().then(({ id }) => {
    window.history.replaceState(null, '', `/c/${id}`);
    return id;
  });
  return bootstrapPromise;
}
