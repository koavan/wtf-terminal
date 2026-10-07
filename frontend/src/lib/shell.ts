/** Quotes a path for bash/zsh: as-is when safe, otherwise single-quoted. */
export function shellQuote(s: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(s) && !s.startsWith('-')) return s;
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

/**
 * `path` relative to `base` when it is inside it ("src/main.go"), else the
 * absolute path. Paths are absolute and use "/".
 */
export function relativeTo(base: string, path: string): string {
  if (!base) return path;
  const b = base.endsWith('/') ? base : base + '/';
  if (path === base) return '.';
  return path.startsWith(b) ? path.slice(b.length) : path;
}

export function parentOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i <= 0 ? '/' : path.slice(0, i);
}

export function baseName(path: string): string {
  return path === '/' ? '/' : path.slice(path.lastIndexOf('/') + 1);
}

/**
 * Ctrl-X Ctrl-]: the integration scripts bind it to removing this session's
 * commands from the shell's own history (see internal/terminal
 * ClearHistoryKeys). Only send it to a shell idle at its prompt.
 */
export const CLEAR_HISTORY_KEYS = '\x18\x1d';

/** Programs that connect elsewhere: a local file tree would be misleading. */
export const REMOTE_PROCESSES = new Set(['ssh', 'mosh', 'mosh-client', 'et', 'telnet']);
