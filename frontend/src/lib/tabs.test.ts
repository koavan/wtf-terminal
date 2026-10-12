import { beforeEach, describe, expect, it, vi } from 'vitest';

const { tabs, focusedPane, detachedSessions } = await import('./tabs.svelte');
const closeWindow = vi.fn();
tabs.onEmpty = closeWindow;
const { leaves } = await import('./layout');

function reset() {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  closeWindow.mockClear();
}

/** Opens n tabs and returns their ids in order. */
function openTabs(n: number): number[] {
  for (let i = 0; i < n; i++) tabs.open({ cwd: `/t${i}` });
  return tabs.list.map((t) => t.id);
}

const ids = () => tabs.list.map((t) => t.id);
const groupOf = (id: number) => tabs.get(id)!.groupId;

/** The rules every operation must preserve. */
function checkInvariants() {
  // Group members are contiguous.
  const seen = new Set<number>();
  let prev: number | null = null;
  for (const t of tabs.list) {
    if (t.groupId !== null && t.groupId !== prev) {
      expect(seen.has(t.groupId), `group ${t.groupId} is split: ${JSON.stringify(tabs.list.map((x) => x.groupId))}`).toBe(false);
      seen.add(t.groupId);
    }
    prev = t.groupId;
  }
  // Groups exist exactly when they have tabs.
  const used = new Set(tabs.list.map((t) => t.groupId).filter((g) => g !== null));
  expect(new Set(tabs.groups.map((g) => g.id))).toEqual(used);
  // Every tab focuses one of its own panes; zoom points at a real pane.
  for (const t of tabs.list) {
    const ids = leaves(t.root).map((p) => p.id);
    expect(ids).toContain(t.focusedPaneId);
    if (t.zoomedPaneId !== null) expect(ids).toContain(t.zoomedPaneId);
  }
  // The active tab exists and is not hidden in a collapsed group.
  if (tabs.list.length > 0) {
    const active = tabs.active;
    expect(active, 'active tab exists').toBeDefined();
    expect(tabs.isHidden(active!), 'active tab is visible').toBe(false);
  }
}

beforeEach(reset);

describe('groups', () => {
  it('creates a group around a tab with the first unused color', () => {
    const [a, b] = openTabs(2);
    const g1 = tabs.createGroup(a)!;
    const g2 = tabs.createGroup(b)!;
    expect(g1.color).toBe('blue');
    expect(g2.color).toBe('red');
    expect(groupOf(a)).toBe(g1.id);
    checkInvariants();
  });

  it('adding to a group moves the tab next to its members', () => {
    const [a, b, c] = openTabs(3);
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(c, g.id);
    expect(ids()).toEqual([a, c, b]);
    expect(groupOf(c)).toBe(g.id);
    checkInvariants();
  });

  it('removing from the middle of a group steps out after it', () => {
    const [a, b, c, d] = openTabs(4);
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(b, g.id);
    tabs.addToGroup(c, g.id);
    tabs.removeFromGroup(b);
    expect(ids()).toEqual([a, c, b, d]);
    expect(groupOf(b)).toBeNull();
    checkInvariants();
  });

  it('dragging between two members joins the group; dragging away leaves it', () => {
    const [a, b, c, d] = openTabs(4);
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(b, g.id);
    tabs.moveBefore(d, b); // a d b c
    expect(groupOf(d)).toBe(g.id);
    tabs.moveBefore(d, null); // a b c d
    expect(groupOf(d)).toBeNull();
    checkInvariants();
  });

  it('a one-tab group travels with its tab', () => {
    const [a, , c] = openTabs(3);
    const g = tabs.createGroup(a)!;
    tabs.moveBefore(a, null);
    expect(ids().at(-1)).toBe(a);
    expect(groupOf(a)).toBe(g.id);
    tabs.moveBefore(a, c);
    expect(groupOf(a)).toBe(g.id);
    checkInvariants();
  });

  it('collapsing the active group focuses the nearest tab outside it', () => {
    const [a, b, c] = openTabs(3);
    const g = tabs.createGroup(b)!;
    tabs.activate(b);
    tabs.toggleCollapsed(g.id);
    expect(tabs.activeId).toBe(c);
    expect(tabs.visible.map((t) => t.id)).toEqual([a, c]);
    checkInvariants();
  });

  it('collapsing the only visible tabs opens a fresh tab', () => {
    const [a] = openTabs(1);
    const g = tabs.createGroup(a)!;
    tabs.toggleCollapsed(g.id);
    expect(tabs.list).toHaveLength(2);
    expect(tabs.activeId).not.toBe(a);
    checkInvariants();
  });

  it('activating a hidden tab expands its group', () => {
    const [a, b] = openTabs(2);
    const g = tabs.createGroup(a)!;
    tabs.activate(b);
    tabs.toggleCollapsed(g.id);
    tabs.activate(a);
    expect(tabs.group(g.id)!.collapsed).toBe(false);
  });

  it('closing a group closes its tabs; ungrouping keeps them', () => {
    const [a, b, c] = openTabs(3);
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(b, g.id);
    tabs.ungroup(g.id);
    expect(ids()).toEqual([a, b, c]);
    expect(tabs.groups).toHaveLength(0);

    const g2 = tabs.createGroup(a)!;
    tabs.addToGroup(b, g2.id);
    tabs.closeGroup(g2.id);
    expect(ids()).toEqual([c]);
    checkInvariants();
  });

  it('closing the last tab closes the window', () => {
    const [a] = openTabs(1);
    tabs.close(a);
    expect(closeWindow).toHaveBeenCalledOnce();
  });

  it('new tab in a group opens after its last member', () => {
    const [a, b, c] = openTabs(3);
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(b, g.id);
    tabs.newTabInGroup(g.id);
    const added = tabs.activeId!;
    expect(ids()).toEqual([a, b, added, c]);
    expect(groupOf(added)).toBe(g.id);
  });
});

