import { tabs } from './tabs.svelte';

/** Which tab's name is being edited in the tab strip (one at a time). */
export const renaming = $state<{ tabId: number | null }>({ tabId: null });

/** Starts editing a tab's name; with no id, the active tab's. */
export function startRename(tabId = tabs.activeId) {
  if (tabId === null) return;
  tabs.activate(tabId); // a tab hidden in a collapsed group comes into view
  renaming.tabId = tabId;
}

/** The text field being typed in, if any other than the terminal's own. */
export function focusedTextField(): HTMLInputElement | HTMLTextAreaElement | null {
  const el = document.activeElement;
  if (el instanceof HTMLInputElement) return el;
  if (el instanceof HTMLTextAreaElement && !el.classList.contains('xterm-helper-textarea')) return el;
  return null;
}
