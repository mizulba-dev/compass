export type NodeKind = 'normal' | 'question';
export type NodeOrigin = 'human' | 'agent';

export interface MapNode {
  id: string;
  text: string;
  x: number;
  y: number;
  parent: string | null;
  root?: boolean;
  dir?: number; // 1 (right) or -1 (left) — which side this subtree grows on
  color?: string;
  kind: NodeKind;
  fog: boolean;
  star: boolean;
  origin: NodeOrigin;
  createdAt: string;
}

export interface Harvest {
  goal: string;
  premises: string[];
  tasks: string[];
}

export interface HumanAction {
  seq: number;
  type: string;
  data: unknown;
  at: string;
}

export interface FogMap {
  id: string;
  title: string;
  nodes: MapNode[];
  harvest: Harvest | null;
  readToken: string;
  humanActions: HumanAction[];
}
