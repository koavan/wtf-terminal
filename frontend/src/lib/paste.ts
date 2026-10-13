// Multi-line paste confirmation: text with more than one line, or with
// control characters, is shown before it reaches the shell. Decided by the
// text alone — never by guessing what a command does.

/** C0 controls other than tab and newlines, DEL, and C1 controls (ESC [ as one byte). */
const CONTROL = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f\x80-\x9f]/g;

export interface PasteFacts {
  /** Lines it has; a final newline doesn't start another. */
  lines: number;
  /** Whether it ends with a newline, which runs the last line without bracketed paste. */
  finalNewline: boolean;
  /** Control characters in it. */
  controls: number;
}

export function pasteFacts(text: string): PasteFacts {
  const finalNewline = /(\r\n|\r|\n)$/.test(text);
  const body = finalNewline ? text.replace(/(\r\n|\r|\n)$/, '') : text;
  return {
    lines: body.split(/\r\n|\r|\n/).length,
    finalNewline,
    controls: text.match(CONTROL)?.length ?? 0,
  };
}

/** Whether a paste is shown first: more than one line, a final newline, or a control character. */
export function needsConfirm(f: PasteFacts): boolean {
  return f.lines > 1 || f.finalNewline || f.controls > 0;
}

/** The text with control characters made visible: ␛ for ESC, ␀–␟ for the rest, ␡ for DEL, ⟨9B⟩ for C1. */
export function showControls(text: string): string {
  return text.replace(CONTROL, (c) => {
    const n = c.charCodeAt(0);
    if (n < 0x20) return String.fromCharCode(0x2400 + n);
    if (n === 0x7f) return '␡';
    return `⟨${n.toString(16).toUpperCase()}⟩`;
  });
}

/** The text without its final newline, so the shell waits for Enter. */
export function withoutFinalNewline(text: string): string {
  return text.replace(/(\r\n|\r|\n)$/, '');
}
