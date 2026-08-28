import { useEffect, useRef, useState } from 'react';
import type { Canvas, Task } from './types';
import { getCanvas, updateTasks, editTasksHuman, recordHumanAction, StaleTokenError } from './api';
import { subscribeLive } from './live';
import { resolveCanvasId } from './canvasBootstrap';
import { setCanvasUpdateListener, setWebMCPStatusListener, type WebMCPStatus } from './webmcp/register';
import { GoalCard } from './components/GoalCard';
import { CurrentCard } from './components/CurrentCard';
import { GapChips } from './components/GapChips';
import { PlanList } from './components/PlanList';
import { PolicyList } from './components/PolicyList';

function webmcpStatusText(status: WebMCPStatus): string {
  switch (status.state) {
    case 'waiting':
      return 'Waiting for WebMCP…';
    case 'registered':
      return `Site tools: ${status.count} available`;
    case 'unsupported':
      return 'WebMCP not supported in this browser';
  }
}

function App() {
  const [id, setId] = useState<string | null>(null);
  const [canvas, setCanvas] = useState<Canvas | null>(null);
  const [connected, setConnected] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [webmcpStatus, setWebmcpStatus] = useState<WebMCPStatus>({ state: 'waiting' });
  const readTokenRef = useRef('');

  // Page-level status, independent of which canvas is loaded — connect as
  // soon as this component exists so devtools-less hosts (ChatGPT's in-app
  // browser) have something on screen to diagnose registration with.
  useEffect(() => {
    setWebMCPStatusListener(setWebmcpStatus);
    return () => setWebMCPStatusListener(null);
  }, []);

  // Resolve the canvas id via the same shared bootstrap a pre-mount WebMCP
  // tool call may already be waiting on (or may have already resolved) —
  // never creates a second canvas of its own.
  useEffect(() => {
    let cancelled = false;
    resolveCanvasId().then((resolvedId) => {
      if (!cancelled) setId(resolvedId);
    });
    return () => {
      cancelled = true;
    };
  }, []);

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

    // Tools are already registered (see main.tsx, before this component
    // even mounted) — just connect this instance as their UI sink.
    setCanvasUpdateListener((c) => {
      setCanvas(c);
      readTokenRef.current = c.readToken;
    });

    return () => {
      cancelled = true;
      unsubscribe();
      setCanvasUpdateListener(null);
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

  return (
    <>
      <header className="app-header">
        <span className="app-name">COMPASS</span>
      </header>

      {!canvas ? (
        <div className="status-line" role="status">
          Loading canvas…
        </div>
      ) : (
        <>
          <GoalCard goal={canvas.goal} tasks={canvas.tasks} />
          <CurrentCard current={canvas.current} />
          <GapChips gaps={canvas.gaps} />
          <PlanList tasks={canvas.tasks} onToggle={handleToggle} onDelete={handleDelete} onMove={handleMove} />
          <PolicyList policies={canvas.policies} />

          <p className="status-line" data-testid="live-status" role={errorMessage ? 'alert' : undefined}>
            {errorMessage ?? (connected ? 'Live' : 'Syncing…')}
          </p>
        </>
      )}

      <footer className="app-footer">
        <p className="status-line" data-testid="webmcp-status">
          {webmcpStatusText(webmcpStatus)}
        </p>
      </footer>
    </>
  );
}

export default App;
