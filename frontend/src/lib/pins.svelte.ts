// Output pinned to the side pane (the Pinned tab), to keep in view while
// you work on: a command's output, or a selection. A pin is a copy of the
// text; it outlives the pane it came from and the scrollback. A command
// still running when pinned keeps its pin up to date until it finishes.
// Pins live in this window only, in memory — never written to disk,
// since output can hold secrets.

/** Pins kept per window; pinning another drops the oldest. */
export const MAX_PINS = 30;
/** A pin keeps at most this much text: the end of it, where a command's result usually is. */
export const MAX_PIN_CHARS = 200_000;

export interface Pin {
  id: number;
  /** The command, or "Selection". */
  title: string;
  text: string;
  /** The text was cut to its last MAX_PIN_CHARS. */
  truncated: boolean;
  /** Where it came from, to show it in the terminal again (while that pane exists). */
  paneId: number;
  /** The command block, for a command's output. */
  blockId: number | null;
  /** Folder of the shell when pinned. */
  cwd: string;
  pinnedAt: number;
  /** The command is still running: the pin follows its output. */
  running: boolean;
  exitCode?: number;
  durationMs?: number;
  collapsed: boolean;
}

export type PinDraft = Pick<Pin, 'title' | 'text' | 'paneId' | 'blockId' | 'cwd' | 'running' | 'exitCode' | 'durationMs'>;

function clip(text: string): { text: string; truncated: boolean } {
  return text.length > MAX_PIN_CHARS ? { text: text.slice(-MAX_PIN_CHARS), truncated: true } : { text, truncated: false };
}

class Pins {
  list = $state<Pin[]>([]);
  #nextId = 1;

  /** Pins text; a command's output already pinned is brought up to date instead of pinned twice. */
  add(draft: PinDraft): Pin {
    const same = draft.blockId !== null && this.list.find((p) => p.paneId === draft.paneId && p.blockId === draft.blockId);
    if (same) {
      Object.assign(same, draft, clip(draft.text), { collapsed: false });
      return same;
    }
    const pin: Pin = { ...draft, ...clip(draft.text), id: this.#nextId++, pinnedAt: Date.now(), collapsed: false };
    this.list = [pin, ...this.list].slice(0, MAX_PINS);
    return this.list[0];
  }

  remove(id: number) {
    this.list = this.list.filter((p) => p.id !== id);
  }

  clear() {
    this.list = [];
  }

  /** Pins following a running command (to refresh while they're on screen). */
  get live(): Pin[] {
    return this.list.filter((p) => p.running);
  }

  /** New output of a running command; `done` with its exit code when it finished. */
  update(paneId: number, blockId: number, text: string, done?: { exitCode: number; durationMs: number }) {
    const pin = this.list.find((p) => p.running && p.paneId === paneId && p.blockId === blockId);
    if (!pin) return;
    Object.assign(pin, clip(text));
    if (done) Object.assign(pin, { running: false, ...done });
  }

  /** The pane closed: its running commands can't be followed any more. */
  paneGone(paneId: number) {
    for (const p of this.list) if (p.paneId === paneId) p.running = false;
  }
}

export const pins = new Pins();
