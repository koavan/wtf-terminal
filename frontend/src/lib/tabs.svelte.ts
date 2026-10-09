import type { Info } from '../../bindings/github.com/dayanraj/wtf/internal/terminal';
import type { InfraContext } from './infra';
import {
  clampRatio, findPane, leaves, neighbor, place, removePane, splitPane,
  type Direction, type LayoutNode, type Pane, type SplitDir,
} from './layout';

export type { Pane } from './layout';

export interface Tab {
  id: number;
  root: LayoutNode;
  focusedPaneId: number;
  /** Pane temporarily filling the whole tab, if any. */
  zoomedPaneId: number | null;
  /** Output arrived while the tab was in the background. */
  activity: boolean;
  /** A command finished while the tab was in the background. */
  finished: 'ok' | 'fail' | null;
  groupId: number | null;
  /** Name the user gave the tab; null shows the automatic name. */
  customTitle: string | null;
}

/** Longest name a tab can be given. */
export const MAX_TITLE = 60;

/** Trims a name; null when nothing is left (meaning "automatic"). */
function cleanTitle(v: unknown): string | null {
  if (typeof v !== 'string') return null;
  const t = v.replace(/\s+/g, ' ').trim().slice(0, MAX_TITLE);
  return t || null;
}

export const GROUP_COLORS = [
  'blue', 'red', 'yellow', 'green', 'pink', 'purple', 'cyan', 'orange', 'grey',
] as const;
export type GroupColor = (typeof GROUP_COLORS)[number];

export interface Group {
  id: number;
  name: string;
  color: GroupColor;
  collapsed: boolean;
}

const MAX_REOPEN = 20;

/**
 * A saved layout. On disk panes keep only their folder; a tab moving to
 * another window also carries each pane's live session and scrollback.
 */
export type SavedNode =
  | { cwd: string; sessionId?: string; buffer?: string }
  | { dir: SplitDir; ratio: number; a: SavedNode; b: SavedNode };

/** A tab handed to another window (see detach/adopt). */
export interface DetachedTab {
  kind: 'tab';
  tab: SavedTabs['tabs'][number];
  group: Omit<Group, 'id' | 'collapsed'> | null;
}

export interface SavedTabs {
  tabs: { groupId: number | null; root: SavedNode; focused: number; zoomed: number | null; title?: string | null }[];
  groups: Group[];
  /** Index of the active tab. */
  active: number;
}

const MAX_SAVED_DEPTH = 16;

/**
 * Sessions handed to another window: their panes unmount without killing
 * the shell (see TerminalPane).
 */
export const detachedSessions = new Set<string>();

// Invariant: the tabs of a group are always contiguous in `list`, like in
// Chrome. Every method that reorders or regroups tabs preserves this.
class Tabs {
  list = $state<Tab[]>([]);
  groups = $state<Group[]>([]);
  activeId = $state<number | null>(null);

  /** Called when the last tab is gone; the window closes itself. */
  onEmpty: () => void = () => {};

  #nextId = 1;
  #nextPaneId = 1;
  #nextGroupId = 1;
  #recentlyClosed: string[] = [];

  get active(): Tab | undefined {
    return this.list.find((t) => t.id === this.activeId);
  }

  /** Tabs that are on screen, i.e. not hidden inside a collapsed group. */
  get visible(): Tab[] {
    return this.list.filter((t) => !this.isHidden(t));
  }

  get(id: number): Tab | undefined {
    return this.list.find((t) => t.id === id);
  }

  /** The pane of the active tab that has keyboard focus. */
  get activePane(): Pane | undefined {
    const tab = this.active;
    return tab && focusedPane(tab);
  }

  group(id: number | null): Group | undefined {
    return id === null ? undefined : this.groups.find((g) => g.id === id);
  }

  tabsIn(groupId: number): Tab[] {
    return this.list.filter((t) => t.groupId === groupId);
  }

  isHidden(tab: Tab): boolean {
    return !!this.group(tab.groupId)?.collapsed;
  }

  // ── Opening and closing ──────────────────────────────────────────────

  /**
   * Opens a new tab and returns its id. By default it starts in the active
   * tab's directory; `command` is typed into its shell once it starts.
   */
  open({
    cwd = this.activePane?.cwd ?? '',
    index = this.list.length,
    groupId = null as number | null,
    command = undefined as string | undefined,
  } = {}): number {
    const pane = this.#newPane(cwd);
    if (command) pane.initialCommand = command;
    const tab: Tab = {
      id: this.#nextId++,
      root: { kind: 'pane', pane },
      focusedPaneId: pane.id,
      zoomedPaneId: null,
      activity: false,
      finished: null,
      groupId,
      customTitle: null,
    };
    this.list.splice(index, 0, tab);
    this.activate(tab.id);
    return tab.id;
  }

