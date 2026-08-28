package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mizulba-dev/webmcp-demo/internal/api"
	"github.com/mizulba-dev/webmcp-demo/internal/store"
	"github.com/mizulba-dev/webmcp-demo/internal/testutil"
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

// estimatedRect mirrors internal/api/mutations.go's estimateRect, hand-
// written independently here rather than imported: the test asserts an
// emergent property (no two node footprints overlap after a realistic
// bulk-add), not a specific internal call sequence, so it should use its
// own copy of the geometry to catch a real regression rather than a
// refactor of the production formula. isWideCharFor mirrors isWideRune:
// CJK/Hangul/fullwidth glyphs render at roughly a full em, wider than the
// Latin average, and text wraps within the node's inner content width
// (narrower than its own outer max-width), not the outer width itself.
func isWideCharFor(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x30FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFF00 && r <= 0xFFEF:
		return true
	}
	return false
}

const (
	estimatedMaxWidth     = 260.0
	estimatedContentWidth = 232.0
)

type estimatedRect struct{ x0, y0, x1, y1 float64 }

func estimatedRectFor(x, y float64, text string) estimatedRect {
	ink := 0.0
	for _, r := range text {
		if isWideCharFor(r) {
			ink += 15
		} else {
			ink += 14
		}
	}
	raw := ink + 40
	width := raw
	if width > estimatedMaxWidth {
		width = estimatedMaxWidth
	}
	if width < 40 {
		width = 40
	}
	lines := 1.0
	if raw > estimatedContentWidth {
		lines = 1
		for lines*estimatedContentWidth < raw {
			lines++
		}
	}
	height := 40 + (lines-1)*22
	return estimatedRect{x0: x, y0: y, x1: x + width, y1: y + height}
}

func rectsOverlap(a, b estimatedRect) bool {
	return a.x0 < b.x1 && a.x1 > b.x0 && a.y0 < b.y1 && a.y1 > b.y0
}

// TestAddNodesAvoidsOverlapAcrossBranches is the standing falsification
// probe for the FB-driven layout fix: bulk-adding branches and their
// children (the shape a real "3 areas x children" agent turn produces) must
// never leave two node footprints overlapping, even across different
// branches.
func TestAddNodesAvoidsOverlapAcrossBranches(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "考えたいこと")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes": []map[string]any{
			{"text": "予算まわりを整理する"},
			{"text": "住宅ローンの基本を知る"},
			{"text": "候補エリアを2〜3か所に絞る"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add branch nodes: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	nodes := m["nodes"].([]any)
	var branchIDs []string
	for _, raw := range nodes {
		n := raw.(map[string]any)
		if n["id"] != rootID {
			branchIDs = append(branchIDs, n["id"].(string))
		}
	}
	if len(branchIDs) != 3 {
		t.Fatalf("want 3 branch nodes, got %d", len(branchIDs))
	}

	childTexts := [][]string{
		{"月々いくら払えるか決める", "諸費用は物件価格の6〜9%程度", "頭金をいくら用意するか決める"},
		{"事前審査を受ける", "変動と固定の違いを学ぶ", "借入可能額の目安をつかむ"},
		{"通勤圏で2駅だけ候補にする", "中古相場をSUUMOで眺める", "内見を1件予約してみる"},
	}
	for i, branchID := range branchIDs {
		var nodesPayload []map[string]any
		for _, text := range childTexts[i] {
			nodesPayload = append(nodesPayload, map[string]any{"text": text})
		}
		rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
			"readToken": token,
			"parent":    branchID,
			"nodes":     nodesPayload,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("add children of branch %d: want 200, got %d: %s", i, rec.Code, rec.Body.String())
		}
		m := getMapNoDeliver(t, h, id)
		token = m["readToken"].(string)
	}

	final := getMapNoDeliver(t, h, id)
	finalNodes := final["nodes"].([]any)
	if len(finalNodes) != 13 { // root + 3 branches + 3*3 children
		t.Fatalf("want 13 nodes total, got %d", len(finalNodes))
	}

	type nt struct {
		id   string
		rect estimatedRect
	}
	var rects []nt
	for _, raw := range finalNodes {
		n := raw.(map[string]any)
		rects = append(rects, nt{
			id:   n["id"].(string),
			rect: estimatedRectFor(n["x"].(float64), n["y"].(float64), n["text"].(string)),
		})
	}
	for i := 0; i < len(rects); i++ {
		for j := i + 1; j < len(rects); j++ {
			if rectsOverlap(rects[i].rect, rects[j].rect) {
				t.Fatalf("node %s and %s footprints overlap: %+v vs %+v", rects[i].id, rects[j].id, rects[i].rect, rects[j].rect)
			}
		}
	}
}

