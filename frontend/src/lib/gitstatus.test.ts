import { describe, expect, it } from 'vitest';
import { GitIndex, branchLabel, type GitStatus } from './gitstatus';

const status = (changes: GitStatus['changes'], over: Partial<GitStatus> = {}): GitStatus => ({
  root: '/repo', branch: 'main', detached: false, upstream: 'origin/main', ahead: 0, behind: 0, changes, truncated: false, error: '', ...over,
});
const change = (path: string, s: string) => ({ path, status: s, staged: false, unstaged: true });

describe('GitIndex', () => {
  const idx = new GitIndex(
    status([change('/repo/src/app.go', 'modified'), change('/repo/src/lib/x.go', 'conflict'), change('/repo/new/', 'untracked'), change('/repo/README.md', 'added')]),
  );

  it('knows each changed file', () => {
    expect(idx.file('/repo/src/app.go')?.status).toBe('modified');
    expect(idx.file('/repo/README.md')?.status).toBe('added');
    expect(idx.file('/repo/src/other.go')).toBeNull();
  });

  it('treats everything in an untracked folder as new', () => {
    expect(idx.file('/repo/new/deep/file.txt')?.status).toBe('untracked');
    expect(idx.folder('/repo/new')).toBe('untracked');
    expect(idx.folder('/repo/new/deep')).toBe('untracked');
  });

  it('marks folders with the change that matters most inside them', () => {
    expect(idx.folder('/repo/src')).toBe('conflict');
    expect(idx.folder('/repo/src/lib')).toBe('conflict');
    expect(idx.folder('/repo')).toBe('conflict');
    expect(idx.folder('/repo/docs')).toBeNull();
    expect(idx.count).toBe(4);
  });

  it('stops at the repository root', () => {
    expect(idx.folder('/')).toBeNull();
  });
});

describe('branchLabel', () => {
  it('says the branch and how far it is from its upstream', () => {
    expect(branchLabel(status([], { ahead: 2, behind: 1 }))).toBe('main ↑2 ↓1');
    expect(branchLabel(status([], { detached: true, branch: 'abc1234' }))).toBe('detached at abc1234');
  });
});
