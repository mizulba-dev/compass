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
}

interface View {
  x: number;
  y: number;
  scale: number;
}

function childOf(nodes: MapNode[], parentId: string): MapNode[] {
  return nodes.filter((n) => n.parent === parentId);
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


export function FogMapCanvas({ nodes, onAdd, onEdit }: FogMapCanvasProps) {
  const canvasRef = useRef<HTMLDivElement>(null);
  const worldRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<View>({ x: 0, y: 0, scale: 1 });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingText, setEditingText] = useState('');
  const draggingRef = useRef<{ id: string; moved: boolean } | null>(null);

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

  // ---------- node interaction ----------
  const handleNodePointerDown = (e: React.PointerEvent, node: MapNode) => {
    if (editingId === node.id) return;
    e.stopPropagation();
    const el = e.currentTarget as HTMLElement;
    const startX = e.clientX, startY = e.clientY;
    const ox = node.x, oy = node.y;
    draggingRef.current = { id: node.id, moved: false };
    let curX = ox, curY = oy;
    el.setPointerCapture(e.pointerId);

    const move = (ev: PointerEvent) => {
      const dx = (ev.clientX - startX) / view.scale;
      const dy = (ev.clientY - startY) / view.scale;
      const drag = draggingRef.current;
      if (!drag) return;
      if (!drag.moved && Math.hypot(ev.clientX - startX, ev.clientY - startY) > 4) {
        drag.moved = true;
        setSelectedId(null);
        el.classList.add('dragging');
      }
      if (drag.moved) {
        curX = ox + dx;
        curY = oy + dy;
        el.style.left = curX + 'px';
        el.style.top = curY + 'px';
      }
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.classList.remove('dragging');
      const drag = draggingRef.current;
      draggingRef.current = null;
      if (drag?.moved) {
        void onEdit({ id: node.id, x: curX, y: curY });
      } else {
        setSelectedId(node.id);
      }
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
  };

  const startEdit = (node: MapNode) => {
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
                const a = { x: parent.x + 60, y: parent.y + 18 };
                const b = { x: n.x + 60, y: n.y + 18 };
                const dx = Math.max(50, Math.abs(b.x - a.x) * 0.5);
                const sx = b.x > a.x ? 1 : -1;
                return (
                  <path
                    key={n.id}
                    d={`M ${a.x} ${a.y} C ${a.x + dx * sx} ${a.y}, ${b.x - dx * sx} ${b.y}, ${b.x} ${b.y}`}
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
                style={{ left: n.x, top: n.y, borderColor: !n.root && n.color && !n.fog ? n.color : undefined }}
                onPointerDown={(e) => handleNodePointerDown(e, n)}
                onDoubleClick={(e) => {
                  e.stopPropagation();
                  startEdit(n);
                }}
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
                    <span className="text">{n.text || ' '}</span>
                    {n.fog && <span className="fogtag">🌫 わからない</span>}
                  </>
                )}

                {selected && selected.id === n.id && !isEditing && (
                  <div className="node-toolbar" data-testid="node-toolbar" onPointerDown={(e) => e.stopPropagation()}>
                    <button
                      type="button"
                      onClick={() => void addChild(n)}
                      data-testid="node-add-child"
                    >
                      ＋子
                    </button>
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
