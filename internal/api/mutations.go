package api

import (
	"encoding/json"
	"math"
)

var colorPalette = []string{"#F0731F", "#E8489B", "#D9A514", "#57A345", "#2D9BB5", "#7A5AF8"}

func isValidNodeKind(k string) bool {
	return k == "normal" || k == "question" || k == "task"
}

func findNodeIndex(m *MapData, id string) int {
	for i := range m.Nodes {
		if m.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}

func findNode(m *MapData, id string) *Node {
	i := findNodeIndex(m, id)
	if i < 0 {
		return nil
	}
	return &m.Nodes[i]
}

func childrenOf(m *MapData, parentID string) []*Node {
	var out []*Node
	for i := range m.Nodes {
		if m.Nodes[i].Parent != nil && *m.Nodes[i].Parent == parentID {
			out = append(out, &m.Nodes[i])
		}
	}
	return out
}

// childPos mirrors the mock's childPos(): new children fan out to the
// right of their parent (root's children alternate left/right), stacked
// vertically among same-side siblings so they don't overlap.
func childPos(m *MapData, parent *Node) (x, y float64, dir int) {
	children := childrenOf(m, parent.ID)
	if parent.Root {
		if len(children)%2 == 0 {
			dir = 1
		} else {
			dir = -1
		}
	} else if parent.Dir != 0 {
		dir = parent.Dir
	} else {
		dir = 1
	}

	if dir > 0 {
		x = parent.X + 280
	} else {
		x = parent.X - 260
	}

	sameSide := 0
	for _, k := range children {
		if (k.X > parent.X) == (dir > 0) {
			sameSide++
		}
	}
	y = parent.Y - 20 + float64(sameSide)*64
	return x, y, dir
}

// colorFor mirrors the mock's color inheritance: a direct child of the root
// gets the next color in the palette (cycling by how many root-children
// already exist); any other node inherits its parent's branch color.
func colorFor(m *MapData, parent *Node) string {
	if parent.Root {
		count := len(childrenOf(m, parent.ID))
		return colorPalette[count%len(colorPalette)]
	}
	return parent.Color
}

// rect is an approximate node footprint, estimated from text length rather
// than a real layout pass (the server never renders anything) — good
// enough to keep newly added nodes from landing on top of others.
type rect struct{ x0, y0, x1, y1 float64 }

func estimateRect(x, y float64, text string) rect {
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
		lines = math.Ceil(raw / 260)
	}
	height := 40 + (lines-1)*22
	return rect{x0: x, y0: y, x1: x + width, y1: y + height}
}

func rectsOverlap(a, b rect) bool {
	return a.x0 < b.x1 && a.x1 > b.x0 && a.y0 < b.y1 && a.y1 > b.y0
}

// avoidCollisions nudges a newly proposed position straight down until its
// estimated footprint clears every existing node's — including nodes on
// other branches, not just siblings. It only ever moves the new node being
// placed; every other node keeps its current position for this call (any
// of them may still be repositioned moments later by relayout, since layout
// ownership belongs to the engine, not to whoever placed a node last).
func avoidCollisions(m *MapData, x, y float64, text string) (float64, float64) {
	return avoidCollisionsExcluding(m, "", x, y, text)
}

// avoidCollisionsExcluding is avoidCollisions for the arrange_nodes case: a
// node that already exists on the map is being repositioned, so it must not
// collide with itself at its old position — excludeID skips it from the
// obstacle set.
func avoidCollisionsExcluding(m *MapData, excludeID string, x, y float64, text string) (float64, float64) {
	for attempt := 0; attempt < 50; attempt++ {
		cand := estimateRect(x, y, text)
		conflict := false
		for i := range m.Nodes {
			if m.Nodes[i].ID == excludeID {
				continue
			}
			if rectsOverlap(cand, estimateRect(m.Nodes[i].X, m.Nodes[i].Y, m.Nodes[i].Text)) {
				conflict = true
				break
			}
		}
		if !conflict {
			return x, y
		}
		y += 60
	}
	return x, y
}

// layoutRowHeight and the two layoutDepthStep constants mirror childPos's
// own literals (64px row spacing, 280px right / 260px left per depth
// level) — relayout is a full-tree generalization of the same placement
// rule childPos already applies node-by-node at creation time.
const (
	layoutRowHeight      = 64.0
	layoutDepthStepRight = 280.0
	layoutDepthStepLeft  = 260.0
)