describe('keyboard navigation', () => {
  it('cycles and numbers only visible tabs', () => {
    const [a, b, c] = openTabs(3);
    const g = tabs.createGroup(b)!;
    tabs.activate(a);
    tabs.toggleCollapsed(g.id);
    tabs.cycle(1);
    expect(tabs.activeId).toBe(c);
    tabs.selectNumber(1);
    expect(tabs.activeId).toBe(a);
    tabs.selectLast();
    expect(tabs.activeId).toBe(c);
  });

  it('moves the active tab one slot at a time', () => {
    const [a, b, c] = openTabs(3);
    tabs.activate(a);
    tabs.moveActive(1);
    expect(ids()).toEqual([b, a, c]);
    tabs.moveActive(1);
    expect(ids()).toEqual([b, c, a]);
    tabs.moveActive(1); // already last
    expect(ids()).toEqual([b, c, a]);
    tabs.moveActive(-1);
    expect(ids()).toEqual([b, a, c]);
  });
});

// Throws thousands of random operations at the store and checks the
// invariants after each one.
describe('random operations keep the invariants', () => {
  it.each([1, 2, 3, 4, 5])('seed %i', (seed) => {
    let state = seed * 2654435761;
    const rand = (n: number) => {
      state = (state * 1103515245 + 12345) & 0x7fffffff;
      return state % n;
    };
    const pick = <T>(xs: T[]): T | undefined => (xs.length ? xs[rand(xs.length)] : undefined);

    openTabs(4);
    for (let step = 0; step < 2000; step++) {
      const tab = pick(tabs.list);
      const visibleTab = pick(tabs.visible);
      const group = pick(tabs.groups);
      const op = rand(18);
      if (tabs.list.length === 0) openTabs(2);

      switch (op) {
        case 0: tabs.open(); break;
        case 1: if (tab && tabs.list.length > 1) tabs.close(tab.id); break;
        case 2: if (tab) tabs.createGroup(tab.id); break;
        case 3: if (tab && group) tabs.addToGroup(tab.id, group.id); break;
        case 4: if (tab) tabs.removeFromGroup(tab.id); break;
        case 5: if (group) tabs.toggleCollapsed(group.id); break;
        case 6: if (group && tabs.list.length > tabs.tabsIn(group.id).length) tabs.ungroup(group.id); break;
        case 7: if (visibleTab) tabs.moveBefore(visibleTab.id, pick(tabs.visible)?.id ?? null); break;
        case 8: tabs.moveActive(rand(2) ? 1 : -1); break;
        case 9: if (tab) tabs.activate(tab.id); break;
        case 10: tabs.cycle(rand(2) ? 1 : -1); break;
        case 11: if (group) tabs.newTabInGroup(group.id); break;
        case 12: tabs.duplicate(); break;
        case 13: tabs.split(rand(2) ? 'row' : 'column'); break;
        case 14: tabs.closeActivePane(); if (tabs.list.length === 0) openTabs(2); break;
        case 15: tabs.focusDirection((['left', 'right', 'up', 'down'] as const)[rand(4)]); break;
        case 16: tabs.toggleZoom(); break;
        case 17: tabs.cyclePane(1); break;
      }
      checkInvariants();
      if (tabs.list.length > 30) tabs.closeMany(tabs.list.slice(0, 15).map((t) => t.id));
    }
  });
});

