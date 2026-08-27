/**
 * Single source of truth for every WebMCP tool description. This is the
 * primary agent-behavior tuning surface for the project (see the product
 * spec §5 / §7): edit these strings, not the tool wiring in register.ts,
 * when adjusting how the agent behaves.
 */
export const descriptions = {
  read_canvas:
    'Reads the full Compass canvas: goal, current situation, gaps, plan (tasks), ' +
    'policies, and past session summaries. Call this before any other Compass tool, ' +
    'and again whenever a write is rejected for a stale state token. The policies array ' +
    'holds decisions the human has already made — follow them in every suggestion you ' +
    'make. If sessions is non-empty, greet the human as a continuation of your last ' +
    'conversation rather than starting over. The response also carries humanActions: ' +
    'edits the human made directly on the page (deleting, reordering, or checking off ' +
    'tasks) since your last tool call. Read them and react — usually by proposing to ' +
    'record the underlying judgment as a policy with add_policy, not by silently ' +
    'reverting the edit.',
  set_goal:
    'Sets or updates the canvas goal (title, optional deadline, optional "why"). If the ' +
    "goal the human describes is vague, ask them for a deadline and their motivation " +
    'before calling this — a goal without a "why" is hard to plan against.',
  set_current:
    "Records the human's own account of where they currently stand, verbatim. Do not " +
    'evaluate, judge, or rephrase their self-report — store it as they said it.',
  upsert_gaps:
    'Adds newly identified gaps between the goal and the current situation, and/or marks ' +
    'existing gaps resolved by id. You may use outside research (e.g. submission ' +
    'requirements, prerequisites) to surface gaps the human has not mentioned.',
  plan_tasks:
    'Creates or reorganizes the task plan. Pass every task you want on the canvas: an ' +
    'existing task (identified by its id, from the last read_canvas result) keeps the ' +
    'order the human has already arranged for it no matter where you place it in this ' +
    'call — only tasks without a matching id are treated as new and appended. Never try ' +
    'to move an existing task ahead of another; the human owns task order.',
  update_tasks:
    'Updates the text and/or done state of existing tasks by id. This does not change ' +
    'task order or delete tasks — those are reserved for the human, editing directly on ' +
    'the canvas.',
  add_policy:
    "Records a standing policy the human's own actions or words have established (e.g. " +
    'from a canvas edit surfaced via humanActions, or something they said in chat). Only ' +
    'record policies you can trace to an actual human decision — never invent a policy ' +
    'on your own judgment. Once recorded, follow it in future plans until the human ' +
    'changes it.',
} as const;
