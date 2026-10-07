import { Service as StateService } from '../../bindings/github.com/dayanraj/wtf/internal/state';
import { settings } from './settings.svelte';
import { tabs, type SavedTabs } from './tabs.svelte';

const VERSION = 1;
const SAVE_DELAY_MS = 400;

interface SavedSession extends SavedTabs {
  version: number;
  fontSize: number;
}

/** Restores the last session. False means there was none (or it was unusable). */
export async function restoreSession(): Promise<boolean> {
  try {
    const raw = await StateService.LoadSession();
    if (!raw) return false;
    const data = JSON.parse(raw) as Partial<SavedSession>;
    if (data?.version !== VERSION) return false;
    // Before the panes mount, so terminals open at the right size.
    if (typeof data.fontSize === 'number') settings.setFontSize(data.fontSize);
    return tabs.restore(data);
  } catch (err) {
    console.warn('could not restore session', err);
    return false;
  }
}

/**
 * Saves the session shortly after anything in it changes: tabs, groups,
 * layouts, folders, text size. Returns a function that stops saving.
 */
export function autosave(): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let last = '';

  return $effect.root(() => {
    $effect(() => {
      const session: SavedSession = { version: VERSION, fontSize: settings.fontSize, ...tabs.snapshot() };
      const json = JSON.stringify(session);
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (json === last) return;
        last = json;
        StateService.SaveSession(json).catch((err) => console.warn('could not save session', err));
      }, SAVE_DELAY_MS);
    });
    return () => clearTimeout(timer);
  });
}
