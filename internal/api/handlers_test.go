package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mizulba-dev/compass/internal/api"
	"github.com/mizulba-dev/compass/internal/store"
	"github.com/mizulba-dev/compass/internal/testutil"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	db := testutil.NewDB(t)
	s, err := store.New(context.Background(), db)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	_, handler := api.New(s)
	return handler
}

func createCanvas(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/canvas", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create canvas: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return body.ID
}

func getMap(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/canvas/"+id, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get map: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	return body
}

func getMapNoDeliver(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/canvas/"+id+"?deliver=0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get map (deliver=0): want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	return body
}

func postJSON(t *testing.T, h http.Handler, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// placeRoot creates the map's root node via the human endpoint (mirroring
// the mock: the human always places the first node) and returns its id.
// Fetches its own fresh readToken first — the guard applies to every write,
// including this one.
func placeRoot(t *testing.T, h http.Handler, id, text string) (nodeID, newToken string) {
	t.Helper()
	token := getMapNoDeliver(t, h, id)["readToken"].(string)
	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes/human", map[string]any{
		"readToken": token,
		"text":      text,
		"x":         100,
		"y":         100,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("place root: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ReadToken string `json:"readToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode place root response: %v", err)
	}
	m := getMapNoDeliver(t, h, id)
	nodes := m["nodes"].([]any)
	root := nodes[len(nodes)-1].(map[string]any)
	return root["id"].(string), resp.ReadToken
}

// TestWriteWithoutReadTokenIsRejected is the standing falsification probe
// for the read_map guard contract: writing without (or with a stale)
// readToken must fail with 409 and steer the caller back to read_map.
func TestWriteWithoutReadTokenIsRejected(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "住宅購入")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"parent": rootID,
		"nodes":  []map[string]any{{"text": "予算"}},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("missing token: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read_map") {
		t.Fatalf("error body must steer the caller back to read_map, got: %s", rec.Body.String())
	}

	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token + "-stale",
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "予算"}},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale token: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHumanActionsNotRedelivered mirrors the store-level probe at the HTTP
// boundary: two consecutive consuming GETs must not return the same human
// actions.
func TestHumanActionsNotRedelivered(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	rec := postJSON(t, h, "/api/canvas/"+id+"/human-actions", map[string]any{
		"type": "add",
		"data": map[string]any{"nodeId": "n1"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("record human action: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	first := getMap(t, h, id)
	actions, _ := first["humanActions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("1st GET: want 1 human action, got %d (%v)", len(actions), first["humanActions"])
	}

	second := getMap(t, h, id)
	actions2, _ := second["humanActions"].([]any)
	if len(actions2) != 0 {
		t.Fatalf("2nd consecutive GET: want 0 human actions (no re-delivery), got %d", len(actions2))
	}
}

// TestNonConsumingReadDoesNotDeliverHumanActions: ?deliver=0 must never
// surface or consume pending humanActions, and a later consuming read must
// still receive them.
func TestNonConsumingReadDoesNotDeliverHumanActions(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	rec := postJSON(t, h, "/api/canvas/"+id+"/human-actions", map[string]any{
		"type": "add",
		"data": map[string]any{"nodeId": "n1"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("record human action: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	for i := 0; i < 2; i++ {
		noDeliver := getMapNoDeliver(t, h, id)
		actions, _ := noDeliver["humanActions"].([]any)
		if len(actions) != 0 {
			t.Fatalf("deliver=0 read #%d: want 0 human actions (non-consuming), got %d", i+1, len(actions))
		}
	}

	consuming := getMap(t, h, id)
	actions, _ := consuming["humanActions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("consuming read after 2x deliver=0: want the 1 pending action still undelivered, got %d", len(actions))
	}
}

// TestAddNodesRejectsMoreThanThree is the standing falsification probe for
// the v3 structural cap: add_nodes must reject a call with 4+ nodes with
// 400, and must not partially apply it.
func TestAddNodesRejectsMoreThanThree(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "住宅購入")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes": []map[string]any{
			{"text": "予算"}, {"text": "ローン"}, {"text": "エリア"}, {"text": "内見"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("4 nodes: want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	m := getMapNoDeliver(t, h, id)
	nodes := m["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("rejected add_nodes must not partially apply: want 1 node (just root), got %d", len(nodes))
	}

	// Exactly 3 must succeed.
	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes": []map[string]any{
			{"text": "予算"}, {"text": "ローン"}, {"text": "エリア"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("3 nodes: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	nodes = m["nodes"].([]any)
	if len(nodes) != 4 {
		t.Fatalf("want 4 nodes (root + 3 children), got %d", len(nodes))
	}
}

// TestUpdateNodeCannotMoveOrDelete mirrors the v1 "agent capability lives
// at the decode surface, not the schema" pattern: /node (update_node) must
// not accept fields that would move or delete a node, even if a caller
// sends them — those are exclusive to /node/human.
func TestUpdateNodeCannotMoveOrDelete(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "住宅購入")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "予算の全体像をつかむ"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add_nodes: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	nodes := m["nodes"].([]any)
	child := nodes[len(nodes)-1].(map[string]any)
	childID := child["id"].(string)
	origX := child["x"].(float64)
	token = m["readToken"].(string)

	// update_node has no x/y/delete fields at all — sending them must be a
	// no-op for those fields (JSON decode simply drops unknown data since
	// updateNodeRequest never declares them).
	rec = postJSON(t, h, "/api/canvas/"+id+"/node", map[string]any{
		"readToken": token,
		"id":        childID,
		"x":         9999,
		"delete":    true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update_node: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m = getMapNoDeliver(t, h, id)
	nodes = m["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("node must not be deleted via update_node: want 2 nodes, got %d", len(nodes))
	}
	var found map[string]any
	for _, raw := range nodes {
		n := raw.(map[string]any)
		if n["id"] == childID {
			found = n
		}
	}
	if found == nil {
		t.Fatal("child node disappeared")
	}
	if found["x"].(float64) != origX {
		t.Fatalf("x must be unchanged by update_node: want %v, got %v", origX, found["x"])
	}
}
