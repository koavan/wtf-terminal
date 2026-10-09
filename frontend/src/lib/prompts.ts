import type { IDecoration, IDisposable, IMarker, Terminal } from '@xterm/xterm';
import { formatDuration } from './toast.svelte';

// Tracks the prompts and commands a shell reports through OSC 133 (see
// internal/terminal/shellintegration) and turns each into a command block:
// a ✓/✗ mark in the margin (click it for the block's actions), its
// duration at the end of the prompt line when it took a while, and the
// command's text and output for copying, selecting and running again.

interface CommandRecord {
  /** Stable per terminal; lets others refer to this command later. */
  id: number;
  prompt: IMarker;
  /** Where the command's output starts and ends. */
  output?: IMarker;
  end?: IMarker;
  startedAt?: number;
  decoration?: IDecoration;
  /** The duration label at the end of the prompt line. */
  durationLabel?: IDecoration;
  text: string;
  exitCode?: number;
  durationMs?: number;
  finishedAt?: Date;
}

/** What's known about one command, for its menu. */
export interface BlockInfo {
  id: number;
  text: string;
  /** Still running: no exit code yet. */
  running: boolean;
  exitCode?: number;
  durationMs?: number;
  finishedAt?: Date;
  /** Its output is still in the scrollback. */
  hasOutput: boolean;
}

/** Commands this quick get no duration label (the mark's tooltip still has it). */
export const DURATION_LABEL_MS = 1000;

export interface PromptEvents {
  onPrompt(): void;
  /** `id` identifies the command for reveal(). */
  onCommandStart(text: string, id: number): void;
  onCommandEnd(exitCode: number, durationMs: number): void;
  /** The variables that pick the shell's cloud and cluster context, at each prompt. */
  onEnv?(env: string): void;
  /** A command's ✓/✗ mark was clicked. */
  onMark?(id: number, e: MouseEvent): void;
}

const MAX_RECORDS = 1000;

/** Undoes the escaping done by the shell scripts (\\ and \xNN). */
export function unescapeCommand(s: string): string {
  return s.replace(/\\(\\|x([0-9a-fA-F]{2}))/g, (_, raw: string, hex?: string) =>
    hex ? String.fromCharCode(parseInt(hex, 16)) : raw,
  );
}

export class PromptTracker implements IDisposable {
  #term: Terminal;
  #events: PromptEvents;
  #records: CommandRecord[] = [];
  #current: CommandRecord | undefined;
  #pendingText = '';
  #nextId = 1;
  #handlers: IDisposable[];

  constructor(term: Terminal, events: PromptEvents) {
    this.#term = term;
    this.#events = events;
    this.#handlers = [
      term.parser.registerOscHandler(133, (data) => this.#on133(data)),
      term.parser.registerOscHandler(633, (data) => {
        if (data.startsWith('E;')) this.#pendingText = unescapeCommand(data.slice(2));
        else if (data.startsWith('P;WtfEnv=')) this.#events.onEnv?.(unescapeCommand(data.slice(9)));
        return true;
      }),
    ];
  }

