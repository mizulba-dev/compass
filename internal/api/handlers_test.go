package api_test

import (
	"bytes"
	"context"
	"encoding/json"
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
// refactor of the production formula.
type estimatedRect struct{ x0, y0, x1, y1 float64 }

func estimatedRectFor(x, y float64, text string) estimatedRect {
	raw := 14*float64(len([]rune(text))) + 40
	width := raw
	if width > 260 {
		width = 260
	}
	if width < 40 {
		width = 40
	}
	lines := 1.0
	if raw > 260 {
		lines = 1
		for lines*260 < raw {
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
