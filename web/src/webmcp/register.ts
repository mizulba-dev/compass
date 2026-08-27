import type { Canvas } from '../types';
import { StaleTokenError, getCanvasForAgent } from '../api';
import { descriptions } from './descriptions';
import type { WebMCPToolResult } from './types';

const STALE_TOKEN_TEXT =
  'Write rejected: your state token is missing or stale. Call read_canvas again, then retry.';

interface RegisterOptions {
  canvasId: string;
  /** Lets registered tools push a fresh canvas into the React app immediately, without waiting for the next SSE tick. */
  onUpdate: (canvas: Canvas) => void;
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

/**
 * Registers Compass's 7 WebMCP tools on navigator.modelContext. A no-op in
 * any environment without WebMCP support (feature-detected), so it is safe
 * to call unconditionally on every page load.
 */
export function registerWebMCPTools({ canvasId, onUpdate }: RegisterOptions): void {
  const modelContext = navigator.modelContext;
  if (!modelContext) return;

  // The current readToken, tracked locally so agent tool calls never need to
  // carry it as an argument. Every tool call ends by re-reading the canvas,
  // which both refreshes this token and delivers any pending humanActions.
  let readToken = '';

  const refresh = async (): Promise<Canvas> => {
    const canvas = await getCanvasForAgent(canvasId);
    readToken = canvas.readToken;
    onUpdate(canvas);
    return canvas;
  };

  const write = async (path: string, body: Record<string, unknown>): Promise<WebMCPToolResult> => {
    const res = await writeJSON(path, { ...body, readToken });
    if (res.status === 409) {
      return err(STALE_TOKEN_TEXT);
    }
    if (!res.ok) {
      const payload = await res.json().catch(() => ({}));
      return err(typeof payload.error === 'string' ? payload.error : `write failed (${res.status})`);
    }
    return ok(await refresh());
  };

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
    execute: (args) => write(`/api/canvas/${canvasId}/goal`, args),
  });

  modelContext.registerTool({
    name: 'set_current',
    description: descriptions.set_current,
    inputSchema: {
      type: 'object',
      properties: { summary: { type: 'string' } },
      required: ['summary'],
    },
    execute: (args) => write(`/api/canvas/${canvasId}/current`, args),
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
    execute: (args) => write(`/api/canvas/${canvasId}/gaps`, args),
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
    execute: (args) => write(`/api/canvas/${canvasId}/tasks/plan`, args),
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
    execute: (args) => write(`/api/canvas/${canvasId}/tasks/update`, args),
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
    execute: (args) => write(`/api/canvas/${canvasId}/policies`, args),
  });
}

// StaleTokenError is re-exported for callers that want to distinguish it
// from other failures elsewhere in the app.
export { StaleTokenError };
