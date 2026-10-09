import { beforeEach, describe, expect, it } from 'vitest';
import { broadcast, isTerminalReport } from './broadcast.svelte';
import { leaves } from './layout';
import { tabs } from './tabs.svelte';

const paneIds = (tabId: number) => leaves(tabs.get(tabId)!.root).map((p) => p.id);

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  broadcast.stopAll();
});

describe('broadcast', () => {
  it('sends to every pane of every tab in a group, and nowhere else', () => {
    const a = tabs.open({ cwd: '/' });
    tabs.split('row');
    const b = tabs.open({ cwd: '/' });
    const outside = tabs.open({ cwd: '/' });
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(b, g.id);

    expect(broadcast.targets(paneIds(a)[0])).toEqual([]);
    broadcast.toggleGroup(g.id);
    expect(broadcast.targets(paneIds(a)[0])).toEqual([paneIds(a)[1], paneIds(b)[0]]);
    expect(broadcast.targets(paneIds(b)[0])).toEqual(paneIds(a));
    expect(broadcast.targets(paneIds(outside)[0])).toEqual([]);
    expect(broadcast.includes(paneIds(outside)[0])).toBe(false);
    expect(broadcast.describe()).toBe('2 tabs in a group');
  });

  it('toggles for the active tab’s group, else its splits, else says no', () => {
    tabs.open({ cwd: '/' });
    expect(broadcast.toggleActive()).toBe(false);

    tabs.split('column');
    expect(broadcast.toggleActive()).toBe(true);
    expect(broadcast.tabs.has(tabs.activeId!)).toBe(true);
    expect(broadcast.describe()).toBe('2 panes of a tab');
    broadcast.toggleActive(); // any broadcast on: the toggle stops it
    expect(broadcast.on).toBe(false);

    const t = tabs.activeId!;
    const other = tabs.open({ cwd: '/' });
    const g = tabs.createGroup(t)!;
    g.name = 'prod';
    tabs.addToGroup(other, g.id);
    expect(broadcast.toggleActive()).toBe(true);
    expect(broadcast.groups.has(g.id)).toBe(true);
    expect(broadcast.describe()).toBe('2 tabs in “prod”');
  });

  it('forgets groups and tabs that are gone', () => {
    const a = tabs.open({ cwd: '/' });
    const b = tabs.open({ cwd: '/' });
    const g = tabs.createGroup(a)!;
    tabs.addToGroup(b, g.id);
    broadcast.toggleGroup(g.id);
    tabs.ungroup(g.id);
    broadcast.prune();
    expect(broadcast.on).toBe(false);
  });

  it('never broadcasts replies the terminal sends for the program', () => {
    for (const reply of ['\x1b[12;40R', '\x1b[?1;2c', '\x1b[>0;276;0c', '\x1b[I', '\x1b[O', '\x1b[<0;10;5M', '\x1b[M !!', '\x1b]11;rgb:0000/0000/0000\x1b\\', '\x1bP1$r0m\x1b\\', '\x1b[?2004;1$y']) {
      expect(isTerminalReport(reply), JSON.stringify(reply)).toBe(true);
    }
    for (const typed of ['ls -la\r', 'a', '\x1b[A', '\x1b[1;5D', '\x03', '\x1b[200~pasted\x1b[201~', '\x1bOP']) {
      expect(isTerminalReport(typed), JSON.stringify(typed)).toBe(false);
    }
  });
});
