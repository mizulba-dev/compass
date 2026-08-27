import { useEffect, useRef, useState } from 'react';
import type { Canvas, Task } from './types';
import { createCanvas, getCanvas, updateTasks, editTasksHuman, recordHumanAction, StaleTokenError } from './api';
import { subscribeLive } from './live';
import { registerWebMCPTools } from './webmcp/register';
import { GoalCard } from './components/GoalCard';
import { CurrentCard } from './components/CurrentCard';
import { GapChips } from './components/GapChips';
import { PlanList } from './components/PlanList';
import { PolicyList } from './components/PolicyList';

function canvasIdFromPath(): string | null {
  const match = window.location.pathname.match(/^\/c\/([^/]+)$/);
  return match ? match[1] : null;
}

function App() {
  const [id, setId] = useState<string | null>(canvasIdFromPath());
  const [canvas, setCanvas] = useState<Canvas | null>(null);
  const [connected, setConnected] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const readTokenRef = useRef('');

  // Bootstrap: create a new canvas and adopt its URL as the share link when
  // none is present in the path.
  useEffect(() => {
    if (id) return;
    let cancelled = false;
    createCanvas().then(({ id: newId }) => {
      if (cancelled) return;
      window.history.replaceState(null, '', `/c/${newId}`);
      setId(newId);
    });
    return () => {
      cancelled = true;
    };
  }, [id]);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;

    getCanvas(id).then((c) => {
      if (!cancelled) {
        setCanvas(c);
        readTokenRef.current = c.readToken;
      }
    });

    const unsubscribe = subscribeLive(id, (c) => {
      setCanvas(c);
      readTokenRef.current = c.readToken;
      setConnected(true);
    });

    registerWebMCPTools({
      canvasId: id,
      onUpdate: (c) => {
        setCanvas(c);
        readTokenRef.current = c.readToken;
      },
    });

    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, [id]);

  /**
   * Runs a write, retrying once with a freshly re-read token if the human's
   * click raced a stale readToken (e.g. the agent wrote in between). On a
   * second failure, surfaces an English message on the status line instead
   * of failing silently.
   */
  const writeWithStaleTokenRetry = async (
    write: (readToken: string) => Promise<{ readToken: string }>,
  ): Promise<boolean> => {
    if (!id) return false;
    try {
      const { readToken } = await write(readTokenRef.current);
      readTokenRef.current = readToken;
      setErrorMessage(null);
      return true;
    } catch (e) {
      if (!(e instanceof StaleTokenError)) {
        setErrorMessage(e instanceof Error ? e.message : 'Something went wrong. Please try again.');
        return false;
      }
      try {
        const fresh = await getCanvas(id);
        readTokenRef.current = fresh.readToken;
        const { readToken } = await write(fresh.readToken);
        readTokenRef.current = readToken;
        setErrorMessage(null);
        return true;
      } catch {
        setErrorMessage('Could not save your change — please try again.');
        return false;
      }
    }
  };

  const applyTaskUpdate = async (
    task: Task,
    write: (readToken: string) => Promise<{ readToken: string }>,
    actionType: 'task.toggle' | 'task.reorder' | 'task.delete',
    actionData: Record<string, unknown>,
  ) => {
    if (!id) return;
    const ok = await writeWithStaleTokenRetry(write);
    if (!ok) return;
    const fresh = await getCanvas(id);
    setCanvas(fresh);
    readTokenRef.current = fresh.readToken;
    await recordHumanAction(id, actionType, { taskId: task.id, ...actionData });
  };

  const handleToggle = (task: Task) =>
    applyTaskUpdate(
      task,
      (readToken) => updateTasks(id!, readToken, [{ id: task.id, done: !task.done }]),
      'task.toggle',
      { done: !task.done },
    );

  const handleDelete = (task: Task) =>
    applyTaskUpdate(
      task,
      (readToken) => editTasksHuman(id!, readToken, [{ id: task.id, delete: true }]),
      'task.delete',
      { delete: true },
    );

  const handleMove = (task: Task, direction: 'up' | 'down') => {
    if (!canvas) return;
    const sorted = [...canvas.tasks].sort((a, b) => a.order - b.order);
    const idx = sorted.findIndex((t) => t.id === task.id);
    const swapIdx = direction === 'up' ? idx - 1 : idx + 1;
    if (idx < 0 || swapIdx < 0 || swapIdx >= sorted.length) return;
    const other = sorted[swapIdx];
    const a = task.order;
    const b = other.order;
    void applyTaskUpdate(
      task,
      (readToken) =>
        editTasksHuman(id!, readToken, [
          { id: task.id, order: b },
          { id: other.id, order: a },
        ]),
      'task.reorder',
      { order: b },
    );
  };

  if (!canvas) {
    return (
      <div className="status-line" role="status">
        Loading canvas…
      </div>
    );
  }

  return (
    <>
      <header className="app-header">
        <span className="app-name">COMPASS</span>
      </header>

      <GoalCard goal={canvas.goal} tasks={canvas.tasks} />
      <CurrentCard current={canvas.current} />
      <GapChips gaps={canvas.gaps} />
      <PlanList tasks={canvas.tasks} onToggle={handleToggle} onDelete={handleDelete} onMove={handleMove} />
      <PolicyList policies={canvas.policies} />

      <p className="status-line" data-testid="live-status" role={errorMessage ? 'alert' : undefined}>
        {errorMessage ?? (connected ? 'Live' : 'Syncing…')}
      </p>
    </>
  );
}

export default App;
