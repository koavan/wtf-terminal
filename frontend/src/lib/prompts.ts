import type { IDecoration, IDisposable, IMarker, Terminal } from '@xterm/xterm';

// Tracks the prompts and commands a shell reports through OSC 133 (see
// internal/terminal/shellintegration) and puts a ✓/✗ mark next to each
// finished command.

interface CommandRecord {
  prompt: IMarker;
  /** Where the command's output starts and ends. */
  output?: IMarker;
  end?: IMarker;
  startedAt?: number;
  decoration?: IDecoration;
}

export interface PromptEvents {
  onPrompt(): void;
  onCommandStart(text: string): void;
  onCommandEnd(exitCode: number, durationMs: number): void;
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
  #handlers: IDisposable[];

  constructor(term: Terminal, events: PromptEvents) {
    this.#term = term;
    this.#events = events;
    this.#handlers = [
      term.parser.registerOscHandler(133, (data) => this.#on133(data)),
      term.parser.registerOscHandler(633, (data) => {
        if (data.startsWith('E;')) this.#pendingText = unescapeCommand(data.slice(2));
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
          this.#current = { prompt };
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
        }
        this.#events.onCommandStart(this.#pendingText);
        this.#pendingText = '';
        break;
      }
      case 'D': {
        const rec = this.#current;
        if (!rec?.startedAt || rec.end) break;
        rec.end = this.#term.registerMarker(0);
        const exitCode = Number.parseInt(arg ?? '', 10) || 0;
        this.#decorate(rec, exitCode);
        this.#events.onCommandEnd(exitCode, performance.now() - rec.startedAt);
        break;
      }
    }
    return true;
  }

  #decorate(rec: CommandRecord, exitCode: number) {
    const ok = exitCode === 0;
    // Optional: the headless terminal used in tests has no decorations.
    const deco = this.#term.registerDecoration?.({ marker: rec.prompt, x: 0, width: 1, layer: 'top' });
    deco?.onRender((el) => {
      el.classList.add('cmd-mark', ok ? 'ok' : 'fail');
      el.title = ok ? 'Succeeded' : `Failed with exit code ${exitCode}`;
    });
    rec.decoration = deco;
  }

  #trim() {
    while (this.#records.length > MAX_RECORDS) {
      const old = this.#records.shift()!;
      old.decoration?.dispose();
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

  /** The output of the most recent finished command, or null. */
  lastOutput(): string | null {
    const rec = this.#records.findLast((r) => r.output && r.end && !r.output.isDisposed && r.output.line >= 0);
    if (!rec) return null;
    const buf = this.#term.buffer.active;
    const from = rec.output!.line;
    const to = rec.end!.line;
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
    for (const r of this.#records) r.decoration?.dispose();
    this.#records = [];
  }
}
