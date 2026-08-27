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

func getCanvas(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/canvas/"+id, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get canvas: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	return body
}

func getCanvasNoDeliver(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/canvas/"+id+"?deliver=0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get canvas (deliver=0): want 200, got %d: %s", rec.Code, rec.Body.String())
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

// TestWriteWithoutReadTokenIsRejected is the standing falsification probe
// for the read_canvas guard contract: writing without (or with a stale)
// readToken must fail with 409 and steer the caller back to read_canvas.
func TestWriteWithoutReadTokenIsRejected(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	rec := postJSON(t, h, "/api/canvas/"+id+"/goal", map[string]any{
		"title": "Ship the MVP",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("missing token: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read_canvas") {
		t.Fatalf("error body must steer the caller to read_canvas, got: %s", rec.Body.String())
	}

	canvas := getCanvas(t, h, id)
	staleToken := canvas["readToken"].(string) + "-stale"
	rec = postJSON(t, h, "/api/canvas/"+id+"/goal", map[string]any{
		"readToken": staleToken,
		"title":     "Ship the MVP",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale token: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read_canvas") {
		t.Fatalf("error body must steer the caller to read_canvas, got: %s", rec.Body.String())
	}
}

// TestHumanActionsNotRedelivered mirrors the store-level probe at the HTTP
// boundary: two consecutive GETs must not return the same human actions.
func TestHumanActionsNotRedelivered(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	rec := postJSON(t, h, "/api/canvas/"+id+"/human-actions", map[string]any{
		"type": "task.delete",
		"data": map[string]any{"taskId": "t1"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("record human action: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	first := getCanvas(t, h, id)
	actions, _ := first["humanActions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("1st GET: want 1 human action, got %d (%v)", len(actions), first["humanActions"])
	}

	second := getCanvas(t, h, id)
	actions2, _ := second["humanActions"].([]any)
	if len(actions2) != 0 {
		t.Fatalf("2nd consecutive GET: want 0 human actions (no re-delivery), got %d", len(actions2))
	}
}

// TestNonConsumingReadDoesNotDeliverHumanActions is the standing
// falsification probe for the must-fix contract change: the page's
// non-consuming read (?deliver=0) must never surface or consume pending
// humanActions, no matter how many times it's called, and a later consuming
// read (read_canvas, no query) must still receive every one of them.
func TestNonConsumingReadDoesNotDeliverHumanActions(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	rec := postJSON(t, h, "/api/canvas/"+id+"/human-actions", map[string]any{
		"type": "task.delete",
		"data": map[string]any{"taskId": "t1"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("record human action: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	for i := 0; i < 2; i++ {
		noDeliver := getCanvasNoDeliver(t, h, id)
		actions, _ := noDeliver["humanActions"].([]any)
		if len(actions) != 0 {
			t.Fatalf("deliver=0 read #%d: want 0 human actions (non-consuming, none shown), got %d", i+1, len(actions))
		}
	}

	consuming := getCanvas(t, h, id)
	actions, _ := consuming["humanActions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("consuming read after 2x deliver=0: want the 1 pending action still undelivered, got %d", len(actions))
	}

	// Once delivered via the consuming read, it must not reappear on a
	// further consuming read either.
	consuming2 := getCanvas(t, h, id)
	actions2, _ := consuming2["humanActions"].([]any)
	if len(actions2) != 0 {
		t.Fatalf("2nd consuming read: want 0 (already delivered), got %d", len(actions2))
	}
}

// TestPlanTasksPreservesExistingOrder is the standing falsification probe
// for the direction's task.order contract: plan_tasks must not reorder
// existing tasks even when the agent submits them in a different sequence;
// only genuinely new tasks may be appended.
func TestPlanTasksPreservesExistingOrder(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	canvas := getCanvas(t, h, id)
	token := canvas["readToken"].(string)

	rec := postJSON(t, h, "/api/canvas/"+id+"/tasks/plan", map[string]any{
		"readToken": token,
		"tasks": []map[string]any{
			{"text": "Write outline"},
			{"text": "Draft section 1"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("initial plan_tasks: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	canvas = getCanvas(t, h, id)
	tasks := canvas["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks after initial plan, got %d", len(tasks))
	}
	t0 := tasks[0].(map[string]any)
	t1 := tasks[1].(map[string]any)
	id0, order0 := t0["id"].(string), t0["order"].(float64)
	id1, order1 := t1["id"].(string), t1["order"].(float64)
	if order0 != 0 || order1 != 1 {
		t.Fatalf("want initial orders 0,1, got %v,%v", order0, order1)
	}

	// Agent tries to replan: resubmit both existing tasks in reversed
	// sequence, plus one genuinely new task.
	token = canvas["readToken"].(string)
	rec = postJSON(t, h, "/api/canvas/"+id+"/tasks/plan", map[string]any{
		"readToken": token,
		"tasks": []map[string]any{
			{"id": id1, "text": "Draft section 1 (revised)"},
			{"id": id0, "text": "Write outline (revised)"},
			{"text": "Write conclusion"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("replan: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	canvas = getCanvas(t, h, id)
	tasks = canvas["tasks"].([]any)
	if len(tasks) != 3 {
		t.Fatalf("want 3 tasks after replan, got %d", len(tasks))
	}
	byID := map[string]map[string]any{}
	for _, raw := range tasks {
		tk := raw.(map[string]any)
		byID[tk["id"].(string)] = tk
	}
	if byID[id0]["order"].(float64) != 0 {
		t.Fatalf("existing task %s must keep order 0, got %v", id0, byID[id0]["order"])
	}
	if byID[id1]["order"].(float64) != 1 {
		t.Fatalf("existing task %s must keep order 1, got %v", id1, byID[id1]["order"])
	}
	if byID[id0]["text"] != "Write outline (revised)" {
		t.Fatalf("existing task text must still update, got %v", byID[id0]["text"])
	}
	// The new task must land after the two existing ones, never reordering them.
	var newOrder float64 = -1
	for _, tk := range byID {
		if tk["text"] == "Write conclusion" {
			newOrder = tk["order"].(float64)
		}
	}
	if newOrder != 2 {
		t.Fatalf("new task must be appended at order 2, got %v", newOrder)
	}
}

// TestTasksUpdateHasNoOrderOrDeleteSurface is the standing falsification
// probe for the should-fix follow-up: the agent-facing /tasks/update
// endpoint must not move or remove a task even if a caller sends order/
// delete fields — those live only on /tasks/human, which has no WebMCP
// tool.
func TestTasksUpdateHasNoOrderOrDeleteSurface(t *testing.T) {
	h := newTestServer(t)
	id := createCanvas(t, h)

	canvas := getCanvas(t, h, id)
	token := canvas["readToken"].(string)
	rec := postJSON(t, h, "/api/canvas/"+id+"/tasks/plan", map[string]any{
		"readToken": token,
		"tasks":     []map[string]any{{"text": "Write outline"}, {"text": "Draft section 1"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("plan_tasks: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	canvas = getCanvas(t, h, id)
	tasks := canvas["tasks"].([]any)
	t0 := tasks[0].(map[string]any)
	taskID := t0["id"].(string)

	// Sending order/delete through /tasks/update must have no effect: the
	// endpoint doesn't decode those fields at all.
	token = canvas["readToken"].(string)
	rec = postJSON(t, h, "/api/canvas/"+id+"/tasks/update", map[string]any{
		"readToken": token,
		"updates":   []map[string]any{{"id": taskID, "order": 99, "delete": true}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks/update: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	canvas = getCanvas(t, h, id)
	tasks = canvas["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks/update with order/delete fields must not delete or reorder: want 2 tasks, got %d", len(tasks))
	}
	for _, raw := range tasks {
		tk := raw.(map[string]any)
		if tk["id"] == taskID && tk["order"].(float64) != 0 {
			t.Fatalf("order must be unchanged by tasks/update, got %v", tk["order"])
		}
	}

	// The same delete now works via the dedicated human-only endpoint.
	token = canvas["readToken"].(string)
	rec = postJSON(t, h, "/api/canvas/"+id+"/tasks/human", map[string]any{
		"readToken": token,
		"edits":     []map[string]any{{"id": taskID, "delete": true}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks/human delete: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	canvas = getCanvas(t, h, id)
	tasks = canvas["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks/human delete: want 1 task remaining, got %d", len(tasks))
	}
}
