# Compass

**The output of a good conversation with your AI, as a living page — not a chat log.**

Everyone already talks to an AI about their goals: where they stand, what's missing, what to
do next. The value of that conversation usually evaporates into scrollback, or gets manually
copied into a to-do app. Compass gives the conversation a shared canvas instead. As you talk,
a page grows — **Goal → Current situation → Gaps → Plan** — and your agent reads that same
page back the next time you open it, picking up exactly where you left off.

Compass is not a to-do app. The plan is a byproduct of the conversation; the conversation is
the product. The human brings judgment — priorities, values, "where I actually am" — that no
agent has access to; the agent brings structuring, research, and replanning. Neither role is
optional.

## Why WebMCP

The canvas is the point, not an implementation detail. Compass exposes its tools entirely
through [WebMCP](https://github.com/webmachinelearning/webmcp) (`navigator.modelContext`) on
the page itself, rather than a backend MCP server:

- The loop this app is built around — *agent asks → human answers → the page changes → human
  edits by hand → the agent's next suggestion reacts to that edit* — only works because agent
  and human are looking at, and acting on, the same live page in the same session.
- The human's edits (deleting a task, reordering the plan, checking something off) are not
  passive approvals; they carry judgment the agent doesn't otherwise have. Compass surfaces
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
- The canvas (goal, current situation, gaps, tasks, policies, session summaries) lives in one
  Postgres row per canvas, read and written as a JSON blob. There's no separate backend MCP
  endpoint — the WebMCP tools are thin wrappers around the same public HTTP API a human's
  browser uses.
- **Every write requires a fresh `readToken`**, returned by `read_canvas` (`GET
  /api/canvas/:id`) and rotated on every successful write. A missing or stale token gets a
  `409` whose body tells the caller to call `read_canvas` again — this both enforces "always
  read before you act" and doubles as optimistic concurrency control.
- Human edits made directly on the page (checking off / reordering / deleting a task) are
  queued server-side and delivered — once — inside the response of whichever tool the agent
  calls next. There is no way to push a notification into a paused chat, so this rides along
  with the next tool call instead.

## Project layout

```
cmd/compass/main.go          HTTP server: API + SSE + static SPA, one origin
internal/store/              Postgres persistence, readToken guard, human-action delivery
internal/api/                HTTP handlers implementing the canvas API contract
web/                         React + TypeScript SPA (Vite)
  src/webmcp/register.ts     registerTool() wiring for all 7 tools
  src/webmcp/descriptions.ts tool descriptions — the main agent-behavior tuning surface
  src/live.ts                SSE subscription with 5s-poll fallback
shakedown/                   smoke-test scenario for the canvas UI
```

## Running locally

Requires Go 1.24+, Node 20+, and Docker.

```bash
cp .env.example .env            # adjust if you like; defaults match docker-compose.yml
docker compose up -d db         # Postgres on localhost:55432
cd web && npm ci && npm run build && cd ..
DATABASE_URL=postgres://compass:compass@localhost:55432/compass?sslmode=disable \
  PORT=8080 STATIC_DIR=web/dist go run ./cmd/compass
```

Open `http://localhost:8080` — it creates a new canvas and redirects to its share URL
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

Open a Compass canvas URL in either, and ask the connected agent to help you set a goal — it
should discover and call `read_canvas`, `set_goal`, and the rest of the tools declared in
`web/src/webmcp/register.ts`.

## License

[MIT](./LICENSE)