  /** Opens a tab right after `id`, in the same group. */
  openNextTo(id: number, duplicate = false) {
    const i = this.#index(id);
    if (i < 0) return;
    const src = this.list[i];
    this.open({ cwd: duplicate ? focusedPane(src).cwd : undefined, index: i + 1, groupId: src.groupId });
  }

  duplicate() {
    if (this.activeId !== null) this.openNextTo(this.activeId, true);
  }

  reopen() {
    const cwd = this.#recentlyClosed.pop();
    if (cwd !== undefined) this.open({ cwd });
  }

  close(id: number) {
    this.closeMany([id]);
  }

  closeActive() {
    if (this.activeId !== null) this.close(this.activeId);
  }

  closeOthers(id: number) {
    this.closeMany(this.list.filter((t) => t.id !== id).map((t) => t.id));
  }

  closeToTheRight(id: number) {
    this.closeMany(this.list.slice(this.#index(id) + 1).map((t) => t.id));
  }

  closeMany(ids: number[]) {
    const closing = new Set(ids);
    if (closing.size === 0) return;
    const activeIndex = this.#index(this.activeId);

    for (const tab of this.list) {
      if (!closing.has(tab.id)) continue;
      this.#recentlyClosed.push(focusedPane(tab).cwd);
      if (this.#recentlyClosed.length > MAX_REOPEN) this.#recentlyClosed.shift();
    }
    this.list = this.list.filter((t) => !closing.has(t.id));
    this.#pruneGroups();

    if (this.list.length === 0) {
      this.onEmpty();
      return;
    }
    if (this.activeId !== null && closing.has(this.activeId)) {
      // Like a browser: focus the tab that slid into this slot, else the one before.
      const next = this.#nearestVisible(Math.min(activeIndex, this.list.length - 1));
      if (next) this.activate(next.id);
      else this.open({ cwd: '' });
    }
  }

  // ── Focus ────────────────────────────────────────────────────────────

  activate(id: number) {
    const tab = this.get(id);
    if (!tab) return;
    const group = this.group(tab.groupId);
    if (group?.collapsed) group.collapsed = false;
    tab.activity = false;
    tab.finished = null;
    this.activeId = id;
  }

  /** Moves focus by `delta` visible tabs, wrapping around. */
  cycle(delta: number) {
    const visible = this.visible;
    const n = visible.length;
    if (n === 0) return;
    const i = visible.findIndex((t) => t.id === this.activeId);
    this.activate(visible[(i + delta + n) % n].id);
  }

  /** 1-based, matching ⌘1…⌘8. Counts visible tabs only. */
  selectNumber(n: number) {
    const tab = this.visible[n - 1];
    if (tab) this.activate(tab.id);
  }

  selectLast() {
    const tab = this.visible.at(-1);
    if (tab) this.activate(tab.id);
  }

  // ── Reordering ───────────────────────────────────────────────────────

  /** Moves a tab so it sits right before `beforeId` (or last when null). */
  moveBefore(id: number, beforeId: number | null) {
    const from = this.#index(id);
    if (from < 0 || beforeId === id) return;
    const [tab] = this.list.splice(from, 1);
    const to = beforeId === null ? this.list.length : this.#index(beforeId);
    this.list.splice(to < 0 ? this.list.length : to, 0, tab);
    this.#settleGroup(tab);
  }

  /** Keyboard move: one visible slot left or right. */
  moveActive(delta: number) {
    const tab = this.active;
    if (!tab) return;
    const visible = this.visible;
    const i = visible.findIndex((t) => t.id === tab.id);
    const j = i + delta;
    if (j < 0 || j >= visible.length) return;
    // Moving right lands after the neighbour, i.e. before the one after it.
    const before = delta > 0 ? (visible[j + 1]?.id ?? null) : visible[j].id;
    this.moveBefore(tab.id, before);
  }

  // ── Groups ───────────────────────────────────────────────────────────

  /** Puts a tab in a fresh group and returns the group. */
  createGroup(tabId: number): Group | undefined {
    const tab = this.get(tabId);
    if (!tab) return;
    const used = new Set(this.groups.map((g) => g.color));
    const color = GROUP_COLORS.find((c) => !used.has(c)) ?? GROUP_COLORS[this.groups.length % GROUP_COLORS.length];
    const group: Group = { id: this.#nextGroupId++, name: '', color, collapsed: false };
    this.groups.push(group);
    // Leaving the old group must not split it, so step out of it first.
    if (tab.groupId !== null) this.#moveAfterGroup(tab, tab.groupId);
    tab.groupId = group.id;
    this.#pruneGroups();
    return this.group(group.id);
  }

  addToGroup(tabId: number, groupId: number) {
    const tab = this.get(tabId);
    const members = this.tabsIn(groupId);
    if (!tab || members.length === 0 || tab.groupId === groupId) return;
    this.list.splice(this.#index(tab.id), 1);
    const last = this.#index(members.at(-1)!.id);
    this.list.splice(last + 1, 0, tab);
    tab.groupId = groupId;
    this.#pruneGroups();
    if (tab.id === this.activeId) this.group(groupId)!.collapsed = false;
  }

  removeFromGroup(tabId: number) {
    const tab = this.get(tabId);
    if (!tab || tab.groupId === null) return;
    this.#moveAfterGroup(tab, tab.groupId);
    tab.groupId = null;
    this.#pruneGroups();
  }

  ungroup(groupId: number) {
    for (const tab of this.list) if (tab.groupId === groupId) tab.groupId = null;
    this.#pruneGroups();
  }

  closeGroup(groupId: number) {
    this.closeMany(this.tabsIn(groupId).map((t) => t.id));
  }

  newTabInGroup(groupId: number) {
    const last = this.tabsIn(groupId).at(-1);
    if (last) this.openNextTo(last.id);
  }

  toggleCollapsed(groupId: number) {
    const group = this.group(groupId);
    if (!group) return;
    if (group.collapsed) {
      group.collapsed = false;
      return;
    }
    group.collapsed = true;
    // The active tab can't hide: hand focus to the nearest tab outside the group.
    if (this.active?.groupId === groupId) {
      const next = this.#nearestVisible(this.#index(this.activeId));
      if (next) this.activate(next.id);
      else this.open({ cwd: focusedPane(this.active).cwd });
    }
  }

  /** Names a tab; an empty name goes back to the automatic one. */
  rename(id: number, title: string) {
    const tab = this.get(id);
    if (tab) tab.customTitle = cleanTitle(title);
  }

  // ── Panes ────────────────────────────────────────────────────────────

  /**
   * Splits the focused pane of the active tab; the new pane gets focus. It
   * starts in the same folder unless `cwd` is given, and can run `command`.
   */
  split(dir: SplitDir, { cwd, command }: { cwd?: string; command?: string } = {}) {
    const tab = this.active;
    if (!tab) return;
    const from = focusedPane(tab);
    const pane = this.#newPane(cwd ?? from.cwd);
    if (command) pane.initialCommand = command;
    tab.zoomedPaneId = null;
    tab.root = splitPane(tab.root, from.id, dir, pane);
    tab.focusedPaneId = pane.id;
  }

  closePane(tabId: number, paneId: number) {
    const tab = this.get(tabId);
    if (!tab) return;
    const order = leaves(tab.root);
    const root = removePane(tab.root, paneId);
    if (!root) {
      this.close(tabId);
      return;
    }
    tab.root = root;
    if (tab.zoomedPaneId === paneId) tab.zoomedPaneId = null;
    if (tab.focusedPaneId === paneId) {
      // Focus the pane before the closed one in reading order, else the first.
      const i = order.findIndex((p) => p.id === paneId);
      const remaining = leaves(root);
      tab.focusedPaneId = (order.slice(0, i).reverse().find((p) => remaining.includes(p)) ?? remaining[0]).id;
    }
  }

  /** ⌘W: closes the focused pane, or the tab when it is the last one. */
  closeActivePane() {
    const tab = this.active;
    if (tab) this.closePane(tab.id, tab.focusedPaneId);
  }

  focusPane(tabId: number, paneId: number) {
    const tab = this.get(tabId);
    if (tab && tab.focusedPaneId !== paneId && findPane(tab.root, paneId)) tab.focusedPaneId = paneId;
  }

  focusDirection(dir: Direction) {
    const tab = this.active;
    if (!tab || tab.zoomedPaneId !== null) return;
    const next = neighbor(place(tab.root).panes, tab.focusedPaneId, dir);
    if (next) tab.focusedPaneId = next.id;
  }

  cyclePane(delta: number) {
    const tab = this.active;
    if (!tab || tab.zoomedPaneId !== null) return;
    const all = leaves(tab.root);
    const i = all.findIndex((p) => p.id === tab.focusedPaneId);
    tab.focusedPaneId = all[(i + delta + all.length) % all.length].id;
  }

  toggleZoom() {
    const tab = this.active;
    if (!tab) return;
    if (tab.zoomedPaneId !== null) tab.zoomedPaneId = null;
    else if (tab.root.kind === 'split') tab.zoomedPaneId = tab.focusedPaneId;
  }

  // ── Session plumbing ─────────────────────────────────────────────────

  setSession(paneId: number, sessionId: string) {
    const pane = this.#pane(paneId);
    if (pane) pane.sessionId = sessionId;
  }

  setOscTitle(paneId: number, title: string) {
    const pane = this.#pane(paneId);
    if (pane) pane.oscTitle = title;
  }

  markIntegrated(paneId: number) {
    const pane = this.#pane(paneId);
    if (pane && !pane.integrated) pane.integrated = true;
  }

  commandStarted(paneId: number, text: string) {
    const pane = this.#pane(paneId);
    if (!pane) return;
    pane.running = true;
    pane.command = text;
  }

  commandFinished(paneId: number, exitCode: number, durationMs: number) {
    const pane = this.#pane(paneId);
    if (!pane) return;
    pane.running = false;
    pane.lastCommand = { text: pane.command, exitCode, durationMs };
    const tab = this.list.find((t) => findPane(t.root, paneId));
    if (tab && tab.id !== this.activeId) tab.finished = exitCode === 0 ? 'ok' : 'fail';
  }

  setInfra(paneId: number, ctx: InfraContext | null) {
    const pane = this.#pane(paneId);
    if (pane) pane.infra = ctx;
  }

  markActivity(id: number) {
    const tab = this.get(id);
    if (tab && id !== this.activeId && !tab.activity) tab.activity = true;
  }

  applyInfo(info: Info) {
    const found = this.bySession(info.id);
    if (!found) return;
    const { pane } = found;
    if (pane.process !== info.process) pane.oscTitle = '';
    pane.process = info.process;
    pane.isShell = info.isShell;
    if (info.cwd) pane.cwd = info.cwd;
  }

  onSessionExit(sessionId: string) {
    const found = this.bySession(sessionId);
    if (found) this.closePane(found.tab.id, found.pane.id);
  }

  bySession(sessionId: string): { tab: Tab; pane: Pane } | undefined {
    for (const tab of this.list) {
      const pane = leaves(tab.root).find((p) => p.sessionId === sessionId);
      if (pane) return { tab, pane };
    }
    return undefined;
  }

  // ── Persistence ──────────────────────────────────────────────────────

  #saveTab(t: Tab, pane: (p: Pane) => SavedNode = (p) => ({ cwd: p.cwd })) {
    const save = (n: LayoutNode): SavedNode =>
      n.kind === 'pane' ? pane(n.pane) : { dir: n.dir, ratio: n.ratio, a: save(n.a), b: save(n.b) };
    const ids = leaves(t.root).map((p) => p.id);
    return {
      groupId: t.groupId,
      root: save(t.root),
      focused: Math.max(0, ids.indexOf(t.focusedPaneId)),
      zoomed: t.zoomedPaneId === null ? null : ids.indexOf(t.zoomedPaneId),
      title: t.customTitle,
    };
  }

  snapshot(): SavedTabs {
    return {
      tabs: this.list.map((t) => this.#saveTab(t)),
      groups: this.groups.map((g) => ({ ...g })),
      active: Math.max(0, this.list.findIndex((t) => t.id === this.activeId)),
    };
  }

  /**
   * Rebuilds tabs from a snapshot (shells start fresh in their old folders).
   * Untrusted input: anything malformed is skipped. Returns false when
   * nothing usable was found.
   */
  restore(data: unknown): boolean {
    if (!isObject(data) || !Array.isArray(data.tabs)) return false;

    const groupIds = new Map<number, number>();
    const groups: Group[] = [];
    for (const g of Array.isArray(data.groups) ? data.groups : []) {
      if (!isObject(g) || typeof g.id !== 'number' || groupIds.has(g.id)) continue;
      const id = this.#nextGroupId++;
      groupIds.set(g.id, id);
      groups.push({
        id,
        name: typeof g.name === 'string' ? g.name.slice(0, 100) : '',
        color: GROUP_COLORS.includes(g.color as GroupColor) ? (g.color as GroupColor) : 'blue',
        collapsed: g.collapsed === true,
      });
    }

    const list: Tab[] = [];
    for (const t of data.tabs) {
      if (!isObject(t)) continue;
      const root = this.#load(t.root, 0);
      if (!root) continue;
      const ids = leaves(root).map((p) => p.id);
      const at = (i: unknown) => (typeof i === 'number' ? ids[i] : undefined);
      list.push({
        id: this.#nextId++,
        root,
        focusedPaneId: at(t.focused) ?? ids[0],
        zoomedPaneId: root.kind === 'split' ? (at(t.zoomed) ?? null) : null,
        activity: false,
        finished: null,
        groupId: (typeof t.groupId === 'number' && groupIds.get(t.groupId)) || null,
        customTitle: cleanTitle(t.title),
      });
    }
    if (list.length === 0) return false;

    // Re-establish the contiguity invariant in case the file was edited.
    const ordered: Tab[] = [];
    const placed = new Set<number>();
    for (const tab of list) {
      if (tab.groupId === null) ordered.push(tab);
      else if (!placed.has(tab.groupId)) {
        placed.add(tab.groupId);
        ordered.push(...list.filter((x) => x.groupId === tab.groupId));
      }
    }

    this.list = ordered;
    this.groups = groups;
    this.#pruneGroups();
    const active = typeof data.active === 'number' ? list[data.active] : undefined;
    this.activate((active ?? ordered[0]).id); // expands its group if collapsed
    return true;
  }

  /**
   * Removes a tab without ending its shells, returning everything another
   * window needs to show it: layout, sessions, scrollback (from `buffer`).
   */
  detach(id: number, buffer: (paneId: number) => string): DetachedTab | undefined {
    const tab = this.get(id);
    if (!tab) return;
    const group = this.group(tab.groupId);
    const saved = this.#saveTab(tab, (p) => ({ cwd: p.cwd, sessionId: p.sessionId ?? undefined, buffer: buffer(p.id) }));
    for (const p of leaves(tab.root)) if (p.sessionId) detachedSessions.add(p.sessionId);
    this.#forget(id);
    return { kind: 'tab', tab: { ...saved, groupId: null }, group: group ? { name: group.name, color: group.color } : null };
  }

  /** Takes in a tab detached by another window; it joins the end, focused. */
  adopt(data: unknown): boolean {
    if (!isObject(data) || data.kind !== 'tab' || !isObject(data.tab)) return false;
    const t = data.tab;
    const root = this.#load(t.root, 0, true);
    if (!root) return false;
    const ids = leaves(root).map((p) => p.id);
    let groupId: number | null = null;
    if (isObject(data.group)) {
      const g = data.group;
      const name = typeof g.name === 'string' ? g.name.slice(0, 100) : '';
      const color = GROUP_COLORS.includes(g.color as GroupColor) ? (g.color as GroupColor) : 'blue';
      // Rejoin a group of the same name and color if this window has one.
      const existing = this.groups.find((x) => x.name === name && x.color === color && name !== '');
      if (existing) groupId = existing.id;
      else {
        groupId = this.#nextGroupId++;
        this.groups.push({ id: groupId, name, color, collapsed: false });
      }
    }
    const tab: Tab = {
      id: this.#nextId++,
      root,
      focusedPaneId: (typeof t.focused === 'number' && ids[t.focused]) || ids[0],
      zoomedPaneId: null,
      activity: false,
      finished: null,
      groupId,
      customTitle: cleanTitle(t.title),
    };
    const last = groupId === null ? -1 : this.list.findLastIndex((x) => x.groupId === groupId);
    this.list.splice(last < 0 ? this.list.length : last + 1, 0, tab);
    this.activate(tab.id);
    return true;
  }

  /** Drops a tab from this window without recording it as closed. */
  #forget(id: number) {
    const i = this.#index(id);
    if (i < 0) return;
    const wasActive = this.activeId === id;
    this.list.splice(i, 1);
    this.#pruneGroups();
    if (this.list.length === 0) {
      this.onEmpty();
      return;
    }
    if (wasActive) {
      const next = this.#nearestVisible(Math.min(i, this.list.length - 1));
      if (next) this.activate(next.id);
      else this.open({ cwd: '' });
    }
  }

  #load(n: unknown, depth: number, live = false): LayoutNode | null {
    if (!isObject(n) || depth > MAX_SAVED_DEPTH) return null;
    if (typeof n.cwd === 'string') {
      const pane = this.#newPane(n.cwd);
      if (live && typeof n.sessionId === 'string') {
        pane.sessionId = n.sessionId;
        if (typeof n.buffer === 'string') pane.restoreBuffer = n.buffer;
      }
      return { kind: 'pane', pane };
    }
    if (n.dir !== 'row' && n.dir !== 'column') return null;
    const a = this.#load(n.a, depth + 1, live);
    const b = this.#load(n.b, depth + 1, live);
    if (!a || !b) return a ?? b;
    const ratio = typeof n.ratio === 'number' && Number.isFinite(n.ratio) ? clampRatio(n.ratio) : 0.5;
    return { kind: 'split', dir: n.dir, ratio, a, b };
  }

  // ── Internals ────────────────────────────────────────────────────────

  #newPane(cwd: string): Pane {
    return {
      id: this.#nextPaneId++,
      sessionId: null,
      initialCwd: cwd,
      cwd,
      process: '',
      isShell: true,
      oscTitle: '',
      integrated: false,
      running: false,
      command: '',
      lastCommand: null,
      infra: null,
    };
  }

  #pane(id: number): Pane | undefined {
    for (const tab of this.list) {
      const pane = findPane(tab.root, id);
      if (pane) return pane;
    }
    return undefined;
  }

  #index(id: number | null): number {
    return this.list.findIndex((t) => t.id === id);
  }

