import type { Harvest } from '../types';

interface HarvestSheetProps {
  harvest: Harvest | null;
  open: boolean;
  onClose: () => void;
}

export function HarvestSheet({ harvest, open, onClose }: HarvestSheetProps) {
  return (
    <div className={`sheet${open ? ' open' : ''}`} role="dialog" aria-label="収穫" data-testid="harvest-sheet">
      <button type="button" className="close" aria-label="閉じる" onClick={onClose}>
        ✕
      </button>
      <h2>収穫 — 地図から畳んだ計画</h2>
      <p className="sub">各項目は地図のノード由来。地図はこのまま残ります。</p>
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
            {harvest.tasks.map((t, i) => (
              <li key={i}>{t}</li>
            ))}
          </ul>
        </>
      ) : (
        <p className="sub">まだ収穫はありません。地図が育ったら、エージェントが収穫します。</p>
      )}
    </div>
  );
}
