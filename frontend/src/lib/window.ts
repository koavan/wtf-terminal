import { Window } from '@wailsio/runtime';
import { Service as Windows } from '../../bindings/github.com/dayanraj/wtf/internal/windows';
import { stopAutosave } from './session.svelte';
import { tabs } from './tabs.svelte';
import { terminalFor } from './terminals';
import { toast } from './toast.svelte';

export { Windows };

/** This window's name, as the Go window manager knows it. */
export const thisWindow = { name: '' };

export async function initWindow() {
  thisWindow.name = await Window.Name();
}

/**
 * Closes this window for good: its saved session is dropped, so it won't
 * come back on the next launch (quitting the app keeps it).
 */
export function closeThisWindow() {
  stopAutosave();
  Windows.Close(thisWindow.name, true);
}

/** Scrollback of every pane, for a tab that moves to another window. */
const buffer = (paneId: number) => terminalFor(paneId)?.serialize() ?? '';

/** Opens a new window holding this tab; x/y are screen coordinates. */
export async function moveTabToNewWindow(tabId: number, x = -1, y = -1) {
  if (tabs.list.length === 1) {
    toast.show('This is the only tab in the window');
    return;
  }
  const payload = tabs.detach(tabId, buffer);
  if (payload) await Windows.NewWindow(JSON.stringify(payload), Math.round(x), Math.round(y));
}

export async function moveTabToWindow(tabId: number, target: string) {
  const payload = tabs.detach(tabId, buffer);
  if (payload) await Windows.MoveTab(target, JSON.stringify(payload));
}

/** Other open windows, for "Move tab to…". */
export async function otherWindows() {
  return ((await Windows.Windows()) ?? []).filter((w) => w.name !== thisWindow.name);
}