  /** The visible tab closest to `index`, preferring the right-hand side. */
  #nearestVisible(index: number): Tab | undefined {
    for (let d = 0; d <= this.list.length; d++) {
      for (const i of d === 0 ? [index] : [index + d, index - d]) {
        const t = this.list[i];
        if (t && !this.isHidden(t) && t.id !== this.activeId) return t;
      }
    }
    return undefined;
  }

  #moveAfterGroup(tab: Tab, groupId: number) {
    this.list.splice(this.#index(tab.id), 1);
    const last = this.list.findLastIndex((t) => t.groupId === groupId);
    this.list.splice(last + 1, 0, tab);
  }

  /**
   * After a tab moved, decide which group it belongs to from where it landed:
   * between two members of an open group → joins it; next to its own group
   * (or still its group's only tab) → stays; anywhere else → ungrouped.
   */
  #settleGroup(tab: Tab) {
    const i = this.#index(tab.id);
    const openGroupOf = (t: Tab | undefined) =>
      t && t.groupId !== null && !this.group(t.groupId)?.collapsed ? t.groupId : null;
    const left = openGroupOf(this.list[i - 1]);
    const right = openGroupOf(this.list[i + 1]);
    const own = tab.groupId;

    if (left !== null && left === right) {
      tab.groupId = left;
    } else if (own !== null && (left === own || right === own)) {
      // stays
    } else if (own !== null && this.tabsIn(own).length === 1) {
      // A one-tab group travels with its tab.
    } else {
      tab.groupId = null;
    }
    this.#pruneGroups();
  }

  #pruneGroups() {
    const live = new Set(this.list.map((t) => t.groupId));
    if (this.groups.some((g) => !live.has(g.id))) {
      this.groups = this.groups.filter((g) => live.has(g.id));
    }
  }
}

function isObject(x: unknown): x is Record<string, unknown> {
  return typeof x === 'object' && x !== null;
}

export const tabs = new Tabs();

/** What a tab is called: the name the user gave it, or its focused pane's title. */
export function tabTitle(tab: Tab, pretty: (p: string) => string): string {
  return tab.customTitle ?? paneTitle(focusedPane(tab), pretty);
}

export function focusedPane(tab: Tab): Pane {
  return findPane(tab.root, tab.focusedPaneId) ?? leaves(tab.root)[0];
}

/** What a pane is called: the program when one is running, otherwise the folder. */
export function paneTitle(pane: Pane, pretty: (p: string) => string): string {
  if (!pane.isShell && pane.process) {
    // "ssh web1" says more than "ssh" when the remote doesn't set a title.
    const ssh = pane.process === 'ssh' && pane.running && pane.command.startsWith('ssh ') ? pane.command : '';
    return pane.oscTitle || ssh || pane.process;
  }
  const path = pretty(pane.cwd);
  if (!path) return 'Terminal';
  if (path === '~' || path === '/') return path;
  return path.slice(path.lastIndexOf('/') + 1);
}
