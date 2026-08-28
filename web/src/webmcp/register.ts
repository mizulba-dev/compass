import type { FogMap } from '../types';
import { StaleTokenError, getMapForAgent } from '../api';
import { resolveCanvasId } from '../canvasBootstrap';
import { descriptions } from './descriptions';
import type { ModelContext, WebMCPToolResult } from './types';

const STALE_TOKEN_TEXT =
  'Write rejected: your state token is missing or stale. Call read_map again, then retry.';

// The current readToken, tracked at module scope so agent tool calls never
// need to carry it as an argument. Every tool call ends by re-reading the
// map, which both refreshes this token and delivers any pending
// humanActions. Module scope (rather than a closure created per
// registerWebMCPTools() call) matches registration itself: both happen
// exactly once per page load, before the React app exists.
let readToken = '';

// The React app connects this after it mounts (see App.tsx), so a tool call
// that completes before mount — or after unmount — just has no UI to push
// into. It still succeeds; the map state itself is unaffected, and the
// next SSE tick or page read reconciles the view.
let updateListener: ((map: FogMap) => void) | null = null;

/** Connects (or clears, with null) the listener that tool calls push fresh map snapshots into. */
export function setMapUpdateListener(listener: ((map: FogMap) => void) | null): void {
  updateListener = listener;
}

export type WebMCPStatus = { state: 'waiting' } | { state: 'registered'; count: number } | { state: 'unsupported' };

let status: WebMCPStatus = { state: 'waiting' };
let statusListener: ((status: WebMCPStatus) => void) | null = null;

function setStatus(next: WebMCPStatus): void {
  status = next;
  statusListener?.(next);
}

/**
 * Connects (or clears, with null) the listener that receives WebMCP
 * registration status transitions (waiting → registered(n) | unsupported).
 * Immediately replays the current status to a newly connected listener, so
 * a UI that mounts after the transition already happened doesn't miss it.
 */
export function setWebMCPStatusListener(listener: ((status: WebMCPStatus) => void) | null): void {
  statusListener = listener;
  listener?.(status);
}

function ok(map: FogMap): WebMCPToolResult {
  return { content: [{ type: 'text', text: JSON.stringify(map) }] };
}

function err(message: string): WebMCPToolResult {
  return { content: [{ type: 'text', text: message }], isError: true };
}

