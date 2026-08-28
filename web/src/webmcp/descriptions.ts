/**
 * Single source of truth for every WebMCP tool description. This is the
 * primary agent-behavior tuning surface for the project: edit these
 * strings, not the tool wiring in register.ts, when adjusting how the
 * agent behaves.
 */
export const descriptions = {
  read_map:
    'Reads the full fog map: title, every node (id, text, position, parent, fog, star, ' +
    'done, kind, origin), and any saved harvest. A node\'s done flag only means anything ' +
    'when kind is "task" — it is true once that concrete step is finished. Call this ' +
    'before any other map tool, and ' +
    'again whenever a write is rejected for a stale state token. Nodes with fog: true ' +
    'are things the human has marked "I don\'t know — let\'s discuss this". Treat all ' +
    'fog-marked nodes as the consultation agenda: when the human refers to the marks in ' +
    'any wording, in any language (e.g. "the marked ones", or "マークの件"), address those nodes ' +
    '— explain context, offer options, or grow clarifying branches, then clear the fog ' +
    'with update_node once resolved. Clearing fog is your highest-priority move. The ' +
    'response also carries humanActions: edits the human made directly on the canvas ' +
    '(adding, editing, moving, deleting, fogging/unfogging, or starring a node) since ' +
    'your last tool call. Read them and react to what they reveal about the human\'s ' +
    'judgment — never silently revert or route around a human edit; their placement and ' +
    'deletions are final. On a fresh map (root only, no other nodes yet), do NOT start ' +
    'with step branches: first add_nodes 1–3 kind: "question" nodes asking what you need ' +
    'to know (constraints, deadline, biggest worry), and wait for the human\'s answers ' +
    '(as their own nodes, or in chat) before proposing any concrete steps.',
  add_nodes:
    'Adds up to 3 new nodes as children of an existing node (pass its id as parent — get ' +
    'ids from read_map). Never add more than 3 in one call, even if more come to mind — ' +
    'make a second call instead; a map that fills up in one shot stops feeling like a ' +
    'conversation. Prefer branching off a node the human just placed or a fogged node ' +
    'over an already-crowded branch. On a fresh map (root only), your first add_nodes ' +
    'call should be 1–3 kind: "question" nodes probing what you need to know (constraints, ' +
    'deadline, biggest worry) — wait for the human\'s answers before proposing concrete ' +
    'steps, rather than jumping straight to a plan. Set kind: "task" for a concrete ' +
    'actionable step the human could do ("do X by Y"); "question" for a node that asks the ' +
    'human something; plain nodes for information or considerations.',
  update_node:
    'Edits an existing node\'s text, and/or clears its fog (unfog: true) once you\'ve ' +
    'given the human enough to resolve it — usually right after add_nodes grew that ' +
    'fogged node some children. Also accepts done: true/false to mark a task-kind node ' +
    'complete or reopen it — use this when the human says in conversation that something ' +
    'is finished (or that it isn\'t, after all); it is not yours to mark done on your own ' +
    'judgment. Convert a node\'s kind (e.g. kind: "task" when the human asks to turn ' +
    'steps into tasks, or "question"/"normal") — done resets to false whenever a node ' +
    'stops being a task, since a completion flag on a non-task node means nothing. This ' +
    'tool cannot move or re-fog a node one at a time — the server automatically retidies ' +
    'the whole map\'s layout after every add_nodes/remove_node call, so you never need to ' +
    'position anything yourself; arrange_nodes exists only for a specific ordering the ' +
    'human explicitly asks for. To remove a node instead, use remove_node.',
  remove_node:
    'Removes a node and its entire subtree. The root cannot be removed. Prefer removing ' +
    'your own (agent-added) nodes when tidying up a branch that turned out to be a dead ' +
    'end; ask the human in conversation before removing nodes they placed — their ' +
    'judgment about what stays on the map is not yours to override unilaterally.',
  arrange_nodes:
    'Repositions nodes in bulk to a SPECIFIC ordering. The server already retidies the ' +
    'whole map automatically after every add_nodes/remove_node call, so you never need ' +
    'this just to keep the layout clean — use it only when the human explicitly asks for ' +
    'a particular arrangement the automatic tidy wouldn\'t produce on its own (e.g. "put ' +
    'the most important ones at the top" or "group these three together"). Never call it ' +
    'on your own initiative just because the layout looks messy; the automatic tidy ' +
    'already handles that. Read the map first, then propose coordinates matching what the ' +
    'human asked for — you don\'t need to avoid overlaps yourself, the server corrects ' +
    'your coordinates so nothing ends up on top of anything else.',
  harvest:
    'Folds the grown map into a plan: one goal, the premises the human has established ' +
    '(their own nodes, not yours), and the next concrete tasks. Before calling this, make ' +
    'sure the actionable steps exist as kind: "task" nodes (convert with update_node if ' +
    'needed) — the plan view lists task nodes live, with their done state. Call this ONLY ' +
    'when the human explicitly asks to fold the ' +
    'map into a plan, wrap up, or finish — for example "let\'s turn this into a plan" or ' +
    '"I think we\'re done here". Never call it just because you added some nodes, and ' +
    'never call it to check what a harvest would look like; opening the harvest view is ' +
    'the human\'s own action on the page. When several task nodes have accumulated and the ' +
    'exploration is settling, proactively OFFER in conversation to fold the map into a ' +
    'plan — still call this tool only after the human agrees; never call it unprompted ' +
    'just because you offered. The map itself is left untouched; harvest is a snapshot, ' +
    'not a mutation.',
} as const;