describe('panes', () => {
  const paneIds = () => leaves(tabs.active!.root).map((p) => p.id);

  it('splits the focused pane and focuses the new one in the same folder', () => {
    tabs.open({ cwd: '/work' });
    const first = tabs.activePane!.id;
    tabs.split('row');
    expect(paneIds()).toHaveLength(2);
    expect(tabs.activePane!.id).not.toBe(first);
    expect(tabs.activePane!.initialCwd).toBe('/work');
  });

  it('moves focus by direction in a 2x2 grid', () => {
    tabs.open();
    const tl = tabs.activePane!.id;
    tabs.split('row');
    const tr = tabs.activePane!.id;
    tabs.split('column');
    const br = tabs.activePane!.id;
    tabs.focusPane(tabs.activeId!, tl);
    tabs.split('column');
    const bl = tabs.activePane!.id;

    tabs.focusDirection('right');
    expect(tabs.activePane!.id).toBe(br);
    tabs.focusDirection('up');
    expect(tabs.activePane!.id).toBe(tr);
    tabs.focusDirection('left');
    expect(tabs.activePane!.id).toBe(tl);
    tabs.focusDirection('down');
    expect(tabs.activePane!.id).toBe(bl);
    tabs.focusDirection('down'); // edge: stays
    expect(tabs.activePane!.id).toBe(bl);
  });

  it('closing a pane gives its space to the sibling and focus to the previous pane', () => {
    tabs.open();
    const a = tabs.activePane!.id;
    tabs.split('row');
    const b = tabs.activePane!.id;
    tabs.split('column');
    const c = tabs.activePane!.id;
    tabs.closeActivePane();
    expect(paneIds()).toEqual([a, b]);
    expect(tabs.activePane!.id).toBe(b);
    expect(tabs.active!.root.kind).toBe('split');
  });

  it('closing the last pane closes the tab', () => {
    const [a, b] = openTabs(2);
    tabs.activate(b);
    tabs.closeActivePane();
    expect(ids()).toEqual([a]);
  });

  it("a pane's shell exiting closes just that pane", () => {
    tabs.open();
    const a = tabs.activePane!;
    tabs.setSession(a.id, 's1');
    tabs.split('row');
    tabs.setSession(tabs.activePane!.id, 's2');
    tabs.onSessionExit('s1');
    expect(paneIds()).toHaveLength(1);
    expect(focusedPane(tabs.active!).sessionId).toBe('s2');
  });

  it('zoom only applies to split tabs and clears when its pane closes', () => {
    tabs.open();
    tabs.toggleZoom();
    expect(tabs.active!.zoomedPaneId).toBeNull();
    tabs.split('row');
    tabs.toggleZoom();
    expect(tabs.active!.zoomedPaneId).toBe(tabs.activePane!.id);
    tabs.closeActivePane();
    expect(tabs.active!.zoomedPaneId).toBeNull();
  });

  it('routes process info to the right pane', () => {
    tabs.open();
    const a = tabs.activePane!;
    tabs.setSession(a.id, 's9');
    tabs.split('row');
    tabs.applyInfo({ id: 's9', process: 'vim', cwd: '/src', isShell: false });
    expect(a.process).toBe('vim');
    expect(a.cwd).toBe('/src');
    expect(tabs.activePane!.process).toBe('');
  });
});