func findRootNode(m *MapData) *Node {
	for i := range m.Nodes {
		if m.Nodes[i].Root {
			return &m.Nodes[i]
		}
	}
	return nil
}

// subtreeLeafCount counts the leaves under node (a childless node counts as
// one leaf of itself). relayout uses this to reserve each top branch an
// exact, contiguous row band sized to its own shape, computed up front
// rather than left to emerge from call order — so two branches on the same
// side can never end up with interleaved rows regardless of how many
// separate add_nodes/remove_node turns grew them.
func subtreeLeafCount(m *MapData, node *Node) int {
	kids := childrenOf(m, node.ID)
	if len(kids) == 0 {
		return 1
	}
	count := 0
	for _, k := range kids {
		count += subtreeLeafCount(m, k)
	}
	return count
}

// relayout re-tidies the whole map after any structural change (a node
// added or removed, by either the agent or the human) — layout ownership
// belongs to the engine, not the human: a drag is honored until the next
// structural change, at which point it's absorbed back into the tidy
// layout like everything else. x is purely parent-relative — the same
// +280/-260 step childPos uses for a single new node — so every child stays
// directly adjacent to its actual parent. y is assigned per top branch
// (root's direct children): each branch reserves an exact contiguous row
// band (see subtreeLeafCount) on its side (left/right), stacked below the
// previous same-side branch's band, so no two top branches' rows ever
// interleave or their edges cross. Within a band, a leaf takes the next row
// (64px apart) and an internal node centers over its own children's y. A
// final pass nudges any node down out of any remaining overlap — a safety
// net for node footprints wider than the standard row spacing (long-
// wrapped text) — mirroring the single-node guard in
// avoidCollisionsExcluding.
func relayout(m *MapData) {
	root := findRootNode(m)
	if root == nil {
		return
	}

	var assignX func(parent *Node)
	assignX = func(parent *Node) {
		for _, child := range childrenOf(m, parent.ID) {
			dir := child.Dir
			if dir == 0 {
				dir = 1
			}
			if dir > 0 {
				child.X = parent.X + layoutDepthStepRight
			} else {
				child.X = parent.X - layoutDepthStepLeft
			}
			assignX(child)
		}
	}
	assignX(root)

	var assignY func(node *Node, rowStart int) float64
	assignY = func(node *Node, rowStart int) float64 {
		kids := childrenOf(m, node.ID)
		if len(kids) == 0 {
			node.Y = root.Y + float64(rowStart)*layoutRowHeight
			return node.Y
		}
		sum := 0.0
		cursor := rowStart
		for _, child := range kids {
			sum += assignY(child, cursor)
			cursor += subtreeLeafCount(m, child)
		}
		node.Y = sum / float64(len(kids))
		return node.Y
	}

	nextRowForSide := map[int]int{}
	for _, branch := range childrenOf(m, root.ID) {
		dir := branch.Dir
		if dir == 0 {
			dir = 1
		}
		start := nextRowForSide[dir]
		assignY(branch, start)
		nextRowForSide[dir] = start + subtreeLeafCount(m, branch)
	}

	for i := range m.Nodes {
		n := &m.Nodes[i]
		if n.Root {
			continue
		}
		n.X, n.Y = avoidCollisionsExcluding(m, n.ID, n.X, n.Y, n.Text)
	}
}

func removeSubtree(m *MapData, id string) {
	toRemove := map[string]bool{id: true}
	// Repeatedly sweep for children of anything already marked, since a
	// child can appear before or after its parent in the slice.
	changed := true
	for changed {
		changed = false
		for i := range m.Nodes {
			n := &m.Nodes[i]
			if n.Parent != nil && toRemove[*n.Parent] && !toRemove[n.ID] {
				toRemove[n.ID] = true
				changed = true
			}
		}
	}
	kept := m.Nodes[:0]
	for _, n := range m.Nodes {
		if !toRemove[n.ID] {
			kept = append(kept, n)
		}
	}
	m.Nodes = kept
}

// addNodesRequest backs POST /api/canvas/:id/nodes (add_nodes, the
// agent-facing WebMCP tool). At most 3 nodes per call — a structural limit
// against dumping a whole map in one shot — and every node needs an
// existing parent: the human places the map's first (root) node, an agent
// can only ever branch off something that already exists.
type addNodesRequest struct {
	tokenEnvelope
	Parent string         `json:"parent"`
	Nodes  []addNodeInput `json:"nodes"`
}

type addNodeInput struct {
	Text string `json:"text"`
	Kind string `json:"kind,omitempty"` // "normal" | "question" | "task"
}

