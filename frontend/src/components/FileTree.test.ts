// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

// A fake filesystem behind the Files service.
const fs: Record<string, { name: string; dir?: boolean; ignored?: boolean }[]> = {
  '/w/proj': [{ name: 'src', dir: true }, { name: 'build', dir: true, ignored: true }, { name: 'My Notes.md' }],
  '/w/proj/src': [{ name: 'main.go' }, { name: 'util.go' }],
};
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({
  Service: {
    List: vi.fn(async (path: string) => {
      if (!fs[path]) throw new Error('no longer exists');
      return {
        path,
        truncated: false,
        entries: fs[path].map((e) => ({ name: e.name, path: `${path}/${e.name}`, dir: !!e.dir, symlink: false, hidden: e.name.startsWith('.'), ignored: !!e.ignored })),
      };
    }),
    Open: vi.fn(async () => {}),
    Reveal: vi.fn(async () => {}),
    GitStatus: vi.fn(async (dir: string) => gitStatus(dir)),
  },
}));
/** The fake repository's status; outside /w/proj there's none. */
let gitChanges: { path: string; status: string; staged: boolean; unstaged: boolean }[] = [];
function gitStatus(dir: string) {
  if (!dir.startsWith('/w/proj')) return { root: '', branch: '', detached: false, upstream: '', ahead: 0, behind: 0, changes: [], truncated: false, error: '' };
  return { root: '/w/proj', branch: 'main', detached: false, upstream: 'origin/main', ahead: 1, behind: 0, changes: gitChanges, truncated: false, error: '' };
}
const setAutoRefresh = vi.fn(async (_on: boolean) => {});
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({
  Service: { SetSidebarAutoRefresh: (on: boolean) => setAutoRefresh(on) },
}));
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
}));
// jsdom doesn't scroll.
Element.prototype.scrollIntoView ??= function () {};
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;

const { tabs } = await import('../lib/tabs.svelte');
const { settings } = await import('../lib/settings.svelte');
const { testConfig } = await import('../test/config');
const { fileTree, sidebar } = await import('../lib/sidebar.svelte');
const { registerTerminal } = await import('../lib/terminals');
const FileTree = (await import('./FileTree.svelte')).default;

const term = {
  focus: vi.fn(), clear: vi.fn(), selectAll: vi.fn(), getSelection: () => '',
  paste: vi.fn(), type: vi.fn(), sendInput: vi.fn(), jumpPrompt: vi.fn(), lastOutput: () => null, blockOutput: () => null, pinLastOutput: () => false, plainText: () => '', transcript: () => null, revealCommand: () => true, serialize: () => '', snapshot: () => [],
};

beforeEach(() => {
  settings.config = testConfig();
  tabs.list = [];
  tabs.groups = [];
  tabs.open({ cwd: '/w/proj' });
  const pane = tabs.activePane!;
  pane.cwd = '/w/proj';
  pane.process = 'bash';
  registerTerminal(pane.id, term);
  fileTree.folders.clear();
  fileTree.expanded.clear();
  fileTree.selected = null;
  fileTree.history.clear();
  sidebar.pinned = null;
  fileTree.git = null;
  fileTree.changesOnly = false;
  gitChanges = [];
  vi.clearAllMocks();
});
afterEach(cleanup);

const rowNames = (c: HTMLElement) => [...c.querySelectorAll('.row .name')].map((n) => n.textContent);

