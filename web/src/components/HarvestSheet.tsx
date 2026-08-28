import { X } from 'lucide-react';
import type { Harvest, MapNode } from '../types';

interface HarvestSheetProps {
  harvest: Harvest | null;
  nodes: MapNode[];
  open: boolean;
  onClose: () => void;
}

export function HarvestSheet({ harvest, nodes, open, onClose }: HarvestSheetProps) {
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
          <h3>NEXT TASKS</h3>
          <ul>
            {taskNodes.length > 0
              ? taskNodes.map((n) => (
                  <li key={n.id} className={n.done ? 'done' : undefined}>
                    {n.text}
                  </li>
                ))
              : // Maps whose plan predates task nodes have their tasks only in
                // the harvest snapshot — fall back to it rather than showing
                // an empty list.
                harvest.tasks.map((t, i) => <li key={i}>{t}</li>)}
          </ul>
        </>
      ) : (
        <p className="sub">No plan yet — once the map has grown, ask the agent to fold it into one.</p>
      )}
    </div>
  );
}
