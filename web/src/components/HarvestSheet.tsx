import { Square, CheckSquare, X } from 'lucide-react';
import type { Harvest, MapNode } from '../types';

interface HarvestSheetProps {
  harvest: Harvest | null;
  nodes: MapNode[];
  open: boolean;
  onClose: () => void;
  /** Toggles a task node's done flag from its checkbox in the plan sheet — the same write path a node's own on-canvas checkbox uses, so both stay in sync. */
  onToggleDone: (nodeId: string, done: boolean) => void;
}

export function HarvestSheet({ harvest, nodes, open, onClose, onToggleDone }: HarvestSheetProps) {
  const taskNodes = nodes.filter((n) => n.kind === 'task');
  return (
    <div className={`sheet${open ? ' open' : ''}`} role="dialog" aria-label="Plan" data-testid="harvest-sheet">
      <button type="button" className="close" aria-label="Close" onClick={onClose}>
        <X size={16} />
      </button>
      <h2>Plan</h2>
      <p className="sub">A plan drawn from the map. The map itself stays as is.</p>
      {harvest ? (
        <>
          <h3>GOAL</h3>
          <ul>
            <li>{harvest.goal}</li>
          </ul>
          <h3>PREMISES — what you established</h3>
          <ul>
            {harvest.premises.map((p, i) => (
              <li key={i}>{p}</li>
            ))}
          </ul>
        </>
      ) : (
        <p className="sub">Ask the agent to fold the map into a plan to add a goal summary.</p>
      )}
      <h3>NEXT TASKS</h3>
      <ul>
        {taskNodes.length > 0
          ? taskNodes.map((n) => (
              <li key={n.id} className={`task-item${n.done ? ' done' : ''}`}>
                <button
                  type="button"
                  className="task-item-check"
                  aria-label={n.done ? 'Mark not done' : 'Mark done'}
                  data-testid="plan-task-check"
                  onClick={() => onToggleDone(n.id, !n.done)}
                >
                  {n.done ? <CheckSquare size={14} /> : <Square size={14} />}
                </button>
                {n.text}
              </li>
            ))
          : // Maps whose plan predates task nodes have their tasks only in
            // the harvest snapshot — fall back to it rather than showing
            // an empty list.
            harvest?.tasks.map((t, i) => <li key={i}>{t}</li>)}
      </ul>
    </div>
  );
}
