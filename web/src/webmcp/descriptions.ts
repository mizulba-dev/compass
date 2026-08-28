/**
 * Single source of truth for every WebMCP tool description. This is the
 * primary agent-behavior tuning surface for the project: edit these
 * strings, not the tool wiring in register.ts, when adjusting how the
 * agent behaves.
 */
export const descriptions = {
  read_map:
    'Reads the full fog map: title, every node (id, text, position, parent, fog, star, ' +
    'kind, origin), and any saved harvest. Call this before any other map tool, and ' +
    'again whenever a write is rejected for a stale state token. Nodes with fog: true ' +
    'are things the human has marked "I don\'t know — let\'s discuss this". Treat all ' +
    'fog-marked nodes as the consultation agenda: when the human refers to the marks in ' +
    'any wording ("the marked ones", "マークの件", "わからないの件"), address those nodes ' +
    '— explain context, offer options, or grow clarifying branches, then clear the fog ' +
    'with update_node once resolved. Clearing fog is your highest-priority move. The ' +
    'response also carries humanActions: edits the human made directly on the canvas ' +
    '(adding, editing, moving, deleting, fogging/unfogging, or starring a node) since ' +
    'your last tool call. Read them and react to what they reveal about the human\'s ' +
    'judgment — never silently revert or route around a human edit; their placement and ' +
    'deletions are final.',
  add_nodes:
    'Adds up to 3 new nodes as children of an existing node (pass its id as parent — get ' +
    'ids from read_map). Never add more than 3 in one call, even if more come to mind — ' +
    'make a second call instead; a map that fills up in one shot stops feeling like a ' +
    'conversation. Prefer branching off a node the human just placed or a fogged node ' +
    'over an already-crowded branch. Set kind: "question" for a node that asks the human ' +
    'something rather than stating a step.',
  update_node:
    'Edits an existing node\'s text, and/or clears its fog (unfog: true) once you\'ve ' +
    'given the human enough to resolve it — usually right after add_nodes grew that ' +
    'fogged node some children. This tool cannot move or re-fog a node one at a time — ' +
    'position is the human\'s to change freely; the only way you may reposition anything ' +
    'is a human-requested bulk tidy via arrange_nodes. To remove a node instead, use ' +
    'remove_node.',
  remove_node:
    'Removes a node and its entire subtree. The root cannot be removed. Prefer removing ' +
    'your own (agent-added) nodes when tidying up a branch that turned out to be a dead ' +
    'end; ask the human in conversation before removing nodes they placed — their ' +
    'judgment about what stays on the map is not yours to override unilaterally.',
  arrange_nodes:
    'Repositions nodes in bulk, tidying up the map\'s layout. Only use this when the ' +
    'human explicitly asks you to tidy, arrange, or organize the map (e.g. "can you ' +
    'clean this up?") — never rearrange nodes on your own initiative just because the ' +
    'layout looks messy to you; a map you\'re free to reflow at will stops feeling like ' +
    'the human\'s own space. Read the map first, then propose coordinates that keep each ' +
    'branch\'s nodes spatially grouped together (not scattered) and close to their parent. ' +
    'You don\'t need to avoid overlaps yourself — send your best-guess coordinates and the ' +
    'server corrects them so nothing ends up on top of anything else.',
  harvest:
    'Folds the grown map into a plan: one goal, the premises the human has established ' +
    '(their own nodes, not yours), and the next concrete tasks (your leaf nodes with no ' +
    'children of their own). Call this ONLY when the human explicitly asks to fold the ' +
    'map into a plan, wrap up, or finish — for example "let\'s turn this into a plan" or ' +
    '"I think we\'re done here". Never call it just because you added some nodes, and ' +
    'never call it to check what a harvest would look like; opening the harvest view is ' +
    'the human\'s own action on the page. The map itself is left untouched; harvest is a ' +
    'snapshot, not a mutation.',
} as const;
