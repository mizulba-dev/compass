import type { Gap } from '../types';

export function GapChips({ gaps }: { gaps: Gap[] }) {
  return (
    <section className="card">
      <h2>Gaps</h2>
      {gaps.length === 0 ? (
        <p className="empty-hint">No gaps identified yet.</p>
      ) : (
        <div className="gap-list">
          {gaps.map((gap) => (
            <span key={gap.id} className={`gap-chip${gap.resolved ? ' resolved' : ''}`}>
              {gap.text}
            </span>
          ))}
        </div>
      )}
    </section>
  );
}
