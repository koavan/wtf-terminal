import { describe, expect, it, vi } from 'vitest';

vi.mock('../../bindings/github.com/dayanraj/wtf/internal/sshhosts', () => ({
  Service: { GitIdentities: vi.fn(async () => [ident({})]) },
}));

import { CLEAR_IDENTITY_COMMAND, gitIdentities, useIdentityCommand, type GitIdentity } from './gitIdentities.svelte';

function ident(i: Partial<GitIdentity>): GitIdentity {
  return { id: '1', name: 'work', authorName: 'Ada L', authorEmail: 'ada@work.example', keyPath: '/Users/ada/.ssh/id_work', githubUser: '', testedAt: 0, ...i };
}

describe('useIdentityCommand', () => {
  it('offers only the key and commits as the author', () => {
    expect(useIdentityCommand(ident({}))).toBe(
      "export GIT_SSH_COMMAND='ssh -i /Users/ada/.ssh/id_work -o IdentitiesOnly=yes'" +
        " GIT_AUTHOR_NAME='Ada L' GIT_COMMITTER_NAME='Ada L'" +
        ' GIT_AUTHOR_EMAIL=ada@work.example GIT_COMMITTER_EMAIL=ada@work.example WTF_GIT_IDENTITY=work',
    );
  });

  it('quotes a key path twice: for the shell, and for the sh git runs it with', () => {
    const cmd = useIdentityCommand(ident({ keyPath: "/Users/a b/.ssh/it's" }));
    expect(cmd).toContain(`GIT_SSH_COMMAND='ssh -i '\\''/Users/a b/.ssh/it'\\''\\'\\'''\\''s'\\'' -o IdentitiesOnly=yes'`);
  });

  it('falls back to git config for a blank author, clearing the last identity', () => {
    expect(useIdentityCommand(ident({ authorName: '', authorEmail: '' }))).toMatch(
      /WTF_GIT_IDENTITY=work; unset GIT_AUTHOR_NAME GIT_COMMITTER_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL$/,
    );
  });

  it('stops by unsetting every variable', () => {
    for (const v of ['GIT_SSH_COMMAND', 'GIT_AUTHOR_NAME', 'GIT_COMMITTER_EMAIL', 'WTF_GIT_IDENTITY']) {
      expect(CLEAR_IDENTITY_COMMAND).toContain(v);
    }
  });
});

describe('commandFor', () => {
  it('finds a saved identity by name, and gives nothing for a deleted one', async () => {
    expect(await gitIdentities.commandFor('work')).toContain('WTF_GIT_IDENTITY=work');
    expect(await gitIdentities.commandFor('gone')).toBe('');
  });
});