  #on133(data: string): boolean {
    const [kind, arg] = data.split(';');
    switch (kind) {
      case 'A': {
        const prompt = this.#term.registerMarker(0);
        if (prompt) {
          this.#current = { id: this.#nextId++, prompt, text: '' };
          this.#records.push(this.#current);
          this.#trim();
        }
        this.#events.onPrompt();
        break;
      }
      case 'C': {
        const rec = this.#current;
        if (rec && !rec.output) {
          rec.output = this.#term.registerMarker(0);
          rec.startedAt = performance.now();
          rec.text = this.#pendingText;
        }
        this.#events.onCommandStart(this.#pendingText, rec?.id ?? 0);
        this.#pendingText = '';
        break;
      }
      case 'D': {
        const rec = this.#current;
        if (!rec?.startedAt || rec.end) break;
        rec.end = this.#term.registerMarker(0);
        const exitCode = Number.parseInt(arg ?? '', 10) || 0;
        rec.exitCode = exitCode;
        rec.durationMs = performance.now() - rec.startedAt;
        rec.finishedAt = new Date();
        this.#decorate(rec, exitCode);
        this.#labelDuration(rec);
        this.#events.onCommandEnd(exitCode, rec.durationMs);
        break;
      }
    }
    return true;
  }

  #decorate(rec: CommandRecord, exitCode: number) {
    const ok = exitCode === 0;
    // Optional: the headless terminal used in tests has no decorations.
    const deco = this.#term.registerDecoration?.({ marker: rec.prompt, x: 0, width: 1, layer: 'top' });
    const took = rec.durationMs !== undefined ? ` · took ${formatDuration(rec.durationMs)}` : '';
    const at = rec.finishedAt ? ` · finished ${rec.finishedAt.toLocaleTimeString()}` : '';
    deco?.onRender((el) => {
      el.classList.add('cmd-mark', ok ? 'ok' : 'fail');
      el.title = `${ok ? 'Succeeded' : `Failed with exit code ${exitCode}`}${took}${at}\nClick for this command's actions`;
      if (!el.dataset.wired) {
        el.dataset.wired = '1';
        el.addEventListener('mousedown', (e) => {
          e.preventDefault();
          e.stopPropagation();
          this.#events.onMark?.(rec.id, e);
        });
      }
    });
    rec.decoration = deco;
  }

  /**
   * Puts how long a command took at the end of its prompt line, when it
   * took a while and the line has room (so it never covers a right-hand
   * prompt or the command itself).
   */
  #labelDuration(rec: CommandRecord) {
    rec.durationLabel?.dispose();
    rec.durationLabel = undefined;
    if (!this.#term.registerDecoration || rec.durationMs === undefined || rec.durationMs < DURATION_LABEL_MS) return;
    if (rec.prompt.isDisposed || rec.prompt.line < 0) return;
    const label = formatDuration(rec.durationMs);
    const width = label.length + 2;
    const cols = this.#term.cols;
    const used = this.#term.buffer.active.getLine(rec.prompt.line)?.translateToString(true).length ?? cols;
    if (used + width + 2 > cols) return;
    const deco = this.#term.registerDecoration({ marker: rec.prompt, x: cols - width, width, layer: 'top' });
    deco?.onRender((el) => {
      el.classList.add('cmd-duration');
      el.textContent = label;
      el.title = `Took ${label}`;
    });
    rec.durationLabel = deco;
  }

  /** Lays the duration labels out again for a new width. */
  relayout() {
    for (const r of this.#records) if (r.durationLabel) this.#labelDuration(r);
  }

  #trim() {
    while (this.#records.length > MAX_RECORDS) {
      const old = this.#records.shift()!;
      old.decoration?.dispose();
      old.durationLabel?.dispose();
      old.prompt.dispose();
      old.output?.dispose();
      old.end?.dispose();
    }
  }

  /** Buffer lines of prompts still in the scrollback, oldest first. */
  #promptLines(): number[] {
    return this.#records.map((r) => r.prompt).filter((m) => !m.isDisposed && m.line >= 0).map((m) => m.line);
  }

  /** Scrolls so the previous (or next) prompt is at the top of the view. */
  jump(direction: -1 | 1) {
    const top = this.#term.buffer.active.viewportY;
    const lines = this.#promptLines();
    const target =
      direction < 0 ? lines.filter((l) => l < top).at(-1) : lines.find((l) => l > top);
    if (target !== undefined) this.#term.scrollToLine(target);
    else if (direction > 0) this.#term.scrollToBottom();
  }

  /**
   * Scrolls a command's prompt to the top of the view. False when it has
   * scrolled out of the buffer.
   */
  reveal(id: number): boolean {
    const rec = this.#records.find((r) => r.id === id);
    if (!rec || rec.prompt.isDisposed || rec.prompt.line < 0) return false;
    this.#term.scrollToLine(rec.prompt.line);
    return true;
  }

  /** The output of the most recent finished command, or null. */
  lastOutput(): string | null {
    const rec = this.#records.findLast((r) => r.output && r.end && !r.output.isDisposed && r.output.line >= 0);
    return rec ? this.#outputOf(rec) : null;
  }

  /** The command whose block holds a buffer line: from its prompt to the next prompt. */
  blockAt(line: number): number | null {
    const live = this.#records.filter((r) => !r.prompt.isDisposed && r.prompt.line >= 0);
    let found: CommandRecord | undefined;
    for (const r of live) {
      if (r.prompt.line <= line) found = r;
      else break;
    }
    return found && found.output ? found.id : null;
  }

  info(id: number): BlockInfo | null {
    const r = this.#records.find((x) => x.id === id);
    if (!r) return null;
    return {
      id,
      text: r.text,
      running: r.exitCode === undefined,
      exitCode: r.exitCode,
      durationMs: r.durationMs,
      finishedAt: r.finishedAt,
      hasOutput: !!r.output && !r.output.isDisposed && r.output.line >= 0,
    };
  }

  /** A command's output so far, or null once it has left the scrollback. */
  output(id: number): string | null {
    const r = this.#records.find((x) => x.id === id);
    return r?.output && !r.output.isDisposed && r.output.line >= 0 ? this.#outputOf(r) : null;
  }

  /** Selects a command's output in the terminal. False when there's none to select. */
  selectOutput(id: number): boolean {
    const r = this.#records.find((x) => x.id === id);
    if (!r?.output || r.output.isDisposed || r.output.line < 0) return false;
    const to = this.#lastLine(r);
    if (to < r.output.line) return false;
    this.#term.selectLines(r.output.line, to);
    return true;
  }

  /** Shades a command's whole block until the returned function is called. */
  highlight(id: number): () => void {
    const r = this.#records.find((x) => x.id === id);
    if (!r || !this.#term.registerDecoration || r.prompt.isDisposed || r.prompt.line < 0) return () => {};
    const height = Math.max(1, this.#lastLine(r) - r.prompt.line + 1);
    const deco = this.#term.registerDecoration({ marker: r.prompt, x: 0, width: this.#term.cols, height, layer: 'bottom' });
    deco?.onRender((el) => el.classList.add('cmd-block-highlight'));
    return () => deco?.dispose();
  }

  /** The last line of a command's output: before its end mark, or the buffer's end while running. */
  #lastLine(r: CommandRecord): number {
    if (r.end && !r.end.isDisposed && r.end.line >= 0) return r.end.line - 1;
    const buf = this.#term.buffer.active;
    return buf.baseY + buf.cursorY;
  }

  #outputOf(rec: CommandRecord): string {
    const buf = this.#term.buffer.active;
    const from = rec.output!.line;
    const to = this.#lastLine(rec) + 1;
    let text = '';
    for (let y = from; y < to; y++) {
      const line = buf.getLine(y);
      if (!line) continue;
      text += line.translateToString(true);
      // A soft-wrapped row continues on the next one.
      if (!(y + 1 < to && buf.getLine(y + 1)?.isWrapped)) text += '\n';
    }
    return text.replace(/\n+$/, '');
  }

  dispose() {
    this.#handlers.forEach((h) => h.dispose());
    for (const r of this.#records) {
      r.decoration?.dispose();
      r.durationLabel?.dispose();
    }
    this.#records = [];
  }
}
