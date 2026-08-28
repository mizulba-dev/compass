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
    <div className={`sheet${open ? ' open' : ''}`} role="dialog" aria-label="計画" data-testid="harvest-sheet">
      <button type="button" className="close" aria-label="閉じる" onClick={onClose}>
        <X size={16} />
      </button>
      <h2>計画</h2>
      <p className="sub">地図から抽出した計画です。地図はそのまま残ります。</p>
      {harvest ? (
        <>
          <h3>GOAL</h3>
          <ul>
            <li>{harvest.goal}</li>
          </ul>
          <h3>PREMISES — あなたが置いた前提</h3>
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
        <p className="sub">まだ計画はありません。地図が育ったら、エージェントが計画にまとめます。</p>
      )}
    </div>
  );
}
