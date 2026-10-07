import { Service as StateService } from '../../bindings/github.com/dayanraj/wtf/internal/state';
import { Service as Windows } from '../../bindings/github.com/dayanraj/wtf/internal/windows';
import { settings } from './settings.svelte';
import { sidebar, type SavedSidebar } from './sidebar.svelte';
import { tabs, type SavedTabs } from './tabs.svelte';

const VERSION = 1;
const SAVE_DELAY_MS = 400;

interface SavedSession extends SavedTabs {
  version: number;
  fontSize: number;
  sidebar?: SavedSidebar;
}

/**
 * Sets up this window from what the window manager hands it: its saved
 * session, or a tab moved here from another window. False means start fresh.
 */
export async function restoreSession(windowName: string): Promise<boolean> {
  try {
    const raw = await Windows.Bootstrap(windowName);
    if (!raw) return false;
    const data = JSON.parse(raw) as Partial<SavedSession> & { kind?: string };
    if (data?.kind === 'tab') return tabs.adopt(data);
    if (data?.version !== VERSION) return false;
    // Before the panes mount, so terminals open at the right size.
    if (typeof data.fontSize === 'number') settings.setFontSize(data.fontSize);
    sidebar.restore(data.sidebar);
    return tabs.restore(data);
  } catch (err) {
    console.warn('could not restore session', err);
    return false;
  }
}

let stop: (() => void) | undefined;

/** Stops saving, e.g. because this window is closing for good. */
export function stopAutosave() {
  stop?.();
  stop = undefined;
}

/**
 * Saves this window's session shortly after anything in it changes: tabs,
 * groups, layouts, folders, text size.
 */
export function autosave(windowName: string) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let last = '';

  stopAutosave();
  stop = $effect.root(() => {
    $effect(() => {
      const session: SavedSession = {
        version: VERSION,
        fontSize: settings.fontSize,
        sidebar: sidebar.snapshot(),
        ...tabs.snapshot(),
      };
      const json = JSON.stringify(session);
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (json === last) return;
        last = json;
        StateService.SaveWindow(windowName, json).catch((err) => console.warn('could not save session', err));
      }, SAVE_DELAY_MS);
    });
    return () => clearTimeout(timer);
  });
}