describe('FileTree', () => {
  it("lists the focused pane's folder, folders first, ignored ones dimmed", async () => {
    const { container } = render(FileTree);
    await waitFor(() => expect(rowNames(container)).toEqual(['src', 'build', 'My Notes.md']));
    const build = [...container.querySelectorAll('.row')][1];
    expect(build.classList.contains('dim')).toBe(true);
  });

  it('expands a folder on click', async () => {
    const screen = render(FileTree);
    await fireEvent.click(await screen.findByText('src'), { detail: 1 });
    await waitFor(() => expect(rowNames(screen.container)).toContain('main.go'));
  });

  it('clicking a file inserts its quoted, relative path', async () => {
    const screen = render(FileTree);
    await fireEvent.click(await screen.findByText('My Notes.md'), { detail: 1 });
    expect(term.paste).toHaveBeenCalledWith(`'My Notes.md' `);
    expect(term.focus).toHaveBeenCalled();
  });

  it('double-clicking a folder cds into it when the shell is idle', async () => {
    const screen = render(FileTree);
    const src = await screen.findByText('src');
    await fireEvent.click(src, { detail: 1 });
    await fireEvent.click(src, { detail: 2 });
    expect(term.type).toHaveBeenCalledWith('\x15cd /w/proj/src\r');
    expect(fileTree.expanded.has('/w/proj/src')).toBe(false); // net: unchanged
  });

  it('does not cd while a program is running', async () => {
    const pane = tabs.activePane!;
    pane.running = true;
    const screen = render(FileTree);
    const src = await screen.findByText('src');
    await fireEvent.click(src, { detail: 1 });
    await fireEvent.click(src, { detail: 2 });
    expect(term.type).not.toHaveBeenCalled();
  });

  it('filters loaded folders, keeping the path to each match', async () => {
    const screen = render(FileTree);
    await fireEvent.click(await screen.findByText('src'), { detail: 1 });
    await screen.findByText('main.go');
    await fireEvent.input(screen.getByPlaceholderText('Filter'), { target: { value: 'util' } });
    await waitFor(() => expect(rowNames(screen.container)).toEqual(['src', 'util.go']));
  });

  it('a pinned root stays put when the pane moves elsewhere', async () => {
    sidebar.pinned = '/w/proj/src';
    const { container } = render(FileTree);
    await waitFor(() => expect(rowNames(container)).toEqual(['main.go', 'util.go']));
  });

  it('shows a note instead of local files during ssh', async () => {
    tabs.activePane!.process = 'ssh';
    tabs.activePane!.isShell = false;
    const screen = render(FileTree);
    expect(await screen.findByText('Remote session')).toBeTruthy();
  });

  it('says why a folder cannot be shown', async () => {
    tabs.activePane!.cwd = '/gone';
    const screen = render(FileTree);
    expect(await screen.findByText('no longer exists')).toBeTruthy();
  });

  it('moves the selection with the arrow keys and opens folders with →', async () => {
    const screen = render(FileTree);
    await screen.findByText('src');
    const tree = screen.getByRole('tree');
    await fireEvent.keyDown(tree, { key: 'ArrowDown' });
    expect(fileTree.selected).toBe('/w/proj/src');
    await fireEvent.keyDown(tree, { key: 'ArrowRight' });
    await waitFor(() => expect(rowNames(screen.container)).toContain('main.go'));
    await fireEvent.keyDown(tree, { key: 'ArrowRight' });
    expect(fileTree.selected).toBe('/w/proj/src/main.go');
    await fireEvent.keyDown(tree, { key: 'Enter' });
    expect(term.paste).toHaveBeenCalledWith('src/main.go ');
  });
});

describe('back and up', () => {
  const moveTo = async (cwd: string) => {
    tabs.activePane!.cwd = cwd;
    await waitFor(() => expect(fileTree.history.get(`pane:${tabs.activePane!.id}`)?.at(-1)).toBe(cwd));
  };

  it('goes up by cd-ing the shell, which the tree follows', async () => {
    const view = render(FileTree);
    await view.findByText('src');
    await fireEvent.click(view.getByLabelText('Up'));
    expect(term.type).toHaveBeenLastCalledWith('\x15cd /w\r');
  });

  it('goes back to the folder shown before, per pane', async () => {
    const view = render(FileTree);
    await view.findByText('src');
    expect((view.getByLabelText('Back') as HTMLButtonElement).disabled).toBe(true);
    await moveTo('/w/proj/src');
    await waitFor(() => expect((view.getByLabelText('Back') as HTMLButtonElement).disabled).toBe(false));
    expect(view.getByLabelText('Back').title).toBe('Back to /w/proj (⌫)');
    await fireEvent.click(view.getByLabelText('Back'));
    expect(term.type).toHaveBeenLastCalledWith('\x15cd /w/proj\r');
    await moveTo('/w/proj');
    // Back used up its step: no ping-pong between the two folders.
    expect(fileTree.history.get(`pane:${tabs.activePane!.id}`)).toEqual(['/w/proj']);
    await waitFor(() => expect((view.getByLabelText('Back') as HTMLButtonElement).disabled).toBe(true));
  });

  it('keeps the step when the shell is busy', async () => {
    const view = render(FileTree);
    await view.findByText('src');
    await moveTo('/w/proj/src');
    tabs.activePane!.running = true;
    term.type.mockClear();
    await fireEvent.click(view.getByLabelText('Back'));
    expect(term.type).not.toHaveBeenCalled();
    expect(fileTree.history.get(`pane:${tabs.activePane!.id}`)).toEqual(['/w/proj', '/w/proj/src']);
    tabs.activePane!.running = false;
  });

  it('moves a pinned tree without touching the shell, with keys too', async () => {
    sidebar.pinned = '/w/proj/src';
    const view = render(FileTree);
    await view.findByText('main.go');
    const tree = view.container.querySelector('.rows') as HTMLElement;
    await fireEvent.keyDown(tree, { key: 'ArrowUp', altKey: true });
    expect(sidebar.pinned).toBe('/w/proj');
    await view.findByText('src');
    await fireEvent.keyDown(view.container.querySelector('.rows') as HTMLElement, { key: 'Backspace' });
    expect(sidebar.pinned).toBe('/w/proj/src');
    expect(term.type).not.toHaveBeenCalledWith(expect.stringContaining('cd '));
    sidebar.pinned = null;
  });
});

