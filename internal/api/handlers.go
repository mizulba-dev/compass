// Package api implements the Canvas HTTP contract: JSON request/response
// bodies, the readToken write guard, and the SSE event stream.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mizulba-dev/compass/internal/store"
)

// MapData mirrors the v3 "fog map" data model: a freeform mind-map canvas
// (nodes with position, parentage, fog/star state) plus an optional folded
// harvest. It is stored and returned as opaque JSON by internal/store; this
// struct exists only so the API layer can validate and mutate fields before
// persisting.
type MapData struct {
	Title   string   `json:"title"`
	Nodes   []Node   `json:"nodes"`
	Harvest *Harvest `json:"harvest"`
}

type Node struct {
	ID        string  `json:"id"`
	Text      string  `json:"text"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Parent    *string `json:"parent"`
	Root      bool    `json:"root,omitempty"`
	Dir       int     `json:"dir,omitempty"` // which side of its parent this subtree grows on: 1 (right) or -1 (left)
	Color     string  `json:"color,omitempty"`
	Kind      string  `json:"kind"` // "normal" | "question"
	Fog       bool    `json:"fog"`
	Star      bool    `json:"star"`
	Origin    string  `json:"origin"` // "human" | "agent"
	CreatedAt string  `json:"createdAt"`
	Pinned    bool    `json:"pinned,omitempty"` // true once a human has dragged it — auto-layout must never move it again
}

type Harvest struct {
	Goal     string   `json:"goal"`
	Premises []string `json:"premises"`
	Tasks    []string `json:"tasks"`
}

func emptyMap() MapData {
	return MapData{Nodes: []Node{}}
}

// API holds the dependencies shared by all handlers.
type API struct {
	store *store.Store
	hub   *hub
}

// New builds an API and returns it together with its http.Handler.
func New(s *store.Store) (*API, http.Handler) {
	a := &API{store: s, hub: newHub()}
	mux := http.NewServeMux()
	a.Register(mux)
	return a, mux
}

// Register attaches all /api routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/canvas", a.handleCreate)
	mux.HandleFunc("GET /api/canvas/{id}", a.handleGet)
	mux.HandleFunc("GET /api/canvas/{id}/events", a.handleEvents)
	mux.HandleFunc("POST /api/canvas/{id}/human-actions", a.handleHumanAction)
	mux.HandleFunc("POST /api/canvas/{id}/nodes", a.writeHandler(applyAddNodes))
	mux.HandleFunc("POST /api/canvas/{id}/nodes/human", a.writeHandler(applyAddNodeHuman))
	mux.HandleFunc("POST /api/canvas/{id}/node", a.writeHandler(applyUpdateNode))
	mux.HandleFunc("POST /api/canvas/{id}/node/human", a.writeHandler(applyNodeHuman))
	mux.HandleFunc("POST /api/canvas/{id}/harvest", a.writeHandler(applyHarvest))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

const staleTokenMessage = "missing or stale readToken: call read_map first, then retry with the returned readToken"

func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(emptyMap())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode initial map")
		return
	}
	id, err := a.store.Create(r.Context(), data)
	if err != nil {
		log.Printf("create canvas: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create canvas")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// canvasResponse is the GET /api/canvas/:id response shape: full canvas data
// plus the read token and any undelivered human actions.
type canvasResponse struct {
	ID           string        `json:"id"`
	Canvas       json.RawMessage `json:"-"`
	ReadToken    string        `json:"readToken"`
	HumanActions []actionJSON  `json:"humanActions"`
}

type actionJSON struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	At   time.Time       `json:"at"`
}

func (c canvasResponse) MarshalJSON() ([]byte, error) {
	// Merge the opaque canvas JSON object with the envelope fields so
	// clients see one flat object: {..canvas fields.., id, readToken, humanActions}.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c.Canvas, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	idRaw, _ := json.Marshal(c.ID)
	fields["id"] = idRaw
	tokenRaw, _ := json.Marshal(c.ReadToken)
	fields["readToken"] = tokenRaw
	actions := c.HumanActions
	if actions == nil {
		actions = []actionJSON{}
	}
	actionsRaw, err := json.Marshal(actions)
	if err != nil {
		return nil, err
	}
	fields["humanActions"] = actionsRaw
	return json.Marshal(fields)
}

// handleGet serves GET /api/canvas/:id. By default it is the consuming
// read_canvas call (delivers and clears pending humanActions) — reserved for
// the WebMCP tool. ?deliver=0 is the non-consuming variant for the page's
// own live-state hydration (initial load, poll fallback, post-write
// refresh): it never returns or delivers humanActions, so the page can never
// steal an event meant for the agent's next tool call.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if r.URL.Query().Get("deliver") == "0" {
		row, err := a.store.Get(r.Context(), id)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toResponse(row))
		return
	}

	row, err := a.store.ReadAndDeliver(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponseWithActions(row))
}

// toResponse never surfaces humanActions: it is the default for every
// page-facing surface (SSE snapshots, write-response refreshes, the
// non-consuming GET). Only toResponseWithActions, used by the consuming
// read_canvas path, includes them.
func toResponse(row *store.Row) canvasResponse {
	return canvasResponse{ID: row.ID, Canvas: row.Data, ReadToken: row.ReadToken}
}

func toResponseWithActions(row *store.Row) canvasResponse {
	resp := toResponse(row)
	actions := make([]actionJSON, 0, len(row.ActionsPending))
	for _, a := range row.ActionsPending {
		actions = append(actions, actionJSON{Seq: a.Seq, Type: a.Type, Data: a.Data, At: a.At})
	}
	resp.HumanActions = actions
	return resp
}

func handleStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "canvas not found")
	case errors.Is(err, store.ErrStaleToken):
		writeError(w, http.StatusConflict, staleTokenMessage)
	default:
		log.Printf("store error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

type humanActionRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (a *API) handleHumanAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req humanActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if err := a.store.RecordHumanAction(r.Context(), id, req.Type, req.Data); err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// applyFn mutates the map in place from the decoded request body. It
// returns a user-facing error message on invalid input, or "" on success.
type applyFn func(m *MapData, body json.RawMessage) string

// tokenEnvelope extracts the readToken every write request body carries
// alongside its own fields.
type tokenEnvelope struct {
	ReadToken string `json:"readToken"`
}

func (a *API) writeHandler(apply applyFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}

		var env tokenEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		row, err := a.store.Get(r.Context(), id)
		if err != nil {
			handleStoreErr(w, err)
			return
		}

		var m MapData
		if err := json.Unmarshal(row.Data, &m); err != nil {
			log.Printf("decode map: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		if msg := apply(&m, body); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}

		newData, err := json.Marshal(m)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode map")
			return
		}

		newToken, err := a.store.Write(r.Context(), id, env.ReadToken, newData)
		if err != nil {
			handleStoreErr(w, err)
			return
		}

		updated, err := a.store.Get(r.Context(), id)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		a.hub.broadcast(id, toResponse(updated))

		writeJSON(w, http.StatusOK, map[string]string{"readToken": newToken})
	}
}

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := a.hub.subscribe(id)
	defer a.hub.unsubscribe(id, ch)

	row, err := a.store.Get(r.Context(), id)
	if err == nil {
		writeSSE(w, toResponse(row))
		flusher.Flush()
	}

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snapshot := <-ch:
			writeSSE(w, snapshot)
			flusher.Flush()
		case <-heartbeat.C:
			// A comment line: ignored by SSE clients as a payload, but its
			// bytes keep the connection from looking idle to intermediaries
			// (and to the page's own liveness check — see web/src/live.ts).
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

// sseHeartbeatInterval must stay comfortably under web/src/live.ts's
// SSE_STALL_MS so a healthy connection never looks stalled to the page.
const sseHeartbeatInterval = 20 * time.Second

func writeSSE(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
}

// hub fans out canvas snapshots to per-canvas SSE subscribers via
// per-connection goroutines and channels (no shared mutable state beyond the
// subscriber map, which is itself guarded by a mutex).
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan canvasResponse]struct{}
}

func newHub() *hub {
	return &hub{subs: map[string]map[chan canvasResponse]struct{}{}}
}

func (h *hub) subscribe(id string) chan canvasResponse {
	ch := make(chan canvasResponse, 4)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[id] == nil {
		h.subs[id] = map[chan canvasResponse]struct{}{}
	}
	h.subs[id][ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(id string, ch chan canvasResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[id], ch)
	if len(h.subs[id]) == 0 {
		delete(h.subs, id)
	}
}

func (h *hub) broadcast(id string, snapshot canvasResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[id] {
		select {
		case ch <- snapshot:
		default:
			// Slow subscriber: drop the snapshot rather than block the
			// writer; the next write will broadcast a fresher one anyway.
		}
	}
}
