# WebMCP Demo — think with your own agent on a live map

**A blank freeform canvas where you and your AI grow a mind map together, one branch at a
time — and "I don't know" (🌫 fog) is a first-class thing to place, not a gap to hide.**

You drop a thing you're trying to figure out onto a blank canvas. Your agent reads it and
branches out a few next steps and questions. Anything you mark as fog — genuinely don't
know — the agent treats as its highest priority to clear, ahead of growing new healthy
branches. You drag, delete, and place nodes freely; the agent never moves or removes anything
you've placed. Once the map has enough shape, the agent folds it into a harvest: one goal, the
premises you established, and the next concrete tasks.

This demo is not a to-do app and not a chat log. The map is the product — a structured, spatial
memory of the parts of a problem you and the agent have already worked through, that a plain
conversation would otherwise lose to scrollback the moment the session ends.

## Why WebMCP

The canvas is the point, not an implementation detail. The app exposes its tools entirely
through [WebMCP](https://github.com/webmachinelearning/webmcp) (`navigator.modelContext`) on
the page itself, rather than a backend MCP server:

- The loop this app is built around — *agent asks → human answers → the page changes → human
  edits by hand → the agent's next suggestion reacts to that edit* — only works because agent
  and human are looking at, and acting on, the same live page in the same session.
- The human's edits (deleting a task, reordering the plan, checking something off) are not
  passive approvals; they carry judgment the agent doesn't otherwise have. The app surfaces
  them straight into the agent's next tool result as `humanActions`.
- Bring your own agent: because the tools live on the page, whatever assistant you already
  use — with its own memory of you — can pick up a canvas it has never seen before and be
  useful immediately.

## Architecture

```
                ┌──────────────────────────────┐
                │  Canvas store (source of truth)│
                │  Go API + Postgres             │
                └──────────────┬────────────────┘
                               │
                ┌──────────────┴────────────────┐
                │  WebMCP surface                 │
                │  the page exposes the tools      │
                └──────────────┬────────────────┘
                               │
                 ChatGPT's in-app browser /
                 Chrome with the WebMCP flag
                 = where the conversation, planning, and judgment happen
```

- A single Go binary serves the API, the SSE event stream, and the built React SPA from one
  origin — WebMCP tool calls use `credentials: "same-origin"`, so same-origin keeps auth
  simple with none of the machinery a cross-origin setup would need.
- The map (title, nodes with position/parent/fog/star, and an optional harvest) lives in one
  Postgres row per map, read and written as a JSON blob. There's no separate backend MCP
  endpoint — the WebMCP tools are thin wrappers around the same public HTTP API a human's
  browser uses.
- **Every write requires a fresh `readToken`**, returned by `read_map` (`GET
  /api/canvas/:id`) and rotated on every successful write. A missing or stale token gets a
  `409` whose body tells the caller to call `read_map` again — this both enforces "always
  read before you act" and doubles as optimistic concurrency control.
- **Agent capability boundaries live in the server's decode surface, not just the tool
  schema.** `add_nodes` can only branch off an existing node and is capped at 3 per call;
  `update_node` can only edit text and clear fog. Moving, deleting, (re-)fogging, and starring
  a node go through separate human-only endpoints that no WebMCP tool ever calls — so even a
  model that ignores its own tool description has no code path to move or remove a node.
- Human edits made directly on the canvas (adding, moving, deleting, fogging/unfogging,
  starring a node) are queued server-side and delivered — once — inside the response of
  whichever tool the agent calls next. There is no way to push a notification into a paused
  chat, so this rides along with the next tool call instead. Rapid-fire drags collapse to the
  node's latest position before delivery.

## Project layout

```
cmd/webmcp-demo/main.go          HTTP server: API + SSE + static SPA, one origin
internal/store/              Postgres persistence, readToken guard, human-action delivery
internal/api/                HTTP handlers implementing the map API contract
web/                         React + TypeScript SPA (Vite)
  src/components/FogMapCanvas.tsx  the freeform canvas: pan/zoom/drag/select/edit
  src/webmcp/register.ts     registerTool() wiring for all 4 tools
  src/webmcp/descriptions.ts tool descriptions — the main agent-behavior tuning surface
  src/live.ts                SSE subscription with 5s-poll fallback
```

## Running locally

Requires Go 1.24+, Node 20+, and Docker.

```bash
cp .env.example .env            # adjust if you like; defaults match docker-compose.yml
docker compose up -d db         # Postgres on localhost:55432
cd web && npm ci && npm run build && cd ..
DATABASE_URL=postgres://compass:compass@localhost:55432/compass?sslmode=disable \
  PORT=8080 STATIC_DIR=web/dist go run ./cmd/webmcp-demo
```

Open `http://localhost:8080` — it creates a new map and redirects to its share URL
(`/c/<id>`), which is also the WebMCP entry point for an agent.

### Tests

```bash
go vet ./...
go test ./...   # needs docker compose's db running (internal/testutil connects to it)
```

## Verifying WebMCP end to end

Two browsers currently support calling page-registered tools:

1. ChatGPT's in-app browser (primary target).
2. Desktop Chrome with `chrome://flags/#enable-webmcp-testing` enabled (fallback / reviewer
   path).

Open a map URL in either, and ask the connected agent to help you think through
something — it should discover and call `read_map`, `add_nodes`, and the rest of the tools
declared in `web/src/webmcp/register.ts`.

## License

[MIT](./LICENSE)
