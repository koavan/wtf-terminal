import type { IBuffer, IBufferCell, IDecoration, IDisposable, ILink, IMarker, Terminal } from '@xterm/xterm';
import { scanLine, type LogToken } from './logs';
import { parseHex } from './themes';
import type { Theme } from './settings.svelte';

// Log-friendly rendering: colors for the log levels, timestamps, URLs, IPs
// and Kubernetes resources on screen, and ⌘-click (Ctrl-click) actions for
// them and for file:line references (see lib/logs for what's found).
//
// It never touches the output itself and never slows rendering down: only
// the rows on screen are scanned, a little after output settles or the
// view scrolls, and only while the pane is visible. Text a program colored
// itself keeps its colors, and full-screen programs (vim, less, htop) are
// left alone. Links are worked out only for the row under the mouse.

/** How long after output or a scroll the screen is scanned again. */
const SCAN_DELAY_MS = 120;
/** A soft-wrapped line longer than this many rows is read only this far each way. */
const MAX_WRAP_ROWS = 40;
/** Decorations per screen, at most. */
const MAX_DECORATIONS = 600;

/** One line of output as the program wrote it — soft-wrapped rows joined — and where each character is. */
export interface LogicalLine {
  text: string;
  /** Buffer row and column of each UTF-16 unit of `text`. */
  rows: number[];
  cols: number[];
  /** Whether each unit's cell has the default color (the program didn't color it). */
  plain: boolean[];
  first: number;
  last: number;
}

export function readLogicalLine(buf: IBuffer, y: number): LogicalLine | null {
  if (!buf.getLine(y)) return null;
  let first = y;
  while (first > 0 && y - first < MAX_WRAP_ROWS && buf.getLine(first)?.isWrapped) first--;
  let last = y;
  while (last - y < MAX_WRAP_ROWS && buf.getLine(last + 1)?.isWrapped) last++;

  const out: LogicalLine = { text: '', rows: [], cols: [], plain: [], first, last };
  let cell: IBufferCell | undefined;
  const parts: string[] = [];
  for (let row = first; row <= last; row++) {
    const line = buf.getLine(row);
    if (!line) break;
    for (let x = 0; x < line.length; x++) {
      cell = line.getCell(x, cell);
      if (!cell || cell.getWidth() === 0) continue;
      const chars = cell.getChars() || ' ';
      const plain = cell.isFgDefault();
      for (let i = 0; i < chars.length; i++) {
        out.rows.push(row);
        out.cols.push(x);
        out.plain.push(plain);
      }
      parts.push(chars);
    }
  }
  out.text = parts.join('');
  return out;
}

/** The token under a cell, with the line it's in. */
export function tokenAt(buf: IBuffer, row: number, col: number): { token: LogToken; line: LogicalLine } | null {
  const line = readLogicalLine(buf, row);
  if (!line) return null;
  for (const token of scanLine(line.text)) {
    for (let i = token.start; i < token.end; i++) {
      if (line.rows[i] === row && line.cols[i] === col) return { token, line };
    }
  }
  return null;
}

export type LogColors = Partial<Record<'error' | 'warn' | 'info' | 'debug' | 'timestamp' | 'url' | 'ip' | 'k8s', string>>;

/** Colors from the theme's own palette; the muted ones halfway between text and background. */
export function logColors(t: Theme): LogColors {
  const p = t.palette.map(hex6);
  const fg = parseHex(t.foreground);
  const bg = parseHex(t.background);
  const muted = fg && bg ? '#' + fg.map((v, i) => Math.round(v + (bg[i] - v) * 0.45).toString(16).padStart(2, '0')).join('') : undefined;
  return { error: p[1], warn: p[3], info: p[2], debug: muted, timestamp: muted, url: p[4], ip: p[5], k8s: p[6] };
}

/** "#rrggbb" — the only form decorations take — or undefined. */
function hex6(c: string | undefined): string | undefined {
  const rgb = c ? parseHex(c) : null;
  return rgb ? '#' + rgb.map((v) => v.toString(16).padStart(2, '0')).join('') : undefined;
}

function colorOf(t: LogToken, colors: LogColors): string | undefined {
  switch (t.kind) {
    case 'level': return colors[t.level];
    case 'timestamp':
    case 'time': return colors.timestamp;
    case 'url': return colors.url;
    case 'ip': return colors.ip;
    case 'k8s': return colors.k8s;
    default: return undefined; // file:line: underlined on hover once it's known to exist
  }
}

/** Kinds this view links; URLs have the web-links addon. */
const LINKED = new Set<LogToken['kind']>(['timestamp', 'ip', 'path', 'k8s', 'jwt']);

export interface LogViewOptions {
  colors: LogColors;
  /** A token was ⌘-clicked (Ctrl-clicked). */
  onActivate(token: LogToken, e: MouseEvent): void;
  /** The absolute path of the file a file:line reference names on this machine; null hides the link. */
  resolveFile(path: string): Promise<string | null>;
  /** Whether the click had the link modifier held. */
  isLinkClick(e: MouseEvent): boolean;
}

interface Painted {
  marker: IMarker;
  decoration: IDecoration;
  x: number;
  width: number;
  color: string;
  text: string;
}

