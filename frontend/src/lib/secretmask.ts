import type { IBuffer, IDecoration, IDisposable, IMarker, Terminal } from '@xterm/xterm';
import { readLogicalLine } from './logview';
import { findSecrets } from './secrets';
import type { Theme } from './settings.svelte';
import { parseHex } from './themes';

// Covers the secrets on screen while Hide Secrets is on (lib/redaction).
// The rows on screen are checked right after each batch of output is read
// (xterm reads output in slices of a few milliseconds) and when the view
// scrolls, in a microtask — before the next frame is drawn, so a secret is
// covered from the first frame it appears in. A cover stays with its text
// as it scrolls. Nothing runs while off.

/** How far above the screen to look for the start of a private key. */
const KEY_LOOKBACK = 200;
const KEY_BEGIN = /-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----/;
const KEY_END = /-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----/;

interface Cover {
  marker: IMarker;
  decoration: IDecoration;
  x: number;
  width: number;
}

/** The covers' color: a solid bar between the theme's text and background. */
export function coverColor(t: Theme): string {
  const fg = parseHex(t.foreground) ?? [128, 128, 128];
  const bg = parseHex(t.background) ?? [0, 0, 0];
  return '#' + fg.map((v, i) => Math.round(v + (bg[i] - v) * 0.6).toString(16).padStart(2, '0')).join('');
}

/** A soft-wrapped line's text, quickly (no cell positions): rows joined. */
function quickLine(buf: IBuffer, y: number): { text: string; last: number } {
  let first = y;
  while (first > 0 && y - first < 40 && buf.getLine(first)?.isWrapped) first--;
  let text = '';
  let last = first;
  for (let row = first; row - first < 80; row++) {
    const line = buf.getLine(row);
    if (!line || (row > first && !line.isWrapped)) break;
    text += line.translateToString(row + 1 < buf.length && !!buf.getLine(row + 1)?.isWrapped ? false : true);
    last = row;
  }
  return { text, last };
}

/** Whether a row is inside a private key that started above it. */
function insideKey(buf: IBuffer, row: number): boolean {
  for (let y = row - 1; y >= Math.max(0, row - KEY_LOOKBACK); y--) {
    const text = buf.getLine(y)?.translateToString(true) ?? '';
    if (KEY_END.test(text)) return false;
    if (KEY_BEGIN.test(text)) return true;
  }
  return false;
}

export class SecretMask implements IDisposable {
  #term: Terminal;
  #on = false;
  #color: string;
  #covers: Cover[] = [];
  #handlers: IDisposable[];
  #queued = false;
  #disposed = false;

  constructor(term: Terminal, color: string) {
    this.#term = term;
    this.#color = color;
    // Output scrolls the screen once per line; one scan after the batch, still before it's drawn.
    const scan = () => {
      if (!this.#on || this.#queued) return;
      this.#queued = true;
      queueMicrotask(() => {
        this.#queued = false;
        if (this.#on && !this.#disposed) this.scan();
      });
    };
    this.#handlers = [term.onWriteParsed(scan), term.onScroll(scan), term.onResize(scan), term.buffer.onBufferChange(scan)];
  }

  set on(on: boolean) {
    if (on === this.#on) return;
    this.#on = on;
    if (on) this.scan();
    else this.#clear();
  }

  /** "#rrggbb": the color of the covers. */
  set color(color: string) {
    if (color === this.#color) return;
    this.#color = color;
    this.#clear();
    if (this.#on) this.scan();
  }

  /** Covers the secrets in the rows on screen, and uncovers rows that no longer have one. */
  scan() {
    const t = this.#term;
    if (!t.registerDecoration) return;
    const buf = t.buffer.active;
    const top = buf.viewportY;
    const bottom = top + t.rows - 1;
    const wanted = new Map<string, { row: number; x: number; width: number }>();
    const want = (row: number, x: number, width: number) => {
      if (row >= top && row <= bottom && width > 0) wanted.set(`${row}:${x}:${width}`, { row, x, width });
    };

    let inKey = insideKey(buf, top);
    let first = top;
    let last = bottom;
    for (let y = top; y <= bottom; ) {
      // Most lines have nothing: skip them without working out where each character is.
      const quick = quickLine(buf, y);
      if (!inKey && !KEY_BEGIN.test(quick.text) && findSecrets(quick.text).length === 0) {
        y = Math.max(y, quick.last) + 1;
        continue;
      }
      const line = readLogicalLine(buf, y);
      if (!line) break;
      first = Math.min(first, line.first);
      last = Math.max(last, line.last);
      let text = line.text;
      // Inside a private key: the whole line, up to and including its END.
      if (inKey) {
        const end = KEY_END.exec(text);
        const upto = end ? end.index + end[0].length : text.trimEnd().length;
        this.#spans(line, 0, upto, want);
        if (end) inKey = false;
        text = ' '.repeat(upto) + text.slice(upto);
      }
      for (const s of findSecrets(text)) this.#spans(line, s.start, s.end, want);
      const begin = KEY_BEGIN.exec(text);
      if (begin && !KEY_END.test(text.slice(begin.index))) inKey = true;
      y = line.last + 1;
    }

    // Keep covers still over a secret; drop those on rows checked now that aren't.
    const kept: Cover[] = [];
    for (const c of this.#covers) {
      if (c.marker.isDisposed) {
        c.decoration.dispose();
        continue;
      }
      const row = c.marker.line;
      if (row < first || row > last || wanted.delete(`${row}:${c.x}:${c.width}`)) kept.push(c);
      else dispose(c);
    }
    const cursor = buf.baseY + buf.cursorY;
    for (const w of wanted.values()) {
      const marker = t.registerMarker(w.row - cursor);
      if (!marker) continue;
      const decoration = t.registerDecoration({
        marker, x: w.x, width: w.width, layer: 'top',
        // Drawn over the text in the terminal's own colors, above selection and search highlights.
        backgroundColor: this.#color, foregroundColor: this.#color,
      });
      if (!decoration) {
        marker.dispose();
        continue;
      }
      decoration.onRender((el) => el.classList.add('secret-cover'));
      kept.push({ marker, decoration, x: w.x, width: w.width });
    }
    this.#covers = kept;
  }

  /** The cells of text[start, end) in a line, one stretch per row. */
  #spans(
    line: NonNullable<ReturnType<typeof readLogicalLine>>,
    start: number,
    end: number,
    want: (row: number, x: number, width: number) => void,
  ) {
    for (let i = start; i < end; ) {
      const row = line.rows[i];
      let j = i;
      while (j + 1 < end && line.rows[j + 1] === row) j++;
      want(row, line.cols[i], line.cols[j] - line.cols[i] + 1);
      i = j + 1;
    }
  }

  #clear() {
    this.#covers.forEach(dispose);
    this.#covers = [];
  }

  dispose() {
    this.#disposed = true;
    this.#handlers.forEach((h) => h.dispose());
    this.#clear();
  }
}

function dispose(c: Cover) {
  c.decoration.dispose();
  c.marker.dispose();
}
