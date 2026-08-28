// Package store implements the Postgres-backed persistence for a single
// Canvas per row. The canvas payload is stored as opaque JSON; this package
// never interprets its shape beyond what is needed for the read-token guard
// and human-action delivery bookkeeping.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a canvas id does not exist.
var ErrNotFound = errors.New("canvas not found")

// ErrStaleToken is returned when a write is attempted with a missing or
// outdated read token.
var ErrStaleToken = errors.New("stale or missing read token")

const schema = `
CREATE TABLE IF NOT EXISTS canvases (
	id text PRIMARY KEY,
	data jsonb NOT NULL,
	read_token text NOT NULL,
	actions_pending jsonb NOT NULL DEFAULT '[]',
	actions_delivered_seq integer NOT NULL DEFAULT 0,
	updated_at timestamptz NOT NULL DEFAULT now()
);
`

// Store wraps a database handle.
type Store struct {
	db *sql.DB
}

// New opens the schema on db (creating the table if absent) and returns a Store.
func New(ctx context.Context, db *sql.DB) (*Store, error) {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// HumanAction is a single human-driven canvas edit recorded for later
// delivery to the agent via a tool result.
type HumanAction struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	At   time.Time       `json:"at"`
}

// Row is the full persisted state of one canvas.
type Row struct {
	ID                  string
	Data                json.RawMessage
	ReadToken           string
	ActionsPending      []HumanAction
	ActionsDeliveredSeq int64
	UpdatedAt           time.Time
}

// NewID returns a 128-bit random URL-safe id, unguessable by enumeration.
func NewID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Create inserts a new canvas with the given initial data and returns its id.
func (s *Store) Create(ctx context.Context, data json.RawMessage) (string, error) {
	id, err := NewID()
	if err != nil {
		return "", err
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO canvases (id, data, read_token, actions_pending, actions_delivered_seq)
		 VALUES ($1, $2, $3, '[]', 0)`,
		id, data, token,
	)
	if err != nil {
		return "", fmt.Errorf("insert canvas: %w", err)
	}
	return id, nil
}

// Get returns the full row for id.
func (s *Store) Get(ctx context.Context, id string) (*Row, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, data, read_token, actions_pending, actions_delivered_seq, updated_at
		 FROM canvases WHERE id = $1`, id)
	return scanRow(row)
}

func scanRow(row *sql.Row) (*Row, error) {
	var r Row
	var pending []byte
	if err := row.Scan(&r.ID, &r.Data, &r.ReadToken, &pending, &r.ActionsDeliveredSeq, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan canvas: %w", err)
	}
	if len(pending) > 0 {
		if err := json.Unmarshal(pending, &r.ActionsPending); err != nil {
			return nil, fmt.Errorf("decode actions_pending: %w", err)
		}
	}
	return &r, nil
}

// ReadAndDeliver returns the canvas plus any undelivered human actions, and
// atomically marks those actions as delivered (actions_pending is cleared,
// actions_delivered_seq advances to the highest seq just delivered). Calling
// it twice in a row yields an empty action list the second time.
func (s *Store) ReadAndDeliver(ctx context.Context, id string) (*Row, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx,
		`SELECT id, data, read_token, actions_pending, actions_delivered_seq, updated_at
		 FROM canvases WHERE id = $1 FOR UPDATE`, id)
	r, err := scanRow(row)
	if err != nil {
		return nil, err
	}

	if len(r.ActionsPending) > 0 {
		maxSeq := r.ActionsDeliveredSeq
		for _, a := range r.ActionsPending {
			if a.Seq > maxSeq {
				maxSeq = a.Seq
			}
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE canvases SET actions_pending = '[]', actions_delivered_seq = $2 WHERE id = $1`,
			id, maxSeq,
		)
		if err != nil {
			return nil, fmt.Errorf("mark actions delivered: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r, nil
}

// Write persists newData for id, requires that expectToken matches the
// canvas's current read token, and issues a new token on success. It returns
// ErrStaleToken (without writing) when expectToken is empty or does not match.
func (s *Store) Write(ctx context.Context, id, expectToken string, newData json.RawMessage) (newToken string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var currentToken string
	err = tx.QueryRowContext(ctx, `SELECT read_token FROM canvases WHERE id = $1 FOR UPDATE`, id).Scan(&currentToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("lock canvas: %w", err)
	}

	if expectToken == "" || expectToken != currentToken {
		return "", ErrStaleToken
	}

	next, err := newTokenFn()
	if err != nil {
		return "", err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE canvases SET data = $2, read_token = $3, updated_at = now() WHERE id = $1`,
		id, newData, next,
	)
	if err != nil {
		return "", fmt.Errorf("update canvas: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return next, nil
}

// newTokenFn is a variable indirection so tests can stub token generation if
// ever needed; production code always uses newToken.
var newTokenFn = newToken

// RecordHumanAction appends a human-driven action to the undelivered queue.
// The assigned seq is monotonic across the canvas's lifetime (delivered or
// not), so re-delivery never reuses a seq.
func (s *Store) RecordHumanAction(ctx context.Context, id, actionType string, data json.RawMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var pendingRaw []byte
	var deliveredSeq int64
	err = tx.QueryRowContext(ctx,
		`SELECT actions_pending, actions_delivered_seq FROM canvases WHERE id = $1 FOR UPDATE`, id,
	).Scan(&pendingRaw, &deliveredSeq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock canvas: %w", err)
	}

	var pending []HumanAction
	if len(pendingRaw) > 0 {
		if err := json.Unmarshal(pendingRaw, &pending); err != nil {
			return fmt.Errorf("decode actions_pending: %w", err)
		}
	}

	// "move" actions fire continuously while a node is dragged. Collapsing
	// them to the latest one per node keeps the agent's next tool result
	// from carrying a queue of stale intermediate positions — only where
	// the node ended up matters.
	if actionType == "move" {
		if nodeID := actionNodeID(data); nodeID != "" {
			kept := pending[:0]
			for _, a := range pending {
				if a.Type == "move" && actionNodeID(a.Data) == nodeID {
					continue
				}
				kept = append(kept, a)
			}
			pending = kept
		}
	}

	maxSeq := deliveredSeq
	for _, a := range pending {
		if a.Seq > maxSeq {
			maxSeq = a.Seq
		}
	}
	pending = append(pending, HumanAction{
		Seq:  maxSeq + 1,
		Type: actionType,
		Data: data,
		At:   time.Now().UTC(),
	})

	encoded, err := json.Marshal(pending)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `UPDATE canvases SET actions_pending = $2 WHERE id = $1`, id, encoded)
	if err != nil {
		return fmt.Errorf("append human action: %w", err)
	}

	return tx.Commit()
}

// actionNodeID extracts the "nodeId" field a human-action's data payload is
// expected to carry (add/edit/move/delete/fog/unfog/star all act on one
// node). Returns "" if the payload doesn't have one — callers that only use
// this for the move-aggregation optimization treat that as "don't
// aggregate" rather than an error.
func actionNodeID(data json.RawMessage) string {
	var probe struct {
		NodeID string `json:"nodeId"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return probe.NodeID
}
