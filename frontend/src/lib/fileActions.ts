import { Clipboard } from '@wailsio/runtime';
import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
import { LABEL, type ChangeKind, type GitChange, type GitIndex } from './gitstatus';
import type { MenuEntry } from './overlay.svelte';
import { isMac } from './platform';
import { relativeTo, shellQuote } from './shell';
import { sidebar, type Entry } from './sidebar.svelte';
import { tabs } from './tabs.svelte';
import { activeTerminal } from './terminals';
import { toast } from './toast.svelte';

/** The path as the focused pane's shell should see it: relative when inside its folder. */
export function pathForShell(path: string): string {
  return shellQuote(relativeTo(tabs.activePane?.cwd ?? '', path));
}

/** Puts a path at the prompt, without running anything. */
export function insertPath(path: string) {
  const term = activeTerminal();
  if (!term) return;
  term.paste(pathForShell(path) + ' ');
  term.focus();
}

/** Whether the focused pane's shell is sitting at its prompt. */
export function shellIdle(): boolean {
  const pane = tabs.activePane;
  return !!pane && pane.isShell && !pane.running;
}

export function cdTo(path: string) {
  const term = activeTerminal();
  if (!term) return;
  if (!shellIdle()) {
    toast.show('The shell is busy — finish the running program first');
    return;
  }
  // Ctrl-U clears anything half-typed at the prompt first.
  term.type(`\x15cd ${shellQuote(path)}\r`);
  term.focus();
}

/** Opens a file in $EDITOR in a new split; at a line with +N, which vi, nano, emacs and micro understand. */
export function openInEditor(path: string, line?: number) {
  const at = line ? `+${line} ` : '';
  tabs.split('row', { cwd: path.slice(0, path.lastIndexOf('/')) || '/', command: `\${EDITOR:-vi} ${at}${shellQuote(path)}` });
}

function copy(text: string, what: string) {
  Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
}

const fail = (verb: string) => (err: unknown) => toast.show(`Couldn't ${verb}: ${err instanceof Error ? err.message : err}`);

export function entryMenu(entry: Entry): MenuEntry[] {
  const reveal = isMac ? 'Reveal in Finder' : 'Open Containing Folder';
  const relative = relativeTo(tabs.activePane?.cwd ?? '', entry.path);
  const common: MenuEntry[] = [
    { label: reveal, action: () => Files.Reveal(entry.path).catch(fail('reveal it')) },
    'separator',
    { label: 'Copy Path', action: () => copy(entry.path, 'path') },
    { label: 'Copy Relative Path', action: () => copy(relative, 'relative path') },
  ];
  if (entry.dir) {
    return [
      { label: 'cd Here', hint: 'double-click', action: () => cdTo(entry.path) },
      { label: 'Open in New Tab', action: () => tabs.open({ cwd: entry.path }) },
      { label: 'Open in New Split', action: () => tabs.split('row', { cwd: entry.path }) },
      { label: 'Insert Path', action: () => insertPath(entry.path) },
      'separator',
      { label: 'Make Root', action: () => (sidebar.pinned = entry.path) },
      ...common,
    ];
  }
  return [
    { label: 'Insert Path', hint: 'click', action: () => insertPath(entry.path) },
    { label: 'Open in Editor', action: () => openInEditor(entry.path) },
    { label: 'Open with Default App', action: () => Files.Open(entry.path).catch(fail('open it')) },
    'separator',
    ...common,
  ];
}

/** "Modified · staged and unstaged": what git says about a change. */
export function changeSummary(c: GitChange): string {
  const where = c.status === 'untracked' || c.status === 'conflict' ? '' : c.staged && c.unstaged ? ' · staged and unstaged' : c.staged ? ' · staged' : ' · not staged';
  return LABEL[c.status as ChangeKind] + where;
}

/**
 * Git's read-only views of a path: its changes and its history, as git
 * commands in a new tab at the repository's root. Nothing here stages,
 * commits or discards.
 */
export function gitMenu(path: string, dir: boolean, git: GitIndex, run = (command: string, cwd: string) => tabs.open({ command, cwd })): MenuEntry[] {
  const root = git.status.root;
  if (path !== root && !path.startsWith(root + '/')) return [];
  const rel = shellQuote(relativeTo(root, path));
  const entries: MenuEntry[] = [];
  if (dir) {
    const kind = git.folder(path);
    if (kind) {
      entries.push({ heading: 'Git: changes inside' });
      entries.push({ label: 'Show Status of Folder', action: () => run(`git status -- ${rel}`, root) });
      entries.push({ label: 'Show Changes in Folder', action: () => run(`git diff HEAD -- ${rel}`, root) });
    }
    entries.push({ label: 'Show Folder History', action: () => run(`git log --oneline -- ${rel}`, root) });
    return entries;
  }
  const c = git.file(path);
  if (c) {
    entries.push({ heading: `Git: ${changeSummary(c)}` });
    const diff =
      c.status === 'untracked' ? `git diff --no-index -- /dev/null ${rel}`
      : c.status === 'conflict' ? `git diff -- ${rel}`
      : c.staged && !c.unstaged ? `git diff --cached -- ${rel}`
      : `git diff HEAD -- ${rel}`;
    entries.push({ label: 'Show Changes', action: () => run(diff, root) });
  }
  if (c?.status !== 'untracked' && c?.status !== 'added') {
    entries.push({ label: 'Show History', action: () => run(`git log --follow --oneline -- ${rel}`, root) });
  }
  return entries;
}
