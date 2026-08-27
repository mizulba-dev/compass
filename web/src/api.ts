import type { Canvas } from './types';

/** Thrown when a write is rejected for a missing or stale readToken. */
export class StaleTokenError extends Error {}

async function parseOrThrow(res: Response): Promise<unknown> {
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const message = typeof body === 'object' && body && 'error' in body ? String((body as { error: unknown }).error) : res.statusText;
    if (res.status === 409) throw new StaleTokenError(message);
    throw new Error(message);
  }
  return body;
}

export async function createCanvas(): Promise<{ id: string }> {
  const res = await fetch('/api/canvas', { method: 'POST', credentials: 'same-origin' });
  return (await parseOrThrow(res)) as { id: string };
}

/**
 * Non-consuming read, for the page's own live-state hydration (initial
 * load, poll fallback, post-write refresh). Never returns or delivers
 * pending humanActions — only read_canvas (getCanvasForAgent) may consume
 * those, so the page can't steal an event meant for the agent's next tool
 * call.
 */
export async function getCanvas(id: string): Promise<Canvas> {
  const res = await fetch(`/api/canvas/${id}?deliver=0`, { credentials: 'same-origin' });
  return (await parseOrThrow(res)) as Canvas;
}

/**
 * Consuming read: the WebMCP read_canvas tool only. Delivers and clears any
 * pending humanActions.
 */
export async function getCanvasForAgent(id: string): Promise<Canvas> {
  const res = await fetch(`/api/canvas/${id}`, { credentials: 'same-origin' });
  return (await parseOrThrow(res)) as Canvas;
}

async function postWrite(path: string, body: Record<string, unknown>): Promise<{ readToken: string }> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return (await parseOrThrow(res)) as { readToken: string };
}

/** Toggling a task's done state, made directly by the human on the page. */
export async function updateTasks(
  id: string,
  readToken: string,
  updates: Array<{ id: string; done?: boolean; text?: string }>,
): Promise<{ readToken: string }> {
  return postWrite(`/api/canvas/${id}/tasks/update`, { readToken, updates });
}

/**
 * Reordering or deleting a task, made directly by the human on the page.
 * There is no WebMCP tool for this — the agent can never call it.
 */
export async function editTasksHuman(
  id: string,
  readToken: string,
  edits: Array<{ id: string; order?: number; delete?: boolean }>,
): Promise<{ readToken: string }> {
  return postWrite(`/api/canvas/${id}/tasks/human`, { readToken, edits });
}

export type HumanActionType = 'task.toggle' | 'task.reorder' | 'task.delete';

/** Records a human edit for the agent to notice on its next tool call. */
export async function recordHumanAction(id: string, type: HumanActionType, data: unknown): Promise<void> {
  await fetch(`/api/canvas/${id}/human-actions`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, data }),
  });
}
