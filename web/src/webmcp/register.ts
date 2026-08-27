import type { Canvas } from '../types';
import { StaleTokenError, getCanvasForAgent } from '../api';
import { resolveCanvasId } from '../canvasBootstrap';
import { descriptions } from './descriptions';
import type { WebMCPToolResult } from './types';

const STALE_TOKEN_TEXT =
  'Write rejected: your state token is missing or stale. Call read_canvas again, then retry.';

// The current readToken, tracked at module scope so agent tool calls never
// need to carry it as an argument. Every tool call ends by re-reading the
// canvas, which both refreshes this token and delivers any pending
// humanActions. Module scope (rather than a closure created per
// registerWebMCPTools() call) matches registration itself: both happen
// exactly once per page load, before the React app exists.
let readToken = '';

// The React app connects this after it mounts (see App.tsx), so a tool call
// that completes before mount — or after unmount — just has no UI to push
// into. It still succeeds; the canvas state itself is unaffected, and the
// next SSE tick or page read reconciles the view.
let updateListener: ((canvas: Canvas) => void) | null = null;

/** Connects (or clears, with null) the listener that tool calls push fresh canvas snapshots into. */
export function setCanvasUpdateListener(listener: ((canvas: Canvas) => void) | null): void {
  updateListener = listener;
}

function ok(canvas: Canvas): WebMCPToolResult {
  return { content: [{ type: 'text', text: JSON.stringify(canvas) }] };
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

async function refresh(): Promise<Canvas> {
  const id = await resolveCanvasId();
  const canvas = await getCanvasForAgent(id);
  readToken = canvas.readToken;
  updateListener?.(canvas);
  return canvas;
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

/**
 * Registers Compass's 7 WebMCP tools on navigator.modelContext. Call this
 * once, synchronously, before the React app mounts (see main.tsx) — it must
 * not wait on any network I/O or on React, so the tools are visible to an
 * agent inspecting the document immediately after load. The canvas id
 * itself is resolved lazily, inside each tool's execute(), via
 * resolveCanvasId() — never at registration time.
 *
 * A no-op in any environment without WebMCP support (feature-detected), so
 * it is safe to call unconditionally on every page load.
 */
export function registerWebMCPTools(): void {
  const modelContext = navigator.modelContext;
  if (!modelContext) return;

  modelContext.registerTool({
    name: 'read_canvas',
    description: descriptions.read_canvas,
    inputSchema: { type: 'object', properties: {} },
    async execute() {
      try {
        return ok(await refresh());
      } catch (e) {
        return err(e instanceof Error ? e.message : 'read_canvas failed');
      }
    },
  });

  modelContext.registerTool({
    name: 'set_goal',
    description: descriptions.set_goal,
    inputSchema: {
      type: 'object',
      properties: {
        title: { type: 'string' },
        deadline: { type: 'string' },
        why: { type: 'string' },
      },
      required: ['title'],
    },
    execute: (args) => write('/goal', args),
  });

  modelContext.registerTool({
    name: 'set_current',
    description: descriptions.set_current,
    inputSchema: {
      type: 'object',
      properties: { summary: { type: 'string' } },
      required: ['summary'],
    },
    execute: (args) => write('/current', args),
  });

  modelContext.registerTool({
    name: 'upsert_gaps',
    description: descriptions.upsert_gaps,
    inputSchema: {
      type: 'object',
      properties: {
        add: { type: 'array', items: { type: 'string' } },
        resolve: { type: 'array', items: { type: 'string' } },
      },
    },
    execute: (args) => write('/gaps', args),
  });

  modelContext.registerTool({
    name: 'plan_tasks',
    description: descriptions.plan_tasks,
    inputSchema: {
      type: 'object',
      properties: {
        tasks: {
          type: 'array',
          items: {
            type: 'object',
            properties: {
              id: { type: 'string' },
              text: { type: 'string' },
              day: { type: 'string' },
            },
            required: ['text'],
          },
        },
      },
      required: ['tasks'],
    },
    execute: (args) => write('/tasks/plan', args),
  });

  modelContext.registerTool({
    name: 'update_tasks',
    description: descriptions.update_tasks,
    inputSchema: {
      type: 'object',
      properties: {
        updates: {
          type: 'array',
          items: {
            type: 'object',
            properties: {
              id: { type: 'string' },
              text: { type: 'string' },
              done: { type: 'boolean' },
            },
            required: ['id'],
            additionalProperties: false,
          },
        },
      },
      required: ['updates'],
      additionalProperties: false,
    },
    execute: (args) => write('/tasks/update', args),
  });

  modelContext.registerTool({
    name: 'add_policy',
    description: descriptions.add_policy,
    inputSchema: {
      type: 'object',
      properties: {
        text: { type: 'string' },
        derivedFrom: { type: 'string' },
      },
      required: ['text', 'derivedFrom'],
    },
    execute: (args) => write('/policies', args),
  });
}

// StaleTokenError is re-exported for callers that want to distinguish it
// from other failures elsewhere in the app.
export { StaleTokenError };