export class LogView implements IDisposable {
  #term: Terminal;
  #opts: LogViewOptions;
  #highlight = true;
  #visible = true;
  #painted: Painted[] = [];
  #timer: ReturnType<typeof setTimeout> | null = null;
  #handlers: IDisposable[] = [];

  constructor(term: Terminal, opts: LogViewOptions) {
    this.#term = term;
    this.#opts = opts;
    const schedule = () => this.#schedule();
    this.#handlers.push(
      term.onWriteParsed(schedule),
      term.onScroll(schedule),
      term.onResize(schedule),
      term.buffer.onBufferChange(schedule),
      term.registerLinkProvider({ provideLinks: (y, cb) => void this.#links(y - 1).then(cb, () => cb(undefined)) }),
    );
  }

  /** Colors on or off (`log-highlighting` in config.toml). */
  set highlight(on: boolean) {
    if (on === this.#highlight) return;
    this.#highlight = on;
    this.#schedule();
  }

  /** Off screen nothing is scanned; the screen is caught up when it's back. */
  set visible(on: boolean) {
    if (on === this.#visible) return;
    this.#visible = on;
    if (on) this.#schedule();
  }

  set colors(colors: LogColors) {
    this.#opts.colors = colors;
    this.#clear(); // every color changes
    this.#schedule();
  }

  #schedule() {
    if (this.#timer !== null || (!this.#highlight && !this.#painted.length)) return;
    this.#timer = setTimeout(() => {
      this.#timer = null;
      if (this.#visible) this.paint();
    }, SCAN_DELAY_MS);
  }

  /** Scans the rows on screen and brings the colors up to date. Normally runs on its own. */
  paint() {
    const t = this.#term;
    const buf = t.buffer.active;
    if (!this.#highlight || buf.type === 'alternate' || !t.registerDecoration) {
      this.#clear();
      return;
    }
    const top = buf.viewportY;
    const bottom = top + t.rows - 1;
    const wanted = new Map<string, Omit<Painted, 'marker' | 'decoration'> & { row: number }>();

    for (let y = top; y <= bottom && wanted.size < MAX_DECORATIONS; ) {
      const line = readLogicalLine(buf, y);
      if (!line) break;
      for (const token of scanLine(line.text)) {
        const color = colorOf(token, this.#opts.colors);
        if (!color) continue;
        // A program's own colors win.
        let plain = true;
        for (let i = token.start; i < token.end && plain; i++) plain = line.plain[i];
        if (!plain) continue;
        // One decoration per row the token is on.
        for (let i = token.start; i < token.end; ) {
          const row = line.rows[i];
          let j = i;
          while (j + 1 < token.end && line.rows[j + 1] === row) j++;
          if (row >= top && row <= bottom) {
            const x = line.cols[i];
            const width = line.cols[j] - x + 1;
            const text = line.text.slice(i, j + 1);
            wanted.set(`${row}:${x}:${width}:${color}:${text}`, { row, x, width, color, text });
          }
          i = j + 1;
        }
      }
      y = line.last + 1;
    }

    // Keep what's still right (rows move as the scrollback grows), drop the rest, add what's new.
    const kept: Painted[] = [];
    for (const p of this.#painted) {
      const key = `${p.marker.line}:${p.x}:${p.width}:${p.color}:${p.text}`;
      if (!p.marker.isDisposed && wanted.delete(key)) kept.push(p);
      else dispose(p);
    }
    const cursor = buf.baseY + buf.cursorY;
    for (const w of wanted.values()) {
      const marker = t.registerMarker(w.row - cursor);
      if (!marker) continue;
      const decoration = t.registerDecoration({ marker, x: w.x, width: w.width, foregroundColor: w.color, layer: 'bottom' });
      if (!decoration) {
        marker.dispose();
        continue;
      }
      kept.push({ marker, decoration, x: w.x, width: w.width, color: w.color, text: w.text });
    }
    this.#painted = kept;
  }

  #clear() {
    this.#painted.forEach(dispose);
    this.#painted = [];
  }

  async #links(row: number): Promise<ILink[] | undefined> {
    const line = readLogicalLine(this.#term.buffer.active, row);
    if (!line) return undefined;
    const links: ILink[] = [];
    for (let token of scanLine(line.text)) {
      if (!LINKED.has(token.kind)) continue;
      const startRow = line.rows[token.start];
      const endRow = line.rows[token.end - 1];
      if (row < startRow || row > endRow) continue;
      if (token.kind === 'path') {
        const path = await this.#opts.resolveFile(token.path);
        if (!path) continue;
        token = { ...token, path };
      }
      const linked = token;
      links.push({
        range: {
          start: { x: line.cols[token.start] + 1, y: startRow + 1 },
          end: { x: line.cols[token.end - 1] + 1, y: endRow + 1 },
        },
        text: token.text,
        decorations: { pointerCursor: true, underline: true },
        activate: (e) => {
          if (this.#opts.isLinkClick(e)) this.#opts.onActivate(linked, e);
        },
      });
    }
    return links.length ? links : undefined;
  }

  dispose() {
    if (this.#timer !== null) clearTimeout(this.#timer);
    this.#handlers.forEach((h) => h.dispose());
    this.#clear();
  }
}

function dispose(p: Painted) {
  p.decoration.dispose();
  p.marker.dispose();
}
