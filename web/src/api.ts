import type { FogMap } from './types';

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
 * pending humanActions — only read_map (getMapForAgent) may consume those,
 * so the page can't steal an event meant for the agent's next tool call.
 */
export async function getMap(id: string): Promise<FogMap> {
  const res = await fetch(`/api/canvas/${id}?deliver=0`, { credentials: 'same-origin' });
  return (await parseOrThrow(res)) as FogMap;
}

/**
 * Consuming read: the WebMCP read_map tool only. Delivers and clears any
 * pending humanActions.
 */
export async function getMapForAgent(id: string): Promise<FogMap> {
  const res = await fetch(`/api/canvas/${id}`, { credentials: 'same-origin' });
  return (await parseOrThrow(res)) as FogMap;
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

/**
 * Places a single freely-positioned node directly on the human's canvas
 * (double-click, or the + toolbar button on a selected node). No WebMCP
 * tool calls this — add_nodes (agent) is capped at 3 per call and always
 * needs an existing parent; this one doesn't.
 */
export async function addNodeHuman(
  id: string,
  readToken: string,
  input: { text: string; x: number; y: number; parent?: string },
): Promise<{ readToken: string }> {
  return postWrite(`/api/canvas/${id}/nodes/human`, { readToken, ...input });
}

/**
 * Every node edit reserved for the human: move, delete (with its subtree),
 * toggle fog either way, star. No WebMCP tool calls this endpoint.
 */
export async function editNodeHuman(
  id: string,
  readToken: string,
  edit: { id: string; text?: string; x?: number; y?: number; fog?: boolean; star?: boolean; delete?: boolean },
): Promise<{ readToken: string }> {
  return postWrite(`/api/canvas/${id}/node/human`, { readToken, ...edit });
}

export type HumanActionType = 'add' | 'edit' | 'move' | 'delete' | 'fog' | 'unfog' | 'star' | 'discuss';

/** Records a human edit for the agent to notice on its next tool call. */
export async function recordHumanAction(id: string, type: HumanActionType, data: unknown): Promise<void> {
  await fetch(`/api/canvas/${id}/human-actions`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, data }),
  });
}