describe('session restore', () => {
  // Group ids are internal and change on restore; compare by position instead.
  const normalized = (snap: ReturnType<typeof tabs.snapshot>) => {
    const pos = new Map(snap.groups.map((g, i) => [g.id, i]));
    return {
      ...snap,
      groups: snap.groups.map(({ id: _id, ...g }) => g),
      tabs: snap.tabs.map((t) => ({ ...t, groupId: t.groupId === null ? null : pos.get(t.groupId) })),
    };
  };

  it('round-trips tabs, groups, layouts, folders and focus', () => {
    const [a, b, c] = openTabs(3);
    const g = tabs.createGroup(a)!;
    g.name = 'api';
    g.color = 'green';
    tabs.addToGroup(c, g.id); // a c b
    tabs.activate(c);
    tabs.split('row');
    tabs.split('column');
    tabs.active!.root.kind === 'split' && (tabs.active!.root.ratio = 0.3);
    tabs.activate(b);
    const before = tabs.snapshot();

    reset();
    expect(tabs.restore(JSON.parse(JSON.stringify(before)))).toBe(true);
    expect(normalized(tabs.snapshot())).toEqual(normalized(before));
    expect(tabs.groups[0]).toMatchObject({ name: 'api', color: 'green', collapsed: false });
    // Restored panes start their shells in the saved folders.
    expect(leaves(tabs.list[1].root).map((p) => p.initialCwd)).toEqual(['/t2', '/t2', '/t2']);
    checkInvariants();
  });

  it('keeps a pane on its git identity through split, duplicate and restore', () => {
    const [a] = openTabs(1);
    tabs.setEnv(focusedPane(tabs.get(a)!).id, { PWD: '/t0', WTF_GIT_IDENTITY: 'work' });
    tabs.split('row');
    expect(focusedPane(tabs.active!).gitIdentity).toBe('work');
    tabs.duplicate();
    expect(focusedPane(tabs.active!).gitIdentity).toBe('work');
    tabs.open({ cwd: '/plain' }); // a new tab doesn't inherit it
    expect(focusedPane(tabs.active!).gitIdentity).toBeUndefined();

    const snap = tabs.snapshot();
    // The split pane hasn't reported yet: it's saved with the identity it's getting.
    expect(snap.tabs[0].root).toMatchObject({ a: { cwd: '/t0', gitIdentity: 'work' }, b: { cwd: '/t0', gitIdentity: 'work' } });
    expect(snap.tabs[2].root).toEqual({ cwd: '/plain' });
    reset();
    tabs.restore(JSON.parse(JSON.stringify(snap)));
    expect(leaves(tabs.list[0].root).map((p) => p.gitIdentity)).toEqual(['work', 'work']);
    expect(leaves(tabs.list[2].root).map((p) => p.gitIdentity)).toEqual([undefined]);
  });

  it('expands a collapsed group if its tab was active', () => {
    const [a] = openTabs(2);
    const g = tabs.createGroup(a)!;
    const snap = tabs.snapshot();
    snap.groups[0].collapsed = true;
    snap.active = 0;
    reset();
    tabs.restore(snap);
    expect(tabs.groups[0].collapsed).toBe(false);
    expect(g).toBeDefined();
    checkInvariants();
  });

  it('rejects garbage and skips malformed parts', () => {
    expect(tabs.restore(null)).toBe(false);
    expect(tabs.restore({ tabs: 'nope' })).toBe(false);
    expect(tabs.restore({ tabs: [{ root: { dir: 'row' } }] })).toBe(false);

    const ok = tabs.restore({
      tabs: [
        { root: { cwd: '/x' }, groupId: 7, focused: 99, zoomed: 0 },
        { root: 'bad' },
        { root: { dir: 'row', ratio: 42, a: { cwd: '/l' }, b: { nonsense: true } } },
        { root: { cwd: '/y' }, groupId: 7 },
      ],
      groups: [{ id: 7, name: 'g', color: 'chartreuse', collapsed: 'yes' }],
      active: 0,
    });
    expect(ok).toBe(true);
    expect(tabs.list).toHaveLength(3);
    expect(tabs.groups[0]).toMatchObject({ color: 'blue', collapsed: false });
    // A split with one bad child collapses to the good child.
    expect(tabs.list.map((t) => t.root.kind)).toEqual(['pane', 'pane', 'pane']);
    checkInvariants();
  });

  it('regroups scattered members so groups stay contiguous', () => {
    tabs.restore({
      tabs: [{ root: { cwd: '/1' }, groupId: 1 }, { root: { cwd: '/2' } }, { root: { cwd: '/3' }, groupId: 1 }],
      groups: [{ id: 1, name: '', color: 'red', collapsed: false }],
      active: 0,
    });
    expect(tabs.list.map((t) => focusedPane(t).cwd)).toEqual(['/1', '/3', '/2']);
    checkInvariants();
  });
});