async function writeJSON(path: string, body: Record<string, unknown>): Promise<Response> {
  return fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

async function refresh(): Promise<FogMap> {
  const id = await resolveCanvasId();
  const map = await getMapForAgent(id);
  readToken = map.readToken;
  updateListener?.(map);
  return map;
}

async function write(pathSuffix: string, body: Record<string, unknown>): Promise<WebMCPToolResult> {
  const id = await resolveCanvasId();
  const res = await writeJSON(`/api/canvas/${id}${pathSuffix}`, { ...body, readToken });
  if (res.status === 409) {
    return err(STALE_TOKEN_TEXT);
  }
  if (!res.ok) {
    const payload = await res.json().catch(() => ({}));
    return err(typeof payload.error === 'string' ? payload.error : `write failed (${res.status})`);
  }
  return ok(await refresh());
}

const TOOL_COUNT = 6;
const POLL_INTERVAL_MS = 250;
const POLL_TIMEOUT_MS = 20000;

let registered = false;

/**
 * Registers Compass's 6 WebMCP tools (read_map / add_nodes / update_node /
 * remove_node / arrange_nodes / harvest) on navigator.modelContext and document.modelContext, tolerating
 * a host (e.g. ChatGPT's in-app browser) that injects modelContext onto the
 * page asynchronously, after this module already ran. If present at call
 * time, tools register immediately and synchronously, with no network or
 * React wait. If absent, this polls every 250ms for up to 20s and also
 * retries on the DOMContentLoaded and load events (belt-and-suspenders for
 * a host whose injection happens to line up with one of those instead of a
 * fixed delay), registering exactly once — an idempotency guard makes every
 * later trigger, and a poll tick racing an event, a no-op once registration
 * has actually happened. After 20s with no modelContext, gives up and
 * reports 'unsupported'.
 *
 * The canvas id itself is resolved lazily, inside each tool's execute(),
 * via resolveCanvasId() — never at registration time.
 */
export function registerWebMCPTools(): void {
  if (tryRegisterOnce()) return;

  const deadline = Date.now() + POLL_TIMEOUT_MS;
  const poll = () => {
    if (tryRegisterOnce()) return;
    if (Date.now() >= deadline) {
      setStatus({ state: 'unsupported' });
      return;
    }
    setTimeout(poll, POLL_INTERVAL_MS);
  };
  setTimeout(poll, POLL_INTERVAL_MS);

  const retryOnEvent = () => {
    tryRegisterOnce();
  };
  document.addEventListener('DOMContentLoaded', retryOnEvent, { once: true });
  window.addEventListener('load', retryOnEvent, { once: true });
}

/**
 * Finds every distinct WebMCP host present right now. Real Chrome exposes
 * `navigator.modelContext` and `document.modelContext` as the same object
 * (verified), but ChatGPT's error message ("tools bound to the current
 * document") and the W3C explainer both reference `document.modelContext`
 * specifically — so a host that only injects the document-scoped one is
 * plausible. Checking both and deduping by identity means we register on
 * whichever exists without double-registering if a host aliases them.
 */
function findModelContextHosts(): ModelContext[] {
  const hosts: ModelContext[] = [];
  const fromNavigator = navigator.modelContext;
  const fromDocument = document.modelContext;
  if (fromNavigator) hosts.push(fromNavigator);
  if (fromDocument && fromDocument !== fromNavigator) hosts.push(fromDocument);
  return hosts;
}

/** Registers all 6 tools on every distinct WebMCP host present, if this hasn't already run. Returns whether tools are registered (either just now, or already). */
function tryRegisterOnce(): boolean {
  if (registered) return true;
  const hosts = findModelContextHosts();
  if (hosts.length === 0) return false;
  registered = true;

  for (const modelContext of hosts) {
    registerToolsOn(modelContext);
  }

  setStatus({ state: 'registered', count: TOOL_COUNT });
  return true;
}

function registerToolsOn(modelContext: ModelContext): void {
  modelContext.registerTool({
    name: 'read_map',
    description: descriptions.read_map,
    inputSchema: { type: 'object', properties: {}, additionalProperties: false },
    async execute() {
      try {
        return ok(await refresh());
      } catch (e) {
        return err(e instanceof Error ? e.message : 'read_map failed');
      }
    },
  });

  modelContext.registerTool({
    name: 'add_nodes',
    description: descriptions.add_nodes,
    inputSchema: {
      type: 'object',
      properties: {
        parent: { type: 'string', description: 'id of an existing node to branch off of' },
        nodes: {
          type: 'array',
          maxItems: 3,
          items: {
            type: 'object',
            properties: {
              text: { type: 'string' },
              kind: { type: 'string', enum: ['normal', 'question', 'task'] },
            },
            required: ['text'],
            additionalProperties: false,
          },
        },
      },
      required: ['parent', 'nodes'],
      additionalProperties: false,
    },
    execute: (args) => write('/nodes', args),
  });

  modelContext.registerTool({
    name: 'update_node',
    description: descriptions.update_node,
    inputSchema: {
      type: 'object',
      properties: {
        id: { type: 'string' },
        text: { type: 'string' },
        unfog: { type: 'boolean', description: 'set true to clear this node\'s fog; there is no way to set fog on through this tool' },
        done: { type: 'boolean', description: 'mark a task-kind node done (true) or reopen it (false)' },
        kind: { type: 'string', enum: ['normal', 'question', 'task'], description: 'convert this node to a different kind' },
      },
      required: ['id'],
      additionalProperties: false,
    },
    execute: (args) => write('/node', args),
  });

  modelContext.registerTool({
    name: 'remove_node',
    description: descriptions.remove_node,
    inputSchema: {
      type: 'object',
      properties: {
        id: { type: 'string' },
      },
      required: ['id'],
      additionalProperties: false,
    },
    execute: (args) => write('/node/remove', args),
  });

  modelContext.registerTool({
    name: 'arrange_nodes',
    description: descriptions.arrange_nodes,
    inputSchema: {
      type: 'object',
      properties: {
        moves: {
          type: 'array',
          items: {
            type: 'object',
            properties: {
              id: { type: 'string' },
              x: { type: 'number' },
              y: { type: 'number' },
            },
            required: ['id', 'x', 'y'],
            additionalProperties: false,
          },
        },
      },
      required: ['moves'],
      additionalProperties: false,
    },
    execute: (args) => write('/nodes/arrange', args),
  });

  modelContext.registerTool({
    name: 'harvest',
    description: descriptions.harvest,
    inputSchema: {
      type: 'object',
      properties: {
        goal: { type: 'string' },
        premises: { type: 'array', items: { type: 'string' } },
        tasks: { type: 'array', items: { type: 'string' } },
      },
      required: ['goal'],
      additionalProperties: false,
    },
    execute: (args) => write('/harvest', args),
  });
}

// StaleTokenError is re-exported for callers that want to distinguish it
// from other failures elsewhere in the app.
export { StaleTokenError };
