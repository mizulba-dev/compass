package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mizulba-dev/compass/internal/store"
	"github.com/mizulba-dev/compass/internal/testutil"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	db := testutil.NewDB(t)
	s, err := store.New(context.Background(), db)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

func TestCreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id, err := s.Create(ctx, json.RawMessage(`{"goal":null}`))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	row, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.ReadToken == "" {
		t.Fatal("expected non-empty read token")
	}
	if len(row.ActionsPending) != 0 {
		t.Fatalf("expected no pending actions, got %d", len(row.ActionsPending))
	}
}

// TestWriteRequiresFreshToken is a standing falsification probe for the
// direction's guard contract: a write with a missing or stale token must be
// rejected without mutating state, so the caller is forced back to
// read_canvas.
func TestWriteRequiresFreshToken(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id, err := s.Create(ctx, json.RawMessage(`{"goal":null}`))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Write(ctx, id, "", json.RawMessage(`{"goal":"x"}`)); err != store.ErrStaleToken {
		t.Fatalf("empty token: want ErrStaleToken, got %v", err)
	}

	row, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if _, err := s.Write(ctx, id, row.ReadToken+"-stale", json.RawMessage(`{"goal":"x"}`)); err != store.ErrStaleToken {
		t.Fatalf("stale token: want ErrStaleToken, got %v", err)
	}

	after, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(after.Data) != `{"goal": null}` && string(after.Data) != `{"goal":null}` {
		t.Fatalf("data must be unchanged after rejected writes, got %s", after.Data)
	}

	newToken, err := s.Write(ctx, id, row.ReadToken, json.RawMessage(`{"goal":"x"}`))
	if err != nil {
		t.Fatalf("write with fresh token should succeed: %v", err)
	}
	if newToken == row.ReadToken {
		t.Fatal("token must rotate on successful write")
	}
}

// TestHumanActionsDeliveredOnce is a standing falsification probe: reading
// the canvas twice in a row must not redeliver the same human actions.
func TestHumanActionsDeliveredOnce(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id, err := s.Create(ctx, json.RawMessage(`{"goal":null}`))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.RecordHumanAction(ctx, id, "task.delete", json.RawMessage(`{"taskId":"t1"}`)); err != nil {
		t.Fatalf("RecordHumanAction: %v", err)
	}
	if err := s.RecordHumanAction(ctx, id, "task.reorder", json.RawMessage(`{"taskId":"t2","order":0}`)); err != nil {
		t.Fatalf("RecordHumanAction: %v", err)
	}

	first, err := s.ReadAndDeliver(ctx, id)
	if err != nil {
		t.Fatalf("ReadAndDeliver (1st): %v", err)
	}
	if len(first.ActionsPending) != 2 {
		t.Fatalf("1st read: want 2 actions, got %d", len(first.ActionsPending))
	}

	second, err := s.ReadAndDeliver(ctx, id)
	if err != nil {
		t.Fatalf("ReadAndDeliver (2nd): %v", err)
	}
	if len(second.ActionsPending) != 0 {
		t.Fatalf("2nd consecutive read: want 0 actions (no re-delivery), got %d", len(second.ActionsPending))
	}
	if second.ActionsDeliveredSeq != 2 {
		t.Fatalf("want delivered seq advanced to 2, got %d", second.ActionsDeliveredSeq)
	}

	// A new action recorded after delivery must not collide with prior seqs.
	if err := s.RecordHumanAction(ctx, id, "task.toggle", json.RawMessage(`{"taskId":"t3"}`)); err != nil {
		t.Fatalf("RecordHumanAction: %v", err)
	}
	third, err := s.ReadAndDeliver(ctx, id)
	if err != nil {
		t.Fatalf("ReadAndDeliver (3rd): %v", err)
	}
	if len(third.ActionsPending) != 1 || third.ActionsPending[0].Seq != 3 {
		t.Fatalf("want exactly one new action with seq 3, got %+v", third.ActionsPending)
	}
}
