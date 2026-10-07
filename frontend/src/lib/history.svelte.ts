import { SvelteMap } from 'svelte/reactivity';

/** Commands kept per pane; the oldest drop off. */
export const MAX_ENTRIES = 1000;

export interface HistoryEntry {
  /** The command's id in its terminal (see PromptTracker.reveal). */
  id: number;
  paneId: number;
  command: string;
  cwd: string;
  startedAt: number;
  /** null while it is still running. */
  finishedAt: number | null;
  durationMs: number | null;
  exitCode: number | null;
}

/**
 * Commands each pane's shell ran, as reported by shell integration.
 * Lives as long as the pane's shell does.
 */
class History {
  #byPane = new SvelteMap<number, HistoryEntry[]>();

  started(paneId: number, id: number, command: string, cwd: string, at = Date.now()) {
    if (!command.trim()) return;
    const list = [...(this.#byPane.get(paneId) ?? [])];
    list.push({ id, paneId, command, cwd, startedAt: at, finishedAt: null, durationMs: null, exitCode: null });
    if (list.length > MAX_ENTRIES) list.splice(0, list.length - MAX_ENTRIES);
    this.#byPane.set(paneId, list);
  }

  finished(paneId: number, exitCode: number, durationMs: number, at = Date.now()) {
    const list = this.#byPane.get(paneId);
    const last = list?.at(-1);
    if (!list || !last || last.finishedAt !== null) return;
    this.#byPane.set(paneId, [...list.slice(0, -1), { ...last, finishedAt: at, durationMs, exitCode }]);
  }

  /** A pane's commands, oldest first. */
  of(paneId: number): HistoryEntry[] {
    return this.#byPane.get(paneId) ?? [];
  }

  clear(paneId: number) {
    this.#byPane.delete(paneId);
  }

  /** Several panes' commands merged into one timeline, oldest first. */
  merged(paneIds: number[]): HistoryEntry[] {
    return paneIds.flatMap((id) => this.of(id)).sort((a, b) => a.startedAt - b.startedAt);
  }
}

export const history = new History();
