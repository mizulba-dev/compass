package api

import "encoding/json"

var colorPalette = []string{"#F0731F", "#E8489B", "#D9A514", "#57A345", "#2D9BB5", "#7A5AF8"}

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
	Kind string `json:"kind,omitempty"` // "normal" | "question"
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
		if in.Kind == "question" {
			kind = "question"
		}
		x, y, dir := childPos(m, parent)
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
	return ""
}

// updateNodeRequest backs POST /api/canvas/:id/node (update_node, the
// agent-facing WebMCP tool). Only text and clearing fog: there is no field
// here that could move, delete, or (re-)fog a node — those are human-only
// (see nodeHumanRequest), and the decode surface enforces it, not just the
// tool's inputSchema/description.
type updateNodeRequest struct {
	tokenEnvelope
	ID    string  `json:"id"`
	Text  *string `json:"text,omitempty"`
	Unfog bool    `json:"unfog,omitempty"`
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
	if req.Text != nil {
		node.Text = *req.Text
	}
	if req.Unfog {
		node.Fog = false
	}
	return ""
}

// nodeHumanRequest backs POST /api/canvas/:id/node/human: every node edit
// reserved for the human (move, delete, toggle fog either way, star). No
// WebMCP tool ever calls this endpoint.
type nodeHumanRequest struct {
	tokenEnvelope
	ID     string   `json:"id"`
	Text   *string  `json:"text,omitempty"`
	X      *float64 `json:"x,omitempty"`
	Y      *float64 `json:"y,omitempty"`
	Fog    *bool    `json:"fog,omitempty"`
	Star   *bool    `json:"star,omitempty"`
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
		return ""
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
	return ""
}

// harvestRequest backs POST /api/canvas/:id/harvest (harvest, the
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