describe('moving tabs between windows', () => {
  it('detaches a tab with its sessions and scrollback, and another window adopts it', () => {
    const [a, b] = openTabs(2);
    const g = tabs.createGroup(b)!;
    g.name = 'api';
    tabs.activate(b);
    tabs.setSession(tabs.activePane!.id, 's1');
    tabs.split('row');
    tabs.setSession(tabs.activePane!.id, 's2');

    const payload = tabs.detach(b, (paneId) => `buffer-${paneId}`)!;
    expect(ids()).toEqual([a]);
    expect(tabs.activeId).toBe(a);
    expect(tabs.groups).toHaveLength(0);
    expect([...detachedSessions].sort()).toEqual(['s1', 's2']);
    expect(closeWindow).not.toHaveBeenCalled();

    // The other window.
    reset();
    detachedSessions.clear();
    openTabs(1);
    expect(tabs.adopt(JSON.parse(JSON.stringify(payload)))).toBe(true);
    const adopted = tabs.active!;
    const panes = leaves(adopted.root);
    expect(panes.map((p) => p.sessionId)).toEqual(['s1', 's2']);
    expect(panes.every((p) => p.restoreBuffer?.startsWith('buffer-'))).toBe(true);
    expect(tabs.group(adopted.groupId)).toMatchObject({ name: 'api', color: 'blue' });
    expect(focusedPane(adopted).sessionId).toBe('s2');
    checkInvariants();
  });

  it('joins an existing group with the same name and color', () => {
    const [a] = openTabs(1);
    const g = tabs.createGroup(a)!;
    g.name = 'api';
    openTabs(1);
    tabs.adopt({ kind: 'tab', tab: { root: { cwd: '/x', sessionId: 's9' }, focused: 0 }, group: { name: 'api', color: 'blue' } });
    expect(tabs.groups).toHaveLength(1);
    expect(tabs.tabsIn(g.id)).toHaveLength(2);
    checkInvariants();
  });

  it('detaching the last tab empties the window', () => {
    const [a] = openTabs(1);
    tabs.detach(a, () => '');
    expect(closeWindow).toHaveBeenCalledOnce();
  });

  it('saved sessions never carry session ids into a restore', () => {
    tabs.restore({ tabs: [{ root: { cwd: '/x', sessionId: 'stale', buffer: 'x' } }], groups: [], active: 0 });
    expect(focusedPane(tabs.active!).sessionId).toBeNull();
    expect(focusedPane(tabs.active!).restoreBuffer).toBeUndefined();
  });

  it('rejects malformed adoptions', () => {
    openTabs(1);
    expect(tabs.adopt(null)).toBe(false);
    expect(tabs.adopt({ kind: 'tab', tab: { root: 'x' } })).toBe(false);
    expect(tabs.adopt({ kind: 'other' })).toBe(false);
    expect(tabs.list).toHaveLength(1);
  });
});

describe('renaming', () => {
  it('a custom name replaces the automatic one until cleared', async () => {
    const { tabTitle } = await import('./tabs.svelte');
    const [a] = openTabs(1);
    const tab = tabs.get(a)!;
    expect(tabTitle(tab, (p) => p)).toBe('t0');
    tabs.rename(a, '  api   server  ');
    expect(tabTitle(tab, (p) => p)).toBe('api server');
    focusedPane(tab).cwd = '/elsewhere';
    expect(tabTitle(tab, (p) => p)).toBe('api server'); // sticks across cd
    tabs.rename(a, '   ');
    expect(tab.customTitle).toBeNull();
    expect(tabTitle(tab, (p) => p)).toBe('elsewhere');
  });

  it('caps the length', () => {
    const [a] = openTabs(1);
    tabs.rename(a, 'x'.repeat(200));
    expect(tabs.get(a)!.customTitle).toHaveLength(60);
  });

  it('survives session restore and moving to another window', () => {
    const [a] = openTabs(2);
    tabs.rename(a, 'db');
    const snap = JSON.parse(JSON.stringify(tabs.snapshot()));
    const moved = tabs.detach(a, () => '')!;
    reset();
    tabs.restore(snap);
    expect(tabs.list[0].customTitle).toBe('db');
    expect(tabs.list[1].customTitle).toBeNull();
    reset();
    openTabs(1);
    tabs.adopt(JSON.parse(JSON.stringify(moved)));
    expect(tabs.active!.customTitle).toBe('db');
  });

  it('ignores junk titles in a saved session', () => {
    tabs.restore({ tabs: [{ root: { cwd: '/x' }, title: 42 }, { root: { cwd: '/y' }, title: '' }], groups: [], active: 0 });
    expect(tabs.list.map((t) => t.customTitle)).toEqual([null, null]);
  });
});
