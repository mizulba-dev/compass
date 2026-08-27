import type { Goal, Task } from '../types';

export function GoalCard({ goal, tasks }: { goal: Goal | null; tasks: Task[] }) {
  const total = tasks.length;
  const done = tasks.filter((t) => t.done).length;
  const pct = total === 0 ? 0 : Math.round((done / total) * 100);

  return (
    <section className="card">
      <h2>Goal</h2>
      {goal ? (
        <>
          <p className="goal-title">{goal.title}</p>
          <p className="goal-meta">
            {goal.deadline ? `Due ${goal.deadline}` : 'No deadline set'}
            {goal.why ? ` · ${goal.why}` : ''}
          </p>
          {total > 0 && (
            <div className="progress-bar" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
              <div className="progress-bar-fill" style={{ width: `${pct}%` }} />
            </div>
          )}
        </>
      ) : (
        <p className="empty-hint">No goal yet — tell the agent what you're working toward.</p>
      )}
    </section>
  );
}