// TestRemoveNodeDeletesSubtreeButNotRoot is the standing falsification
// probe for the remove_node WebMCP tool: it must remove a node and its
// entire subtree, must refuse to remove the root with 400, and the removal
// must actually be visible on a subsequent read.
func TestRemoveNodeDeletesSubtreeButNotRoot(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "考えたいこと")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "ブランチA"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add branch: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	var branchID string
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] != rootID {
			branchID = n["id"].(string)
		}
	}

	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    branchID,
		"nodes":     []map[string]any{{"text": "子1"}, {"text": "子2"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add children: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	if len(m["nodes"].([]any)) != 4 {
		t.Fatalf("want 4 nodes before removal, got %d", len(m["nodes"].([]any)))
	}

	// Removing the root must be rejected with 400.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node/remove", map[string]any{
		"readToken": token,
		"id":        rootID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("remove root: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	if len(m["nodes"].([]any)) != 4 {
		t.Fatalf("rejected root removal must not change node count: want 4, got %d", len(m["nodes"].([]any)))
	}

	// Removing an unknown id must fail without mutating state.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node/remove", map[string]any{
		"readToken": token,
		"id":        "does-not-exist",
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("remove unknown id: want an error status, got 200")
	}

	// Removing the branch must take its 2 children with it, leaving only the root.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node/remove", map[string]any{
		"readToken": token,
		"id":        branchID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("remove branch: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	final := getMapNoDeliver(t, h, id)
	finalNodes := final["nodes"].([]any)
	if len(finalNodes) != 1 {
		t.Fatalf("want 1 node (just root) after subtree removal, got %d: %v", len(finalNodes), finalNodes)
	}
	if finalNodes[0].(map[string]any)["id"] != rootID {
		t.Fatalf("the surviving node must be the root, got %v", finalNodes[0])
	}
}

// TestArrangeNodesMovesAndAvoidsCollisions is the standing falsification
// probe for arrange_nodes: a bulk move must actually reposition every node,
// an unknown id anywhere in the batch must reject the whole call (400,
// state unchanged), and colliding target coordinates must be corrected so
// no two nodes end up overlapping.
func TestArrangeNodesMovesAndAvoidsCollisions(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "考えたいこと")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "枝A"}, {"text": "枝B"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add branches: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	var branchAID, branchBID string
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		switch n["text"] {
		case "枝A":
			branchAID = n["id"].(string)
		case "枝B":
			branchBID = n["id"].(string)
		}
	}

	// Unknown id in the batch must reject the whole call, changing nothing.
	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes/arrange", map[string]any{
		"readToken": token,
		"moves": []map[string]any{
			{"id": rootID, "x": 500, "y": 500},
			{"id": "does-not-exist", "x": 0, "y": 0},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("arrange with unknown id: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	unchanged := getMapNoDeliver(t, h, id)
	for _, raw := range unchanged["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == rootID && n["x"].(float64) == 500 {
			t.Fatalf("rejected arrange must not partially apply: root x was changed to %v", n["x"])
		}
	}

	// Empty moves must be rejected too.
	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes/arrange", map[string]any{
		"readToken": token,
		"moves":     []map[string]any{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("arrange with empty moves: want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// A real arrange call: send colliding coordinates for root, branch A,
	// and branch B (all the same point) — the server must correct them so
	// none overlap, while still actually moving them near the requested spot.
	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes/arrange", map[string]any{
		"readToken": token,
		"moves": []map[string]any{
			{"id": rootID, "x": 1000, "y": 1000},
			{"id": branchAID, "x": 1000, "y": 1000},
			{"id": branchBID, "x": 1000, "y": 1000},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("arrange: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	final := getMapNoDeliver(t, h, id)
	var rects []estimatedRect
	for _, raw := range final["nodes"].([]any) {
		n := raw.(map[string]any)
		x := n["x"].(float64)
		if x < 900 {
			t.Fatalf("node %v was not moved toward the requested position: x=%v", n["id"], x)
		}
		rects = append(rects, estimatedRectFor(x, n["y"].(float64), n["text"].(string)))
	}
	for i := 0; i < len(rects); i++ {
		for j := i + 1; j < len(rects); j++ {
			if rectsOverlap(rects[i], rects[j]) {
				t.Fatalf("arranged nodes still overlap: %+v vs %+v", rects[i], rects[j])
			}
		}
	}
}

// TestTaskKindAndDoneRoundTrip is the standing falsification probe for the
// task-node addition (direction addendum 5): add_nodes must accept kind:
// "task" and default it to not done; both the agent's update_node and the
// human's node/human endpoints must accept done: true/false and persist it;
// and a "done" humanAction must round-trip through the delivery pipe like
// any other action type.
func TestTaskKindAndDoneRoundTrip(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "住宅購入")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "頭金を貯める", "kind": "task"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add_nodes: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	nodes := m["nodes"].([]any)
	task := nodes[len(nodes)-1].(map[string]any)
	taskID := task["id"].(string)
	token = m["readToken"].(string)

	if task["kind"] != "task" {
		t.Fatalf("want kind task, got %v", task["kind"])
	}
	if task["done"] != false {
		t.Fatalf("want a freshly added task node to start not done, got %v", task["done"])
	}

	// Agent-facing update_node accepts done: true.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node", map[string]any{
		"readToken": token,
		"id":        taskID,
		"done":      true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update_node done:true: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == taskID && n["done"] != true {
			t.Fatalf("update_node done:true did not persist: %v", n["done"])
		}
	}

	// Human-facing node/human accepts done: false (reopening the task).
	rec = postJSON(t, h, "/api/canvas/"+id+"/node/human", map[string]any{
		"readToken": token,
		"id":        taskID,
		"done":      false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("node/human done:false: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == taskID && n["done"] != false {
			t.Fatalf("node/human done:false did not persist: %v", n["done"])
		}
	}

	// A "done" humanAction round-trips through the delivery pipe: delivered
	// once, then not redelivered on a later read.
	rec = postJSON(t, h, "/api/canvas/"+id+"/human-actions", map[string]any{
		"type": "done",
		"data": map[string]any{"nodeId": taskID, "done": false},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("record done human action: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	first := getMap(t, h, id)
	actions, _ := first["humanActions"].([]any)
	if len(actions) != 1 || actions[0].(map[string]any)["type"] != "done" {
		t.Fatalf("want 1 delivered humanAction of type done, got %v", first["humanActions"])
	}
	second := getMap(t, h, id)
	actions2, _ := second["humanActions"].([]any)
	if len(actions2) != 0 {
		t.Fatalf("2nd consecutive read: want 0 human actions (no re-delivery), got %d", len(actions2))
	}
}

// TestUpdateNodeKindConversionResetsDone is the standing falsification
// probe for direction addendum 6: the agent-facing update_node tool must be
// able to convert an existing node's kind (e.g. normal -> task), and
// converting a task back away from "task" must reset its done flag rather
// than carry a stale completion state into whatever it becomes next.
func TestUpdateNodeKindConversionResetsDone(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "住宅購入")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "住宅ローンの基本を知る"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add_nodes: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	nodes := m["nodes"].([]any)
	nodeID := nodes[len(nodes)-1].(map[string]any)["id"].(string)
	token = m["readToken"].(string)

	// normal -> task via update_node.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node", map[string]any{
		"readToken": token,
		"id":        nodeID,
		"kind":      "task",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update_node kind:task: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	var found map[string]any
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == nodeID {
			found = n
		}
	}
	if found["kind"] != "task" {
		t.Fatalf("want kind task after conversion, got %v", found["kind"])
	}

	// Mark it done.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node", map[string]any{
		"readToken": token,
		"id":        nodeID,
		"done":      true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update_node done:true: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == nodeID && n["done"] != true {
			t.Fatalf("done:true did not persist before conversion back: %v", n["done"])
		}
	}

	// task -> normal must reset done to false, not carry it over.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node", map[string]any{
		"readToken": token,
		"id":        nodeID,
		"kind":      "normal",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update_node kind:normal: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == nodeID {
			if n["kind"] != "normal" {
				t.Fatalf("want kind normal after converting back, got %v", n["kind"])
			}
			if n["done"] != false {
				t.Fatalf("converting away from task must reset done to false, got %v", n["done"])
			}
		}
	}

	// An invalid kind value must be rejected, not silently coerced.
	token = m["readToken"].(string)
	rec = postJSON(t, h, "/api/canvas/"+id+"/node", map[string]any{
		"readToken": token,
		"id":        nodeID,
		"kind":      "bogus",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update_node kind:bogus: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRelayoutAbsorbsHumanDragsAndKeepsTopBranchesDisjoint is the standing
// falsification probe for direction addendum 8: layout ownership belongs to
// the engine, not to whoever last moved a node. It reproduces the real
// usage pattern that motivated the change — 3 branches grown across 3
// separate turns, with a human drag of one node interleaved between them —
// and requires that in the final state:
//  1. every non-root node sits exactly +280px (right) or -260px (left) from
//     its actual parent, INCLUDING the node the human dragged — a plain
//     move is only honored until the next structural change, then it's
//     absorbed back into the tidy layout like everything else;
//  2. no two node footprints overlap (same invariant as
//     TestAddNodesAvoidsOverlapAcrossBranches, over a shape now grown
//     across multiple turns with a drag in between); and
//  3. each top branch's own subtree occupies a y-range disjoint from every
//     other top branch's — i.e. branches are stacked in contiguous bands,
//     never interleaved, so edges between different branches can't cross.
func TestRelayoutAbsorbsHumanDragsAndKeepsTopBranchesDisjoint(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "考えたいこと")

	// Turn 1: 3 top branches off the root.
	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes": []map[string]any{
			{"text": "branch A"},
			{"text": "branch B"},
			{"text": "branch C"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add branches: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	var branchIDs []string
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] != rootID {
			branchIDs = append(branchIDs, n["id"].(string))
		}
	}
	if len(branchIDs) != 3 {
		t.Fatalf("want 3 branches, got %d", len(branchIDs))
	}

	// Turn 2: 3 children under branch A only.
	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    branchIDs[0],
		"nodes":     []map[string]any{{"text": "A child 1"}, {"text": "A child 2"}, {"text": "A child 3"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add children of branch A: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	var branchAChildID string
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["parent"] == branchIDs[0] {
			branchAChildID = n["id"].(string)
			break
		}
	}
	if branchAChildID == "" {
		t.Fatal("expected at least one child of branch A")
	}

	// Interleaved human drag: move a branch-A child far away — this must
	// NOT survive the next structural change.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node/human", map[string]any{
		"readToken": token,
		"id":        branchAChildID,
		"x":         9999,
		"y":         -9999,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("drag branch A child: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == branchAChildID && (n["x"].(float64) != 9999 || n["y"].(float64) != -9999) {
			t.Fatalf("drag did not take effect immediately: got (%v,%v)", n["x"], n["y"])
		}
	}

	// Turn 3: 3 children under branch B — a structural change elsewhere,
	// which must relayout the WHOLE map, absorbing the branch-A drag too.
	rec = postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    branchIDs[1],
		"nodes":     []map[string]any{{"text": "B child 1"}, {"text": "B child 2"}, {"text": "B child 3"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add children of branch B: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	final := getMapNoDeliver(t, h, id)
	finalNodes := final["nodes"].([]any)
	if len(finalNodes) != 10 { // root + 3 branches + 3 (A) + 3 (B)
		t.Fatalf("want 10 nodes total, got %d", len(finalNodes))
	}
	byID := map[string]map[string]any{}
	for _, raw := range finalNodes {
		n := raw.(map[string]any)
		byID[n["id"].(string)] = n
	}

	// (1) Every non-root node — including the dragged one — sits exactly
	// +280/-260 from its actual parent.
	for id, n := range byID {
		if n["parent"] == nil {
			continue // root
		}
		parent := byID[n["parent"].(string)]
		dx := n["x"].(float64) - parent["x"].(float64)
		want := 280.0
		if n["dir"].(float64) < 0 {
			want = -260.0
		}
		if dx != want {
			t.Fatalf("node %s: want x offset from parent %v, got %v", id, want, dx)
		}
	}
	if byID[branchAChildID]["x"].(float64) == 9999 {
		t.Fatal("dragged node's x survived a later structural change — layout must absorb drags")
	}

	// (2) No two node footprints overlap.
	var rects []estimatedRect
	for _, n := range byID {
		rects = append(rects, estimatedRectFor(n["x"].(float64), n["y"].(float64), n["text"].(string)))
	}
	for i := 0; i < len(rects); i++ {
		for j := i + 1; j < len(rects); j++ {
			if rectsOverlap(rects[i], rects[j]) {
				t.Fatalf("footprints overlap: %+v vs %+v", rects[i], rects[j])
			}
		}
	}

	// (3) Each top branch's own subtree occupies a y-range disjoint from
	// every other top branch's.
	subtreeYRange := func(rootID string) (minY, maxY float64) {
		minY, maxY = math.Inf(1), math.Inf(-1)
		var walk func(id string)
		walk = func(id string) {
			n := byID[id]
			y := n["y"].(float64)
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
			for cid, c := range byID {
				if c["parent"] == id {
					walk(cid)
				}
			}
		}
		walk(rootID)
		return
	}
	// Only branches on the SAME side can possibly have crossing edges —
	// opposite-side branches are mirrored around the root and never
	// interact, so their y-ranges are free to overlap.
	type yrange struct {
		id       string
		dir      float64
		min, max float64
	}
	var ranges []yrange
	for _, bid := range branchIDs {
		min, max := subtreeYRange(bid)
		ranges = append(ranges, yrange{bid, byID[bid]["dir"].(float64), min, max})
	}
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			a, b := ranges[i], ranges[j]
			sameSide := (a.dir < 0) == (b.dir < 0)
			if sameSide && a.min <= b.max && b.min <= a.max {
				t.Fatalf("same-side top branches %s and %s have overlapping y-ranges: [%v,%v] vs [%v,%v]", a.id, b.id, a.min, a.max, b.min, b.max)
			}
		}
	}
}

// TestTidyRelayoutsOnDemand is the standing falsification probe for the
// manual Tidy button: a drag with no follow-up structural change leaves the
// map looking untidy (relayout only fires automatically on add/remove), and
// POST /tidy must be the on-demand fix — after calling it, the dragged
// node must be back at its tidy position, not off wherever it was dropped.
func TestTidyRelayoutsOnDemand(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "考えたいこと")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes":     []map[string]any{{"text": "branch A"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add branch: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	var branchID string
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] != rootID {
			branchID = n["id"].(string)
		}
	}

	// Drag it away — no structural change follows, so it must stay put.
	rec = postJSON(t, h, "/api/canvas/"+id+"/node/human", map[string]any{
		"readToken": token,
		"id":        branchID,
		"x":         9999,
		"y":         -9999,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("drag: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m = getMapNoDeliver(t, h, id)
	token = m["readToken"].(string)
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == branchID && (n["x"].(float64) != 9999 || n["y"].(float64) != -9999) {
			t.Fatalf("drag without a structural change should not be reflowed on its own: got (%v,%v)", n["x"], n["y"])
		}
	}

	// Tidy: an on-demand relayout, with no add/remove involved.
	rec = postJSON(t, h, "/api/canvas/"+id+"/tidy", map[string]any{"readToken": token})
	if rec.Code != http.StatusOK {
		t.Fatalf("tidy: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	final := getMapNoDeliver(t, h, id)
	var root, branch map[string]any
	for _, raw := range final["nodes"].([]any) {
		n := raw.(map[string]any)
		if n["id"] == rootID {
			root = n
		} else if n["id"] == branchID {
			branch = n
		}
	}
	dx := branch["x"].(float64) - root["x"].(float64)
	want := 280.0
	if branch["dir"].(float64) < 0 {
		want = -260.0
	}
	if dx != want {
		t.Fatalf("after tidy: want x offset from root %v, got %v", want, dx)
	}
	if branch["y"].(float64) != root["y"].(float64) {
		t.Fatalf("after tidy: a single leaf branch should sit on root's own row, got y=%v (root y=%v)", branch["y"], root["y"])
	}
}

// TestRelayoutGivesLongJapaneseSiblingsEnoughRoom is the standing
// falsification probe for the reported bug: two long Japanese question
// nodes under the same parent wrap to multiple lines (CJK glyphs render
// near a full em, wider than the ~14px/rune average that fits Latin text),
// and the old fixed 64px row height didn't leave room for that — the two
// footprints overlapped. Reproduces the exact strings from the bug report.
func TestRelayoutGivesLongJapaneseSiblingsEnoughRoom(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)
	rootID, token := placeRoot(t, h, id, "住宅購入")

	rec := postJSON(t, h, "/api/canvas/"+id+"/nodes", map[string]any{
		"readToken": token,
		"parent":    rootID,
		"nodes": []map[string]any{
			{"text": "絶対に譲れない条件は何ですか？（立地・広さ・性能・間取りなど）", "kind": "question"},
			{"text": "妥協してもよい条件はどれくらいありますか？（予算・築年数・駅からの距離など）", "kind": "question"},
			{"text": "いつまでに住み替えたいですか？", "kind": "task"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add_nodes: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := getMapNoDeliver(t, h, id)
	var rects []estimatedRect
	var ids []string
	for _, raw := range m["nodes"].([]any) {
		n := raw.(map[string]any)
		ids = append(ids, n["id"].(string))
		rects = append(rects, estimatedRectFor(n["x"].(float64), n["y"].(float64), n["text"].(string)))
	}
	for i := 0; i < len(rects); i++ {
		for j := i + 1; j < len(rects); j++ {
			if rectsOverlap(rects[i], rects[j]) {
				t.Fatalf("long Japanese siblings %s and %s overlap: %+v vs %+v", ids[i], ids[j], rects[i], rects[j])
			}
		}
	}
}
