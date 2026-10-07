import { prettyPath } from './backend';
import { leaves, type Pane } from './layout';
import { overlay } from './overlay.svelte';
import { tabs, type Tab } from './tabs.svelte';

/** Panes busy with something other than an idle shell. */
export function busyPanes(panes: Pane[]): Pane[] {
  return panes.filter((p) => p.running || (!p.isShell && p.process));
}

export function describePane(p: Pane): string {
  const what = p.running && p.command ? p.command : p.process;
  return `${what} — ${prettyPath(p.cwd)}`;
}

const panesOf = (list: Tab[]) => list.flatMap((t) => leaves(t.root));

/** Runs `action` right away, or after confirming when programs would be killed. */
export function confirmIfBusy(panes: Pane[], title: string, confirmLabel: string, action: () => void) {
  const busy = busyPanes(panes);
  if (busy.length === 0) {
    action();
    return;
  }
  overlay.confirm({
    title,
    message:
      busy.length === 1
        ? 'A program is still running. Closing will end it.'
        : `${busy.length} programs are still running. Closing will end them.`,
    items: busy.map(describePane),
    confirmLabel,
    onConfirm: action,
  });
}

export function closeTabs(ids: number[], title = ids.length === 1 ? 'Close this tab?' : `Close ${ids.length} tabs?`) {
  const list = tabs.list.filter((t) => ids.includes(t.id));
  confirmIfBusy(panesOf(list), title, ids.length === 1 ? 'Close Tab' : 'Close Tabs', () => tabs.closeMany(ids));
}

export function closeTab(id: number) {
  closeTabs([id]);
}

export function closeActivePane() {
  const tab = tabs.active;
  const pane = tabs.activePane;
  if (!tab || !pane) return;
  const lastPane = tab.root.kind === 'pane';
  confirmIfBusy([pane], lastPane ? 'Close this tab?' : 'Close this pane?', lastPane ? 'Close Tab' : 'Close Pane', () =>
    tabs.closePane(tab.id, pane.id),
  );
}

export function closeGroup(groupId: number) {
  const ids = tabs.tabsIn(groupId).map((t) => t.id);
  const name = tabs.group(groupId)?.name;
  closeTabs(ids, name ? `Close the “${name}” group?` : 'Close this group?');
}

export function closeWindow(onClose: () => void) {
  confirmIfBusy(panesOf(tabs.list), 'Close this window?', 'Close Window', onClose);
}
