import type { GitChange, GitStatus } from '../../bindings/github.com/dayanraj/wtf/internal/files';

export type { GitChange, GitStatus } from '../../bindings/github.com/dayanraj/wtf/internal/files';

// What the file tree shows of git: each changed file's state, and which
// folders hold changes. Built once per `git status` (see internal/files).

export type ChangeKind = 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked' | 'conflict';

/** The letter beside a changed file, as in most editors. */
export const LETTER: Record<ChangeKind, string> = {
  modified: 'M', added: 'A', deleted: 'D', renamed: 'R', untracked: 'U', conflict: '!',
};

export const LABEL: Record<ChangeKind, string> = {
  modified: 'Modified', added: 'Added', deleted: 'Deleted', renamed: 'Renamed', untracked: 'Untracked (new)', conflict: 'Merge conflict',
};

/** Which kind wins when a folder holds several: the one that most needs attention. */
const RANK: ChangeKind[] = ['conflict', 'modified', 'deleted', 'renamed', 'added', 'untracked'];

export class GitIndex {
  readonly status: GitStatus;
  #files = new Map<string, GitChange>();
  /** Untracked folders: everything inside them is new. */
  #newFolders: string[] = [];
  /** Folders holding changes, with the kind that matters most. */
  #folders = new Map<string, ChangeKind>();

  constructor(status: GitStatus) {
    this.status = status;
    const root = status.root;
    for (const c of status.changes ?? []) {
      const path = c.path.endsWith('/') ? c.path.slice(0, -1) : c.path;
      if (c.path.endsWith('/')) this.#newFolders.push(path);
      else this.#files.set(path, c);
      // Every folder from the change up to the repository root.
      for (let dir = parentOf(path); dir.length >= root.length && dir !== path; dir = parentOf(dir)) {
        const was = this.#folders.get(dir);
        const kind = c.status as ChangeKind;
        if (!was || RANK.indexOf(kind) < RANK.indexOf(was)) this.#folders.set(dir, kind);
        if (dir === root || dir === '/') break;
      }
      if (c.path.endsWith('/')) this.#folders.set(path, 'untracked');
    }
  }

  /** A file's change, or null when it's unchanged (or not in the repository). */
  file(path: string): GitChange | null {
    const c = this.#files.get(path);
    if (c) return c;
    const inNew = this.#newFolders.find((d) => path.startsWith(d + '/'));
    return inNew ? { path, status: 'untracked', staged: false, unstaged: true } : null;
  }

  /** The most pressing kind of change inside a folder, or null. */
  folder(path: string): ChangeKind | null {
    return this.#folders.get(path) ?? (this.#newFolders.some((d) => path === d || path.startsWith(d + '/')) ? 'untracked' : null);
  }

  get count(): number {
    return (this.status.changes ?? []).length;
  }
}

function parentOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i <= 0 ? '/' : path.slice(0, i);
}

/** "main ↑2 ↓1" */
export function branchLabel(s: GitStatus): string {
  const parts = [s.detached ? `detached at ${s.branch}` : s.branch || 'no commits yet'];
  if (s.ahead) parts.push(`↑${s.ahead}`);
  if (s.behind) parts.push(`↓${s.behind}`);
  return parts.join(' ');
}
