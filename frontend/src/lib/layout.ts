// Split-pane layout: a binary tree of panes, and the geometry derived from it.

import type { InfraContext } from './infra';

export interface Pane {
  id: number;
  /** Backend session id; null until the shell has been spawned. */
  sessionId: string | null;
  /** Directory the shell was started in. */
  initialCwd: string;
  /** Latest known state of the foreground process. */
  cwd: string;
  process: string;
  isShell: boolean;
  /** Title set by the running program via OSC 0/2, e.g. by ssh or vim. */
  oscTitle: string;
  /** The shell reports prompts and commands (shell integration is active). */
  integrated: boolean;
  /** A command line is executing; `command` is its text. */
  running: boolean;
  command: string;
  lastCommand: FinishedCommand | null;
  /** Cloud and cluster context the shell reported at its last prompt. */
  infra: InfraContext | null;
  /** Typed into the shell once it starts, e.g. to open a file in $EDITOR. */
  initialCommand?: string;
  /** Scrollback carried over from another window, written once on mount. */
  restoreBuffer?: string;
}

export interface FinishedCommand {
  text: string;
  exitCode: number;
  durationMs: number;
}

/** row = side by side (split right), column = stacked (split down). */
export type SplitDir = 'row' | 'column';

export interface PaneNode {
  kind: 'pane';
  pane: Pane;
}

export interface SplitNode {
  kind: 'split';
  dir: SplitDir;
  /** Share of the space taken by `a` (left or top), 0–1. */
  ratio: number;
  a: LayoutNode;
  b: LayoutNode;
}

export type LayoutNode = PaneNode | SplitNode;

/** A rectangle in fractions of the tab's area. */
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface PlacedPane {
  pane: Pane;
  rect: Rect;
}

export interface Divider {
  split: SplitNode;
  /** The area the split divides. */
  rect: Rect;
}

export type Direction = 'left' | 'right' | 'up' | 'down';

export const MIN_RATIO = 0.1;

export function leaves(node: LayoutNode): Pane[] {
  return node.kind === 'pane' ? [node.pane] : [...leaves(node.a), ...leaves(node.b)];
}

export function findPane(node: LayoutNode, id: number): Pane | undefined {
  return leaves(node).find((p) => p.id === id);
}

/** Lays the tree out in the unit square. */
export function place(node: LayoutNode): { panes: PlacedPane[]; dividers: Divider[] } {
  const panes: PlacedPane[] = [];
  const dividers: Divider[] = [];
  const walk = (n: LayoutNode, r: Rect) => {
    if (n.kind === 'pane') {
      panes.push({ pane: n.pane, rect: r });
      return;
    }
    dividers.push({ split: n, rect: r });
    if (n.dir === 'row') {
      const wa = r.w * n.ratio;
      walk(n.a, { x: r.x, y: r.y, w: wa, h: r.h });
      walk(n.b, { x: r.x + wa, y: r.y, w: r.w - wa, h: r.h });
    } else {
      const ha = r.h * n.ratio;
      walk(n.a, { x: r.x, y: r.y, w: r.w, h: ha });
      walk(n.b, { x: r.x, y: r.y + ha, w: r.w, h: r.h - ha });
    }
  };
  walk(node, { x: 0, y: 0, w: 1, h: 1 });
  return { panes, dividers };
}

/** Splits pane `id`, putting `added` after it (right or below). */
export function splitPane(node: LayoutNode, id: number, dir: SplitDir, added: Pane): LayoutNode {
  if (node.kind === 'pane') {
    if (node.pane.id !== id) return node;
    return { kind: 'split', dir, ratio: 0.5, a: node, b: { kind: 'pane', pane: added } };
  }
  node.a = splitPane(node.a, id, dir, added);
  node.b = splitPane(node.b, id, dir, added);
  return node;
}

/** Removes pane `id`; its sibling takes the freed space. Null when nothing is left. */
export function removePane(node: LayoutNode, id: number): LayoutNode | null {
  if (node.kind === 'pane') return node.pane.id === id ? null : node;
  const a = removePane(node.a, id);
  const b = removePane(node.b, id);
  if (!a) return b;
  if (!b) return a;
  node.a = a;
  node.b = b;
  return node;
}

/** The pane next to `fromId` in a direction, preferring the one most in line with it. */
export function neighbor(panes: PlacedPane[], fromId: number, dir: Direction): Pane | undefined {
  const from = panes.find((p) => p.pane.id === fromId)?.rect;
  if (!from) return undefined;
  const EPS = 1e-6;
  const horizontal = dir === 'left' || dir === 'right';

  let best: { pane: Pane; distance: number; overlap: number } | undefined;
  for (const { pane, rect: r } of panes) {
    if (pane.id === fromId) continue;
    let distance: number;
    switch (dir) {
      case 'right': distance = r.x - (from.x + from.w); break;
      case 'left': distance = from.x - (r.x + r.w); break;
      case 'down': distance = r.y - (from.y + from.h); break;
      case 'up': distance = from.y - (r.y + r.h); break;
    }
    if (distance < -EPS) continue;
    const overlap = horizontal
      ? Math.min(from.y + from.h, r.y + r.h) - Math.max(from.y, r.y)
      : Math.min(from.x + from.w, r.x + r.w) - Math.max(from.x, r.x);
    if (overlap <= EPS) continue;
    if (!best || distance < best.distance - EPS || (Math.abs(distance - best.distance) <= EPS && overlap > best.overlap)) {
      best = { pane, distance, overlap };
    }
  }
  return best?.pane;
}

export function clampRatio(r: number): number {
  return Math.max(MIN_RATIO, Math.min(1 - MIN_RATIO, r));
}
