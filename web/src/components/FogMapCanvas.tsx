import { useEffect, useRef, useState } from 'react';
import type { MapNode } from '../types';

export interface AddNodeInput {
  text: string;
  x: number;
  y: number;
  parent?: string;
}

export interface EditNodeInput {
  id: string;
  text?: string;
  x?: number;
  y?: number;
  fog?: boolean;
  star?: boolean;
  delete?: boolean;
}

interface FogMapCanvasProps {
  nodes: MapNode[];
  /** Adds a node placed directly by the human; resolves with the new node's id once persisted (or undefined if the write failed). */
  onAdd: (input: AddNodeInput) => Promise<string | undefined>;
  /** Applies a human edit (move/fog/star/delete/text); resolves once persisted. */
  onEdit: (input: EditNodeInput) => Promise<void>;
  /** Marks a node as the conversation's current focus (records a "discuss" humanAction). */
  onDiscuss: (node: MapNode) => void;
}

interface View {
  x: number;
  y: number;
  scale: number;
}

const SINGLE_CLICK_DELAY_MS = 250;
const LONG_PRESS_MS = 500;
// How long the 💬 badge stays lit after marking a node for discussion. This
// is a client-only, purely visual hint (not persisted map data), so it's
// simplest to just time it out rather than track whether the agent has
// actually consumed the humanAction yet — the badge is a nudge, not a
// source of truth.
const DISCUSS_BADGE_MS = 60000;

function childOf(nodes: MapNode[], parentId: string): MapNode[] {
  return nodes.filter((n) => n.parent === parentId);
}

/** The point on a node's box an edge connects to — shared by the SVG render and the imperative drag-time update below, so both draw the exact same curve. */
function anchorOf(x: number, y: number): { x: number; y: number } {
  return { x: x + 60, y: y + 18 };
}

function edgePathD(a: { x: number; y: number }, b: { x: number; y: number }): string {
  const dx = Math.max(50, Math.abs(b.x - a.x) * 0.5);
  const sx = b.x > a.x ? 1 : -1;
  return `M ${a.x} ${a.y} C ${a.x + dx * sx} ${a.y}, ${b.x - dx * sx} ${b.y}, ${b.x} ${b.y}`;
}

/** Mirrors the server's childPos: fan new children to the right (root's alternate left/right), stacked among same-side siblings. */
function childPos(nodes: MapNode[], parent: MapNode): { x: number; y: number } {
  const siblings = childOf(nodes, parent.id);
  const dir = parent.root ? (siblings.length % 2 === 0 ? 1 : -1) : parent.dir || 1;
  const x = parent.x + (dir > 0 ? 280 : -260);
  const sameSide = siblings.filter((s) => s.x > parent.x === dir > 0).length;
  const y = parent.y - 20 + sameSide * 64;
  return { x, y };
}


