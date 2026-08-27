import type { Task } from '../types';

interface PlanListProps {
  tasks: Task[];
  onToggle: (task: Task) => void;
  onDelete: (task: Task) => void;
  onMove: (task: Task, direction: 'up' | 'down') => void;
}

export function PlanList({ tasks, onToggle, onDelete, onMove }: PlanListProps) {
  const sorted = [...tasks].sort((a, b) => a.order - b.order);

  return (
    <section className="card">
      <h2>Plan</h2>
      {sorted.length === 0 ? (
        <p className="empty-hint">No tasks yet — the agent will propose a plan.</p>
      ) : (
        <ul className="task-list" data-testid="task-list">
          {sorted.map((task, i) => (
            <li key={task.id} className={`task-row${task.done ? ' done' : ''}`} data-testid="task-item">
              <input
                type="checkbox"
                checked={task.done}
                onChange={() => onToggle(task)}
                aria-label={`Mark "${task.text}" as ${task.done ? 'not done' : 'done'}`}
                data-testid="task-toggle"
              />
              <span className="task-text">
                {task.text}
                {task.day && <span className="task-day"> · {task.day}</span>}
              </span>
              <div className="task-actions">
                <button
                  type="button"
                  className="icon-btn"
                  onClick={() => onMove(task, 'up')}
                  disabled={i === 0}
                  aria-label={`Move "${task.text}" up`}
                  data-testid="task-move-up"
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="icon-btn"
                  onClick={() => onMove(task, 'down')}
                  disabled={i === sorted.length - 1}
                  aria-label={`Move "${task.text}" down`}
                  data-testid="task-move-down"
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="icon-btn danger"
                  onClick={() => onDelete(task)}
                  aria-label={`Delete "${task.text}"`}
                  data-testid="task-delete"
                >
                  ✕
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