func applyAddNodes(m *MapData, body json.RawMessage) string {
	var req addNodesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if len(req.Nodes) == 0 {
		return "nodes must have at least 1 item"
	}
	if len(req.Nodes) > 3 {
		return "nodes must have at most 3 items per call — add more in a follow-up call"
	}
	parent := findNode(m, req.Parent)
	if parent == nil {
		return "parent node not found: call read_map first and pass an existing node id as parent"
	}

	for _, in := range req.Nodes {
		if in.Text == "" {
			continue
		}
		kind := "normal"
		if in.Kind == "question" || in.Kind == "task" {
			kind = in.Kind
		}
		x, y, dir := childPos(m, parent)
		x, y = avoidCollisions(m, x, y, in.Text)
		parentID := parent.ID
		m.Nodes = append(m.Nodes, Node{
			ID:        newLocalID(),
			Text:      in.Text,
			X:         x,
			Y:         y,
			Parent:    &parentID,
			Dir:       dir,
			Color:     colorFor(m, parent),
			Kind:      kind,
			Origin:    "agent",
			CreatedAt: nowISO(),
		})
	}
	relayout(m)
	return ""
}

// addNodeHumanRequest backs POST /api/canvas/:id/nodes/human: a single
// freely-placed node, added directly by the human on the canvas (double
// click, or the + toolbar button). No count limit — humans can add as
// freely as the mock's canvas allows. The very first node ever placed
// becomes the map's root.
type addNodeHumanRequest struct {
	tokenEnvelope
	Text   string  `json:"text"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Parent *string `json:"parent,omitempty"`
}

func applyAddNodeHuman(m *MapData, body json.RawMessage) string {
	var req addNodeHumanRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	node := Node{
		ID:        newLocalID(),
		Text:      req.Text,
		X:         req.X,
		Y:         req.Y,
		Kind:      "normal",
		Origin:    "human",
		CreatedAt: nowISO(),
	}
	if req.Parent != nil && *req.Parent != "" {
		parent := findNode(m, *req.Parent)
		if parent == nil {
			return "parent node not found"
		}
		parentID := parent.ID
		node.Parent = &parentID
		node.Color = colorFor(m, parent)
		if req.X < parent.X {
			node.Dir = -1
		} else {
			node.Dir = 1
		}
	} else if len(m.Nodes) == 0 {
		node.Root = true
	}
	m.Nodes = append(m.Nodes, node)
	relayout(m)
	return ""
}

// updateNodeRequest backs POST /api/canvas/:id/node (update_node, the
// agent-facing WebMCP tool). Text, clearing fog, toggling a task's done
// flag, and converting a node's kind — there is no field here that could
// move, delete, or (re-)fog a node — those are human-only (see
// nodeHumanRequest), and the decode surface enforces it, not just the
// tool's inputSchema/description.
type updateNodeRequest struct {
	tokenEnvelope
	ID    string  `json:"id"`
	Text  *string `json:"text,omitempty"`
	Unfog bool    `json:"unfog,omitempty"`
	Done  *bool   `json:"done,omitempty"`
	Kind  *string `json:"kind,omitempty"`
}

func applyUpdateNode(m *MapData, body json.RawMessage) string {
	var req updateNodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	node := findNode(m, req.ID)
	if node == nil {
		return "node not found"
	}
	if req.Kind != nil && !isValidNodeKind(*req.Kind) {
		return "invalid kind: must be normal, question, or task"
	}
	if req.Text != nil {
		node.Text = *req.Text
	}
	if req.Unfog {
		node.Fog = false
	}
	if req.Kind != nil {
		node.Kind = *req.Kind
		// A node that stops being a task no longer carries a meaningful
		// done state — reset it so re-converting to task later starts
		// fresh rather than resurrecting a stale completion flag.
		if node.Kind != "task" {
			node.Done = false
		}
	}
	if req.Done != nil {
		node.Done = *req.Done
	}
	return ""
}

// removeNodeRequest backs POST /api/canvas/:id/node/remove (remove_node,
// the agent-facing WebMCP tool). Removes a node and its entire subtree —
// the same removal semantics as the human's delete — but the root is
// off-limits: an agent can prune branches it grew, never erase the map's
// starting point.
type removeNodeRequest struct {
	tokenEnvelope
	ID string `json:"id"`
}

func applyRemoveNode(m *MapData, body json.RawMessage) string {
	var req removeNodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	node := findNode(m, req.ID)
	if node == nil {
		return "node not found"
	}
	if node.Root {
		return "the root node cannot be removed"
	}
	removeSubtree(m, req.ID)
	relayout(m)
	return ""
}

// arrangeNodesRequest backs POST /api/canvas/:id/nodes/arrange
// (arrange_nodes, the agent-facing WebMCP tool): a bulk reposition, used
// when the human explicitly asks to tidy up the map. All-or-nothing: an
// unknown id anywhere in the batch rejects the whole call before any
// position changes.
type arrangeNodesRequest struct {
	tokenEnvelope
	Moves []arrangeMoveInput `json:"moves"`
}

type arrangeMoveInput struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

func applyArrangeNodes(m *MapData, body json.RawMessage) string {
	var req arrangeNodesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if len(req.Moves) == 0 {
		return "moves must have at least 1 item"
	}
	for _, mv := range req.Moves {
		if findNode(m, mv.ID) == nil {
			return "node not found: " + mv.ID
		}
	}
	// Each move is corrected against collisions before it's applied, using
	// the map's current state — including any earlier moves already
	// applied in this same batch — so a sloppy set of AI-proposed
	// coordinates still lands with zero overlap.
	for _, mv := range req.Moves {
		node := findNode(m, mv.ID)
		x, y := avoidCollisionsExcluding(m, mv.ID, mv.X, mv.Y, node.Text)
		node.X = x
		node.Y = y
	}
	return ""
}

// nodeHumanRequest backs POST /api/canvas/:id/node/human: every node edit
// reserved for the human (move, delete, toggle fog either way, star, and
// converting a node's kind — e.g. the right-click "タスクにする" toggle). No
// WebMCP tool ever calls this endpoint.
type nodeHumanRequest struct {
	tokenEnvelope
	ID     string   `json:"id"`
	Text   *string  `json:"text,omitempty"`
	X      *float64 `json:"x,omitempty"`
	Y      *float64 `json:"y,omitempty"`
	Fog    *bool    `json:"fog,omitempty"`
	Star   *bool    `json:"star,omitempty"`
	Done   *bool    `json:"done,omitempty"`
	Kind   *string  `json:"kind,omitempty"`
	Delete bool     `json:"delete,omitempty"`
}

func applyNodeHuman(m *MapData, body json.RawMessage) string {
	var req nodeHumanRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if findNode(m, req.ID) == nil {
		return "node not found"
	}
	if req.Delete {
		removeSubtree(m, req.ID)
		relayout(m)
		return ""
	}
	if req.Kind != nil && !isValidNodeKind(*req.Kind) {
		return "invalid kind: must be normal, question, or task"
	}
	node := findNode(m, req.ID)
	if req.Text != nil {
		node.Text = *req.Text
	}
	if req.X != nil {
		node.X = *req.X
	}
	if req.Y != nil {
		node.Y = *req.Y
	}
	if req.Fog != nil {
		node.Fog = *req.Fog
	}
	if req.Star != nil {
		node.Star = *req.Star
	}
	if req.Kind != nil {
		node.Kind = *req.Kind
		if node.Kind != "task" {
			node.Done = false
		}
	}
	if req.Done != nil {
		node.Done = *req.Done
	}
	return ""
}

// harvestRequest backs POST /api/canvas/:id/harvest (harvest, the
// applyTidy backs POST /api/canvas/:id/tidy: an immediate, human-triggered
// relayout. Since relayout only fires automatically on a structural change
// (a node added or removed), a drag with no such follow-up leaves the map
// looking untidy until one happens — this endpoint is the manual "reset
// now" for that gap. No WebMCP tool calls this; it's not recorded as a
// humanAction, since it doesn't reveal anything about the human's judgment
// the agent needs to react to.
func applyTidy(m *MapData, _ json.RawMessage) string {
	relayout(m)
	return ""
}

// agent-facing WebMCP tool): folds the grown map into a plan. The map
// itself is untouched — harvest is a summary, not a mutation of nodes.
type harvestRequest struct {
	tokenEnvelope
	Goal     string   `json:"goal"`
	Premises []string `json:"premises"`
	Tasks    []string `json:"tasks"`
}

func applyHarvest(m *MapData, body json.RawMessage) string {
	var req harvestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if req.Goal == "" {
		return "goal is required"
	}
	premises := req.Premises
	if premises == nil {
		premises = []string{}
	}
	tasks := req.Tasks
	if tasks == nil {
		tasks = []string{}
	}
	m.Harvest = &Harvest{Goal: req.Goal, Premises: premises, Tasks: tasks}
	return ""
}
