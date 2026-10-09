import { SvelteSet } from 'svelte/reactivity';
import { leaves } from './layout';
import { tabs, type Tab } from './tabs.svelte';

// Broadcast input: what's typed into one pane is also sent to the others
// of its tab group (every pane of every tab) or of its tab (its splits) —
// for running the same commands on a fleet of hosts. It never survives a
// restart, and while it's on the window says so loudly (BroadcastBar).

class Broadcast {
  /** Tab groups whose panes all receive what's typed in any of them. */
  groups = new SvelteSet<number>();
  /** Tabs whose split panes all receive what's typed in any of them. */
  tabs = new SvelteSet<number>();

  /** Cheap check for the typing path: is anything broadcasting at all? */
  get on(): boolean {
    return this.groups.size > 0 || this.tabs.size > 0;
  }

  /** The tabs input typed into `tab` reaches, itself included; [] when not broadcasting. */
  scope(tab: Tab): Tab[] {
    if (tab.groupId !== null && this.groups.has(tab.groupId)) return tabs.tabsIn(tab.groupId);
    if (this.tabs.has(tab.id)) return [tab];
    return [];
  }

  /** Panes (by id) that should also get input typed into `paneId`. */
  targets(paneId: number): number[] {
    if (!this.on) return [];
    const tab = tabs.list.find((t) => leaves(t.root).some((p) => p.id === paneId));
    if (!tab) return [];
    return this.scope(tab)
      .flatMap((t) => leaves(t.root))
      .map((p) => p.id)
      .filter((id) => id !== paneId);
  }

  /** Whether a pane is part of a broadcast (sends and receives). */
  includes(paneId: number): boolean {
    return this.on && this.targets(paneId).length > 0;
  }

  toggleGroup(groupId: number) {
    if (this.groups.has(groupId)) this.groups.delete(groupId);
    else this.groups.add(groupId);
  }

  toggleTab(tabId: number) {
    if (this.tabs.has(tabId)) this.tabs.delete(tabId);
    else this.tabs.add(tabId);
  }

  stopAll() {
    this.groups.clear();
    this.tabs.clear();
  }

  /** Drops groups and tabs that are gone (closed, ungrouped). */
  prune() {
    for (const id of this.groups) if (tabs.tabsIn(id).length === 0) this.groups.delete(id);
    for (const id of this.tabs) if (!tabs.get(id)) this.tabs.delete(id);
  }

  /**
   * What the bar says: e.g. "Typing goes to 4 tabs in “prod”". Several
   * broadcasts at once are summed up.
   */
  describe(): string {
    const parts: string[] = [];
    for (const id of this.groups) {
      const n = tabs.tabsIn(id).length;
      const name = tabs.group(id)?.name;
      parts.push(`${n} ${n === 1 ? 'tab' : 'tabs'} in ${name ? `“${name}”` : 'a group'}`);
    }
    for (const id of this.tabs) {
      const tab = tabs.get(id);
      if (tab) parts.push(`${leaves(tab.root).length} panes of a tab`);
    }
    return parts.join(' and ');
  }

  /**
   * The toggle command: stops everything if anything is on; otherwise
   * starts for the active tab's group, or its split panes. Returns false
   * when there's nothing to broadcast to.
   */
  toggleActive(): boolean {
    if (this.on) {
      this.stopAll();
      return true;
    }
    const tab = tabs.active;
    if (!tab) return false;
    if (tab.groupId !== null && tabs.tabsIn(tab.groupId).length > 1) {
      this.groups.add(tab.groupId);
      return true;
    }
    if (tab.root.kind === 'split') {
      this.tabs.add(tab.id);
      return true;
    }
    return false;
  }
}

export const broadcast = new Broadcast();

/**
 * Replies xterm.js sends on the program's behalf (cursor position, device
 * attributes, mouse and focus reports, color queries). They come through
 * the same channel as typing, but belong to the pane that was asked, so
 * they're never broadcast.
 */
export function isTerminalReport(data: string): boolean {
  return (
    /^\x1b\[\d+;\d+R$/.test(data) || // cursor position
    /^\x1b\[[?>=]?[\d;]*c$/.test(data) || // device attributes
    /^\x1b\[[IO]$/.test(data) || // focus in/out
    /^\x1b\[M[\s\S]{3}$/.test(data) || // X10 mouse
    /^\x1b\[<\d+;\d+;\d+[Mm]$/.test(data) || // SGR mouse
    /^\x1b\[\??[\d;]*\$y$/.test(data) || // mode report
    /^\x1b\][\s\S]*(\x07|\x1b\\)$/.test(data) || // OSC reply
    /^\x1bP[\s\S]*\x1b\\$/.test(data) // DCS reply
  );
}