describe('auto-refresh', () => {
  it('re-reads folders after a command only while auto-refresh is on', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/files');
      const list = vi.mocked(Service.List);
      const pane = tabs.activePane!;

      settings.config = testConfig({ sidebarAutoRefresh: false });
      const screen = render(FileTree);
      await screen.findByText('src');
      list.mockClear();
      pane.lastCommand = { text: 'touch x', exitCode: 0, durationMs: 5 };
      await vi.advanceTimersByTimeAsync(1000);
      expect(list).not.toHaveBeenCalled();

      settings.config = testConfig({ sidebarAutoRefresh: true });
      pane.lastCommand = { text: 'touch y', exitCode: 0, durationMs: 5 };
      await vi.advanceTimersByTimeAsync(1000);
      expect(list).toHaveBeenCalledWith('/w/proj');
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('sidebar state', () => {
  it('clamps width and restores only valid values', () => {
    sidebar.setWidth(5000);
    expect(sidebar.width).toBe(520);
    sidebar.restore({ open: true, width: 300, pinned: 'relative/nope' });
    expect(sidebar.open).toBe(true);
    expect(sidebar.width).toBe(300);
    expect(sidebar.pinned).toBeNull();
    sidebar.restore('garbage');
    expect(sidebar.width).toBe(300);
  });

  it("shows the repository's branch and marks changed files and the folders holding them", async () => {
    gitChanges = [
      { path: '/w/proj/src/main.go', status: 'modified', staged: false, unstaged: true },
      { path: '/w/proj/My Notes.md', status: 'untracked', staged: false, unstaged: true },
      { path: '/w/proj/old.go', status: 'deleted', staged: true, unstaged: false },
    ];
    const { container, getByText, getByRole } = render(FileTree);
    await waitFor(() => expect(getByText('main ↑1')).toBeTruthy());
    const rows = () => [...container.querySelectorAll('.row')];
    await waitFor(() => expect(rows()[0].classList.contains('git-modified')).toBe(true)); // src holds a change
    expect(rows()[0].querySelector('.gitdot')).toBeTruthy();
    expect(rows()[2].querySelector('.letter')?.textContent).toBe('U');

    // The changes list, deleted files included.
    await fireEvent.click(getByRole('button', { name: /3 changes/ }));
    await waitFor(() => expect(rowNames(container)).toEqual(['My Notes.md', 'old.go', 'main.go']));
    expect(container.querySelector('.row.gone .name')?.textContent).toBe('old.go');
  });

  it('shows nothing about git outside a repository', async () => {
    tabs.activePane!.cwd = '/w';
    fs['/w'] = [{ name: 'proj', dir: true }];
    const { container } = render(FileTree);
    await waitFor(() => expect(rowNames(container)).toEqual(['proj']));
    expect(container.querySelector('.gitbar')).toBeNull();
  });
});