export function FogMapCanvas({ nodes, onAdd, onEdit, onDiscuss }: FogMapCanvasProps) {
  const canvasRef = useRef<HTMLDivElement>(null);
  const worldRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<View>({ x: 0, y: 0, scale: 1 });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingText, setEditingText] = useState('');
  // Which nodes currently show the 💬 badge. Purely a client-side visual
  // hint (see DISCUSS_BADGE_MS) — never persisted, never read from the map.
  const [discussedIds, setDiscussedIds] = useState<Set<string>>(new Set());
  const discussTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  const draggingRef = useRef<{ id: string; moved: boolean } | null>(null);
  // Drag-time edge following: mirrors the mock's
  // requestAnimationFrame(renderEdges) on every pointermove. The dragged
  // node's own position is already updated directly via el.style (no
  // setState per move, so no full re-render); the connected SVG paths need
  // the same imperative treatment; a React re-render only happens once, on
  // pointerup, when the final position is persisted.
  const pendingEdgeDragRef = useRef<{ node: MapNode; x: number; y: number } | null>(null);
  const edgeRafIdRef = useRef<number | null>(null);
  // Single click grows a child; double click edits the existing node. Both
  // start from the same pointerup, so a plain tap defers its action for
  // SINGLE_CLICK_DELAY_MS — if a second tap on the same node lands before
  // that fires, we cancel it and let the native dblclick handler (which
  // fires within the same window) take over instead, so a double click
  // never also sprouts a child.
  const pendingClickRef = useRef<{ timer: ReturnType<typeof setTimeout>; nodeId: string } | null>(null);
  // Long-press (touch) opens the same menu a right-click does.
  const longPressTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const toWorld = (clientX: number, clientY: number) => ({
    x: (clientX - view.x) / view.scale,
    y: (clientY - view.y) / view.scale,
  });

  const byId = (id: string) => nodes.find((n) => n.id === id) ?? null;

  // ---------- pan ----------
  const handleCanvasPointerDown = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest('.node')) return;
    setSelectedId(null);
    const canvas = canvasRef.current;
    if (!canvas) return;
    canvas.classList.add('panning');
    const startX = e.clientX - view.x;
    const startY = e.clientY - view.y;
    canvas.setPointerCapture(e.pointerId);
    const move = (ev: PointerEvent) => setView((v) => ({ ...v, x: ev.clientX - startX, y: ev.clientY - startY }));
    const up = () => {
      canvas.classList.remove('panning');
      canvas.removeEventListener('pointermove', move);
      canvas.removeEventListener('pointerup', up);
    };
    canvas.addEventListener('pointermove', move);
    canvas.addEventListener('pointerup', up);
  };

  const handleCanvasDoubleClick = (e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest('.node')) return;
    const p = toWorld(e.clientX, e.clientY);
    void placeFreeNode(p.x, p.y);
  };

  const zoomAt = (clientX: number, clientY: number, factor: number) => {
    setView((v) => {
      const scale = Math.min(1.8, Math.max(0.35, v.scale * factor));
      const w = { x: (clientX - v.x) / v.scale, y: (clientY - v.y) / v.scale };
      return { scale, x: clientX - w.x * scale, y: clientY - w.y * scale };
    });
  };

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault();
    zoomAt(e.clientX, e.clientY, e.deltaY < 0 ? 1.08 : 1 / 1.08);
  };

  const fit = () => {
    if (nodes.length === 0) {
      setView({ x: 0, y: 0, scale: 1 });
      return;
    }
    const pad = 70;
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const n of nodes) {
      minX = Math.min(minX, n.x);
      minY = Math.min(minY, n.y);
      maxX = Math.max(maxX, n.x + 200);
      maxY = Math.max(maxY, n.y + 50);
    }
    const w = window.innerWidth, h = window.innerHeight;
    const scale = Math.max(0.35, Math.min(1.4, Math.min((w - pad * 2) / (maxX - minX), (h - pad * 2) / (maxY - minY))));
    setView({
      scale,
      x: (w - (maxX - minX) * scale) / 2 - minX * scale,
      y: (h - (maxY - minY) * scale) / 2 - minY * scale,
    });
  };

  // ---------- add ----------
  // Mirrors the mock: create an empty node and drop straight into inline
  // editing, rather than prompting for text up front. window.prompt() was
  // tried first but is suppressed in some embedded browsers (e.g. ChatGPT's
  // in-app browser), which would make node creation silently do nothing in
  // the primary target environment.
  async function placeFreeNode(x: number, y: number) {
    const isFirst = nodes.length === 0;
    const newId = await onAdd({ text: '', x: isFirst ? x - 90 : x, y: isFirst ? y - 20 : y });
    if (newId) {
      setSelectedId(null);
      setEditingId(newId);
      setEditingText('');
    }
  }

  async function addChild(parent: MapNode) {
    const pos = childPos(nodes, parent);
    const newId = await onAdd({ text: '', x: pos.x, y: pos.y, parent: parent.id });
    if (newId) {
      setSelectedId(null);
      setEditingId(newId);
      setEditingText('');
    }
  }

  // ---------- discuss ----------
  const markDiscuss = (node: MapNode) => {
    onDiscuss(node);
    const existing = discussTimersRef.current.get(node.id);
    if (existing) clearTimeout(existing);
    setDiscussedIds((prev) => new Set(prev).add(node.id));
    const timer = setTimeout(() => {
      discussTimersRef.current.delete(node.id);
      setDiscussedIds((prev) => {
        const next = new Set(prev);
        next.delete(node.id);
        return next;
      });
    }, DISCUSS_BADGE_MS);
    discussTimersRef.current.set(node.id, timer);
  };

  useEffect(() => {
    const timers = discussTimersRef.current;
    return () => {
      timers.forEach((t) => clearTimeout(t));
    };
  }, []);

  // ---------- node interaction ----------

  /** Rewrites the `d` of every SVG path touching `node` (its own edge to its parent, plus every edge to its children) to match a live drag position, without touching React state. */
  const updateEdgesForDrag = (node: MapNode, x: number, y: number) => {
    const world = worldRef.current;
    if (!world) return;
    const b = anchorOf(x, y);
    if (node.parent) {
      const parent = byId(node.parent);
      if (parent) {
        const path = world.querySelector<SVGPathElement>(`[data-edge-child="${node.id}"]`);
        path?.setAttribute('d', edgePathD(anchorOf(parent.x, parent.y), b));
      }
    }
    for (const child of nodes) {
      if (child.parent === node.id) {
        const path = world.querySelector<SVGPathElement>(`[data-edge-child="${child.id}"]`);
        path?.setAttribute('d', edgePathD(b, anchorOf(child.x, child.y)));
      }
    }
  };

  /** Throttles edge updates to once per animation frame, mirroring the mock's requestAnimationFrame(renderEdges) on every pointermove — always applies the LATEST pending position, not the one at schedule time. */
  const scheduleEdgeDragUpdate = (node: MapNode, x: number, y: number) => {
    pendingEdgeDragRef.current = { node, x, y };
    if (edgeRafIdRef.current != null) return;
    edgeRafIdRef.current = requestAnimationFrame(() => {
      edgeRafIdRef.current = null;
      const pending = pendingEdgeDragRef.current;
      if (pending) updateEdgesForDrag(pending.node, pending.x, pending.y);
    });
  };

  /** Cancels any single-click-grows-a-child timer waiting on `node`, so a following dblclick (edit) never also sprouts a child. */
  const cancelPendingClick = (nodeId?: string) => {
    const pending = pendingClickRef.current;
    if (pending && (nodeId === undefined || pending.nodeId === nodeId)) {
      clearTimeout(pending.timer);
      pendingClickRef.current = null;
    }
  };

  const clearLongPressTimer = () => {
    if (longPressTimerRef.current != null) {
      clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
    }
  };

  const handleNodePointerDown = (e: React.PointerEvent, node: MapNode) => {
    if (editingId === node.id) return;
    e.stopPropagation();
    const el = e.currentTarget as HTMLElement;
    const startX = e.clientX, startY = e.clientY;
    const ox = node.x, oy = node.y;
    draggingRef.current = { id: node.id, moved: false };
    let curX = ox, curY = oy;
    el.setPointerCapture(e.pointerId);

    if (e.pointerType === 'touch') {
      clearLongPressTimer();
      longPressTimerRef.current = setTimeout(() => {
        longPressTimerRef.current = null;
        const drag = draggingRef.current;
        if (drag && !drag.moved) {
          cancelPendingClick(node.id);
          setSelectedId(node.id);
        }
      }, LONG_PRESS_MS);
    }

    const move = (ev: PointerEvent) => {
      const dx = (ev.clientX - startX) / view.scale;
      const dy = (ev.clientY - startY) / view.scale;
      const drag = draggingRef.current;
      if (!drag) return;
      if (!drag.moved && Math.hypot(ev.clientX - startX, ev.clientY - startY) > 4) {
        drag.moved = true;
        clearLongPressTimer();
        setSelectedId(null);
        el.classList.add('dragging');
      }
      if (drag.moved) {
        curX = ox + dx;
        curY = oy + dy;
        el.style.left = curX + 'px';
        el.style.top = curY + 'px';
        scheduleEdgeDragUpdate(node, curX, curY);
      }
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.classList.remove('dragging');
      clearLongPressTimer();
      if (edgeRafIdRef.current != null) {
        cancelAnimationFrame(edgeRafIdRef.current);
        edgeRafIdRef.current = null;
      }
      pendingEdgeDragRef.current = null;
      const drag = draggingRef.current;
      draggingRef.current = null;
      if (drag?.moved) {
        void onEdit({ id: node.id, x: curX, y: curY });
        return;
      }
      // A plain tap: if a second tap on this same node lands within
      // SINGLE_CLICK_DELAY_MS, treat the pair as a double click and let the
      // dblclick handler edit the node instead (see cancelPendingClick).
      if (pendingClickRef.current?.nodeId === node.id) {
        cancelPendingClick(node.id);
        return;
      }
      const timer = setTimeout(() => {
        pendingClickRef.current = null;
        void addChild(node);
      }, SINGLE_CLICK_DELAY_MS);
      pendingClickRef.current = { timer, nodeId: node.id };
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
  };

  const handleNodeContextMenu = (e: React.MouseEvent, node: MapNode) => {
    e.preventDefault();
    e.stopPropagation();
    cancelPendingClick(node.id);
    setSelectedId(node.id);
  };

  const startEdit = (node: MapNode) => {
    cancelPendingClick(node.id);
    setSelectedId(null);
    setEditingId(node.id);
    setEditingText(node.text);
  };

  const commitEdit = async () => {
    const id = editingId;
    if (!id) return;
    setEditingId(null);
    const text = editingText.trim();
    const node = byId(id);
    if (!text) {
      if (node && !node.root && childOf(nodes, id).length === 0) {
        await onEdit({ id, delete: true });
      }
      return;
    }
    await onEdit({ id, text });
  };

  useEffect(() => {
    if (nodes.length === 0) fit();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selected = selectedId ? byId(selectedId) : null;

  return (
    <>
      <div
        ref={canvasRef}
        className="canvas"
        onPointerDown={handleCanvasPointerDown}
        onDoubleClick={handleCanvasDoubleClick}
        onWheel={handleWheel}
      >
        <div
          ref={worldRef}
          className="world"
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
        >
          <svg className="edges" style={{ overflow: 'visible' }}>
            {nodes
              .filter((n) => n.parent && byId(n.parent))
              .map((n) => {
                const parent = byId(n.parent!)!;
                const a = anchorOf(parent.x, parent.y);
                const b = anchorOf(n.x, n.y);
                return (
                  <path
                    key={n.id}
                    data-edge-child={n.id}
                    d={edgePathD(a, b)}
                    stroke={n.color || 'var(--node-edge)'}
                  />
                );
              })}
          </svg>

          {nodes.map((n) => {
            const isEditing = editingId === n.id;
            const cls = [
              'node',
              n.root ? 'root' : '',
              n.kind === 'question' ? 'question' : '',
              n.fog ? 'fogged' : '',
              selectedId === n.id ? 'selected' : '',
              isEditing ? 'editing' : '',
            ]
              .filter(Boolean)
              .join(' ');
            return (
              <div
                key={n.id}
                data-testid="map-node"
                data-node-id={n.id}
                className={cls}
                tabIndex={0}
                style={
                  {
                    left: n.x,
                    top: n.y,
                    borderColor: !n.root && n.color && !n.fog ? n.color : undefined,
                    '--node-color': n.color || undefined,
                  } as React.CSSProperties
                }
                onPointerDown={(e) => handleNodePointerDown(e, n)}
                onDoubleClick={(e) => {
                  e.stopPropagation();
                  startEdit(n);
                }}
                onContextMenu={(e) => handleNodeContextMenu(e, n)}
              >
                {isEditing ? (
                  <textarea
                    className="edit-input"
                    data-testid="node-edit-input"
                    rows={1}
                    autoFocus
                    value={editingText}
                    onChange={(e) => {
                      setEditingText(e.target.value);
                      const el = e.target;
                      el.style.height = 'auto';
                      el.style.height = el.scrollHeight + 'px';
                    }}
                    onFocus={(e) => {
                      const el = e.target;
                      el.style.height = 'auto';
                      el.style.height = el.scrollHeight + 'px';
                    }}
                    onBlur={() => void commitEdit()}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault();
                        (e.target as HTMLTextAreaElement).blur();
                      }
                    }}
                  />
                ) : (
                  <>
                    {n.origin === 'agent' && !n.root && <span className="spark">✦</span>}
                    {n.star && <span className="spark">★</span>}
                    {discussedIds.has(n.id) && <span className="spark" data-testid="discuss-badge">💬</span>}
                    <span className="text">{n.text || ' '}</span>
                    {n.fog && <span className="fogtag">🌫 わからない</span>}
                  </>
                )}

                {selected && selected.id === n.id && !isEditing && (
                  <div
                    className="node-toolbar"
                    data-testid="node-toolbar"
                    onPointerDown={(e) => e.stopPropagation()}
                    onContextMenu={(e) => e.preventDefault()}
                  >
                    {!n.root && (
                      <button
                        type="button"
                        onClick={() => void onEdit({ id: n.id, fog: !n.fog })}
                        data-testid="node-toggle-fog"
                      >
                        {n.fog ? '🌫 晴れた' : '🌫'}
                      </button>
                    )}
                    <button type="button" onClick={() => startEdit(n)} data-testid="node-start-edit">
                      ✏
                    </button>
                    <button
                      type="button"
                      onClick={() => markDiscuss(n)}
                      data-testid="node-discuss"
                    >
                      💬
                    </button>
                    {!n.root && (
                      <button
                        type="button"
                        onClick={() => void onEdit({ id: n.id, star: !n.star })}
                        data-testid="node-toggle-star"
                      >
                        {n.star ? '★' : '☆'}
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedId(null);
                        void onEdit({ id: n.id, delete: true });
                      }}
                      style={{ color: 'var(--danger)' }}
                      data-testid="node-delete"
                    >
                      ✕
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {nodes.length === 0 && (
        <div className="empty-hint">
          <div className="big">考えたいことを、まず1つ置く</div>
          <div className="small">下の ＋ か、キャンバスをダブルクリック</div>
        </div>
      )}

      <div className="zoomer">
        <button type="button" aria-label="拡大" onClick={() => zoomAt(window.innerWidth / 2, window.innerHeight / 2, 1.2)}>＋</button>
        <button type="button" aria-label="縮小" onClick={() => zoomAt(window.innerWidth / 2, window.innerHeight / 2, 1 / 1.2)}>－</button>
        <button type="button" aria-label="全体表示" style={{ fontSize: '.7rem' }} onClick={fit}>⌂</button>
      </div>

      <button
        type="button"
        className="add-fab"
        aria-label="ノードを追加"
        data-testid="add-node-fab"
        onClick={() => {
          const p = toWorld(window.innerWidth / 2, window.innerHeight / 2);
          void placeFreeNode(p.x, p.y);
        }}
      >
        ＋
      </button>
    </>
  );
}
