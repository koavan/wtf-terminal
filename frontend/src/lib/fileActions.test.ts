import { describe, expect, it, vi } from 'vitest';

vi.mock('@wailsio/runtime', () => ({ Clipboard: { SetText: vi.fn() } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: {} }));
vi.mock('./backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('./window', () => ({ thisWindow: { name: 'main' } }));

const { gitMenu, changeSummary } = await import('./fileActions');
const { GitIndex } = await import('./gitstatus');

const idx = new GitIndex({
  root: '/repo', branch: 'main', detached: false, upstream: '', ahead: 0, behind: 0, truncated: false, error: '',
  changes: [
    { path: '/repo/src/app.go', status: 'modified', staged: false, unstaged: true },
    { path: '/repo/src/new file.go', status: 'untracked', staged: false, unstaged: true },
    { path: '/repo/staged.go', status: 'modified', staged: true, unstaged: false },
  ],
});

function menu(path: string, dir = false) {
  const run = vi.fn();
  const entries = gitMenu(path, dir, idx, run);
  const labels = entries.map((e) => (e === 'separator' ? '—' : 'heading' in e ? `# ${e.heading}` : e.label));
  const click = (label: string) => {
    const e = entries.find((x) => x !== 'separator' && 'label' in x && x.label === label);
    if (!e || e === 'separator' || !('action' in e)) throw new Error(label);
    e.action();
    return run.mock.calls.at(-1);
  };
  return { labels, click };
}

describe('gitMenu', () => {
  it('shows a modified file’s changes and history, from the repository root', () => {
    const m = menu('/repo/src/app.go');
    expect(m.labels).toEqual(['# Git: Modified · not staged', 'Show Changes', 'Show History']);
    expect(m.click('Show Changes')).toEqual(['git diff HEAD -- src/app.go', '/repo']);
    expect(m.click('Show History')).toEqual(['git log --follow --oneline -- src/app.go', '/repo']);
  });

  it('diffs a new file against nothing, and quotes names', () => {
    const m = menu('/repo/src/new file.go');
    expect(m.labels).toEqual(['# Git: Untracked (new)', 'Show Changes']);
    expect(m.click('Show Changes')).toEqual(["git diff --no-index -- /dev/null 'src/new file.go'", '/repo']);
  });

  it('shows only staged changes when that is all there is', () => {
    expect(menu('/repo/staged.go').click('Show Changes')).toEqual(['git diff --cached -- staged.go', '/repo']);
  });

  it('offers history for unchanged files, and folder views', () => {
    expect(menu('/repo/README.md').labels).toEqual(['Show History']);
    expect(menu('/repo/src', true).labels).toEqual(['# Git: changes inside', 'Show Status of Folder', 'Show Changes in Folder', 'Show Folder History']);
    expect(menu('/elsewhere/x.go')).toMatchObject({ labels: [] });
  });

  it('says where a change is', () => {
    expect(changeSummary({ path: '', status: 'modified', staged: true, unstaged: true })).toBe('Modified · staged and unstaged');
  });
});
