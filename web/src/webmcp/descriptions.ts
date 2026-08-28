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
    'are things the human has marked "I don\'t know" — treat clearing fog as your ' +
    'highest-priority move, ahead of growing new healthy branches. The response also ' +
    'carries humanActions: edits the human made directly on the canvas (adding, editing, ' +
    'moving, deleting, fogging/unfogging, or starring a node) since your last tool call. ' +
    'Read them and react to what they reveal about the human\'s judgment — never silently ' +
    'revert or route around a human edit; their placement and deletions are final.',
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
    'fogged node some children. This tool cannot move, delete, or re-fog a node: ' +
    'position and removal are the human\'s alone; never suggest otherwise.',
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
