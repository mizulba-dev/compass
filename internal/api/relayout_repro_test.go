package api

import (
	"encoding/json"
	"os"
	"testing"
)

func loadReproMap(t *testing.T) MapData {
	t.Helper()
	raw, err := os.ReadFile("testdata/repro-map.json")
	if err != nil {
		t.Fatalf("read repro-map.json: %v", err)
	}
	var m MapData
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode repro-map.json: %v", err)
	}
	return m
}

// assertRelayoutInvariants is the standing falsification harness for two
// structural invariants any relayout output must satisfy, regardless of
// input shape:
//  1. every non-leaf node's y sits within the [min, max] range of its own
//     children's y — it's always the average of them, so this can only be
//     violated by something AFTER the tidy-tree walk moving it
//     independently (e.g. the final collision-avoidance pass reacting to a
//     spuriously-undersized sibling band and shoving one node far away from
//     the rest of its own chain — see TestReproMapFiebxhStaysWithItsFamily).
//  2. every non-root node's Dir matches its actual parent's Dir (a node
//     only picks its own left/right side when its parent is the root;
//     every deeper descendant inherits it) — a node landing in the wrong
//     side's row-band accounting would show up here.
func assertRelayoutInvariants(t *testing.T, m *MapData) {
	t.Helper()
	byID := map[string]*Node{}
	for i := range m.Nodes {
		byID[m.Nodes[i].ID] = &m.Nodes[i]
	}

	for i := range m.Nodes {
		n := &m.Nodes[i]
		kids := childrenOf(m, n.ID)
		if len(kids) == 0 {
			continue
		}
		minY, maxY := kids[0].Y, kids[0].Y
		for _, k := range kids[1:] {
			if k.Y < minY {
				minY = k.Y
			}
			if k.Y > maxY {
				maxY = k.Y
			}
		}
		if n.Y < minY || n.Y > maxY {
			t.Errorf("invariant violated: node %s (y=%v) falls outside its children's y-range [%v, %v]", n.ID, n.Y, minY, maxY)
		}
	}

	for i := range m.Nodes {
		n := &m.Nodes[i]
		if n.Root || n.Parent == nil {
			continue
		}
		parent := byID[*n.Parent]
		if parent.Root {
			continue // top branches pick their own side independently
		}
		if n.Dir != parent.Dir {
			t.Errorf("invariant violated: node %s dir=%v does not match its parent %s dir=%v", n.ID, n.Dir, parent.ID, parent.Dir)
		}
	}
}

// TestReproMapFiebxhStaysWithItsFamily is the standing falsification probe
// for a real production bug: after Tidy, a question node (id fiebxh4xj4,
// long text) partway down a single-child chain (dkv6v6jlye -> fiebxh4xj4 ->
// t5vddfu47i -> 6fwrq2krtu -> p3nwlj5tm4) landed at y=1020 while every
// other node in the same chain sat at y=0. Root cause: subtreeHeight only
// summed LEAF heights, ignoring however tall an internal node (like
// fiebxh4xj4, whose own text wraps to multiple lines) actually renders —
// so the next top branch's band ("調査候補..." / lp5xob5sla) started too
// early and collided with fiebxh4xj4, and the final avoidCollisionsExcluding
// pass "fixed" that by shoving fiebxh4xj4 1020px away from the rest of its
// own chain. Reproduces the exact production data (internal/api/testdata/
// repro-map.json, exported from the live map that triggered the report).
func TestReproMapFiebxhStaysWithItsFamily(t *testing.T) {
	m := loadReproMap(t)
	relayout(&m)

	byID := map[string]*Node{}
	for i := range m.Nodes {
		byID[m.Nodes[i].ID] = &m.Nodes[i]
	}
	fiebxh := byID["fiebxh4xj4"]
	if fiebxh == nil {
		t.Fatal("expected fiebxh4xj4 in the repro map")
	}
	child := byID["t5vddfu47i"] // fiebxh4xj4's only child
	if child == nil {
		t.Fatal("expected t5vddfu47i (fiebxh4xj4's child) in the repro map")
	}
	if fiebxh.Y != child.Y {
		t.Fatalf("fiebxh4xj4 separated from its own chain: fiebxh4xj4.y=%v, its child t5vddfu47i.y=%v", fiebxh.Y, child.Y)
	}

	assertRelayoutInvariants(t, &m)
}

// TestRelayoutInvariantsOnGeneratedTree re-runs the same two structural
// invariants against a freshly built tree (not the repro data) — 2 top
// branches on the SAME side (left, so they share one row-band sequence),
// the first being a single-child chain with a long-text (multi-line)
// internal node partway down it, at the same depth as the second branch's
// own child — so the same class of bug (an internal node's own height
// ignored when reserving the NEXT same-side sibling's band) reproduces
// here on its own shape, not just against the one production snapshot. A
// single lone branch on a side, or a long node buried at a depth with no
// same-side sibling nearby in x, wouldn't actually collide even with the
// bug present — the geometry has to line up, same as it did in production.
func TestRelayoutInvariantsOnGeneratedTree(t *testing.T) {
	longText := "絶対に譲れない条件は何ですか？（立地・広さ・性能・間取りなど、思いつく限り書き出してください）"
	root := Node{ID: "root", Text: "テーマ", Root: true, Kind: "normal", Origin: "human"}
	mk := func(id, parent, text string, dir int) Node {
		p := parent
		return Node{ID: id, Parent: &p, Text: text, Dir: dir, Kind: "normal", Origin: "agent"}
	}
	m := MapData{Nodes: []Node{
		root,
		// Two top branches on the SAME side (left) with similar short text,
		// so their own x columns (and their depth-2 children's) land close
		// together — the same-depth proximity that let the real bug's two
		// unrelated nodes overlap despite belonging to different branches.
		mk("branchA", "root", "branch A", -1),
		mk("branchB", "root", "branch B", -1),
		// branchA's only child is the tall (multi-line) node, followed by
		// a short single-child tail — mirrors fiebxh4xj4 sitting partway
		// down dkv6v6jlye's chain in the repro data.
		mk("tall", "branchA", longText, -1),
		mk("tail", "tall", "tail short leaf", -1),
		// branchB's own child sits at the SAME depth as "tall" — if
		// subtreeHeight under-reserves branchA's band, this is what ends
		// up colliding with "tall".
		mk("b1", "branchB", "branch B child", -1),
	}}
	relayout(&m)
	assertRelayoutInvariants(t, &m)
}
