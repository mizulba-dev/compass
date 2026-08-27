import type { CurrentState } from '../types';

export function CurrentCard({ current }: { current: CurrentState | null }) {
  return (
    <section className="card">
      <h2>Where you are</h2>
      {current ? (
        <p className="current-summary">{current.summary}</p>
      ) : (
        <p className="empty-hint">Not recorded yet — tell the agent where things stand.</p>
      )}
    </section>
  );
}
