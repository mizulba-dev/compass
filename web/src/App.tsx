import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { FogMap } from './types';
import { getMap, addNodeHuman, editNodeHuman, recordHumanAction, tidyMap, StaleTokenError, type HumanActionType } from './api';
import { subscribeLive } from './live';
import { resolveCanvasId } from './canvasBootstrap';
import { setMapUpdateListener, setWebMCPStatusListener, type WebMCPStatus } from './webmcp/register';
import { FogMapCanvas, type AddNodeInput, type EditNodeInput } from './components/FogMapCanvas';
import { HarvestSheet } from './components/HarvestSheet';
import { ClipboardList } from 'lucide-react';

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
  const [map, setMap] = useState<FogMap | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [webmcpStatus, setWebmcpStatus] = useState<WebMCPStatus>({ state: 'waiting' });
  const [sheetOpen, setSheetOpen] = useState(false);
  const readTokenRef = useRef('');

  // Page-level status, independent of which map is loaded — connect as
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

    getMap(id).then((m) => {
      if (!cancelled) {
        setMap(m);
        readTokenRef.current = m.readToken;
      }
    });

    const unsubscribe = subscribeLive(id, (m) => {
      setMap(m);
      readTokenRef.current = m.readToken;
    });

    // Tools are already registered (see main.tsx, before this component
    // even mounted) — just connect this instance as their UI sink.
    setMapUpdateListener((m) => {
      setMap(m);
      readTokenRef.current = m.readToken;
    });

    return () => {
      cancelled = true;
      unsubscribe();
      setMapUpdateListener(null);
    };
  }, [id]);

  /**
   * Runs a write, retrying once with a freshly re-read token if the human's
   * action raced a stale readToken (e.g. the agent wrote in between). On a
   * second failure, surfaces an English message on the footer instead of
   * failing silently.
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
        const fresh = await getMap(id);
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

  const refreshAndRecord = async (actionType: HumanActionType, data: unknown) => {
    if (!id) return;
    const fresh = await getMap(id);
    setMap(fresh);
    readTokenRef.current = fresh.readToken;
    await recordHumanAction(id, actionType, data);
  };

  const handleAdd = async (input: AddNodeInput): Promise<string | undefined> => {
    if (!id) return undefined;
    const beforeIds = new Set((map?.nodes ?? []).map((n) => n.id));
    const ok = await writeWithStaleTokenRetry((readToken) => addNodeHuman(id, readToken, input));
    if (!ok) return undefined;
    const fresh = await getMap(id);
    setMap(fresh);
    readTokenRef.current = fresh.readToken;
    const created = fresh.nodes.find((n) => !beforeIds.has(n.id));
    await recordHumanAction(id, 'add', { nodeId: created?.id, text: input.text, parent: input.parent ?? null });
    return created?.id;
  };

  const handleEdit = async (input: EditNodeInput) => {
    if (!id) return;
    const ok = await writeWithStaleTokenRetry((readToken) => editNodeHuman(id, readToken, input));
    if (!ok) return;
    if (input.delete) {
      await refreshAndRecord('delete', { nodeId: input.id });
    } else if (input.x !== undefined || input.y !== undefined) {
      await refreshAndRecord('move', { nodeId: input.id, x: input.x, y: input.y });
    } else if (input.fog !== undefined) {
      await refreshAndRecord(input.fog ? 'fog' : 'unfog', { nodeId: input.id });
    } else if (input.star !== undefined) {
      await refreshAndRecord('star', { nodeId: input.id, star: input.star });
    } else if (input.done !== undefined) {
      await refreshAndRecord('done', { nodeId: input.id, done: input.done });
    } else if (input.kind !== undefined) {
      await refreshAndRecord('edit', { nodeId: input.id, kind: input.kind });
    } else if (input.text !== undefined) {
      await refreshAndRecord('edit', { nodeId: input.id, text: input.text });
    }
  };

  const handleTidy = async () => {
    await writeWithStaleTokenRetry((readToken) => tidyMap(id!, readToken));
    if (id) {
      const fresh = await getMap(id);
      setMap(fresh);
      readTokenRef.current = fresh.readToken;
    }
  };

  return (
    <>
      {map ? (
        <FogMapCanvas nodes={map.nodes} onAdd={handleAdd} onEdit={handleEdit} onTidy={handleTidy} />
      ) : (
        <div className="empty-hint" role="status">
          <div className="big">Loading…</div>
        </div>
      )}

      {/* Portalled to document.body: all `position: fixed` chrome, kept out
          of any ancestor that could someday gain a transform (see the same
          note in FogMapCanvas.tsx). */}
      {createPortal(
        <>
          <div className="brand">
            WebMCP Demo
            <p className="webmcp-status" data-testid="webmcp-status" role={errorMessage ? 'alert' : undefined}>
              {errorMessage ?? webmcpStatusText(webmcpStatus)}
            </p>
          </div>

          {(map?.harvest || (map?.nodes ?? []).some((n) => n.kind === 'task')) && (
            <button
              type="button"
              className="harvest-fab"
              aria-label="View plan"
              data-testid="harvest-fab"
              onClick={() => setSheetOpen(true)}
            >
              <ClipboardList size={16} /> Plan
            </button>
          )}

          <HarvestSheet
            harvest={map?.harvest ?? null}
            nodes={map?.nodes ?? []}
            open={sheetOpen}
            onClose={() => setSheetOpen(false)}
            onToggleDone={(nodeId, done) => void handleEdit({ id: nodeId, done })}
          />
        </>,
        document.body,
      )}
    </>
  );
}

export default App;
