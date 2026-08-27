export interface Goal {
  title: string;
  deadline?: string;
  why?: string;
}

export interface CurrentState {
  summary: string;
  updatedAt: string;
}

export interface Gap {
  id: string;
  text: string;
  resolved: boolean;
}

export interface Task {
  id: string;
  text: string;
  day: string | null;
  order: number;
  done: boolean;
  origin: 'agent' | 'human';
}

export interface Policy {
  id: string;
  text: string;
  derivedFrom: string;
}

export interface SessionLogEntry {
  at: string;
  summary: string;
}

export interface HumanAction {
  seq: number;
  type: string;
  data: unknown;
  at: string;
}

export interface Canvas {
  id: string;
  goal: Goal | null;
  current: CurrentState | null;
  gaps: Gap[];
  tasks: Task[];
  policies: Policy[];
  sessions: SessionLogEntry[];
  readToken: string;
  humanActions: HumanAction[];
}
