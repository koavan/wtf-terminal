import { Service, type GitIdentity } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
import type { Pane } from './layout';
import { shellQuote } from './shell';
import { tabs } from './tabs.svelte';
import { runInPane } from './terminals';
import { toast } from './toast.svelte';

export type { GitIdentity, GitHubTest } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';

// Git identities: an SSH key plus the name and email to commit as, used
// per tab so different tabs can work on GitHub as different users (see
// internal/sshhosts/identities.go). Using one types an `export` line into
// the tab's shell, like picking an AWS profile does.

const VARS = ['GIT_SSH_COMMAND', 'GIT_AUTHOR_NAME', 'GIT_AUTHOR_EMAIL', 'GIT_COMMITTER_NAME', 'GIT_COMMITTER_EMAIL', 'WTF_GIT_IDENTITY'];

/** The variable the shell reports for the identity it uses. */
export const IDENTITY_VAR = 'WTF_GIT_IDENTITY';

/**
 * Points a shell at an identity: git connects with only its key
 * (IdentitiesOnly, so ssh-agent's other keys can't log in as someone else)
 * and commits as its author. An author left blank falls back to git config.
 */
export function useIdentityCommand(i: GitIdentity): string {
  // git runs GIT_SSH_COMMAND through sh, so the key path is quoted inside it too.
  const ssh = `ssh -i ${shellQuote(i.keyPath)} -o IdentitiesOnly=yes`;
  const set = [`GIT_SSH_COMMAND=${shellQuote(ssh)}`];
  const unset: string[] = [];
  for (const [value, names] of [
    [i.authorName, ['GIT_AUTHOR_NAME', 'GIT_COMMITTER_NAME']],
    [i.authorEmail, ['GIT_AUTHOR_EMAIL', 'GIT_COMMITTER_EMAIL']],
  ] as const) {
    if (value) set.push(...names.map((n) => `${n}=${shellQuote(value)}`));
    else unset.push(...names);
  }
  set.push(`${IDENTITY_VAR}=${shellQuote(i.name)}`);
  return `export ${set.join(' ')}${unset.length ? `; unset ${unset.join(' ')}` : ''}`;
}

/** Back to the user's own ssh and git config. */
export const CLEAR_IDENTITY_COMMAND = `unset ${VARS.join(' ')}`;

/** The identity a pane's shell reported at its last prompt. */
export function paneIdentity(pane: Pane | null | undefined): string {
  return pane?.env?.[IDENTITY_VAR] ?? '';
}

class GitIdentities {
  list = $state.raw<GitIdentity[]>([]);
  loaded = $state(false);
  #loading: Promise<void> | null = null;

  load(): Promise<void> {
    this.#loading ??= Service.GitIdentities()
      .then((l) => {
        this.list = l ?? [];
        this.loaded = true;
      })
      .finally(() => (this.#loading = null));
    return this.#loading;
  }

  byName(name: string): GitIdentity | undefined {
    return this.list.find((i) => i.name === name);
  }

  async save(i: GitIdentity) {
    this.list = (await Service.SaveGitIdentity(i)) ?? [];
  }

  async remove(id: string) {
    this.list = (await Service.DeleteGitIdentity(id)) ?? [];
  }

  async test(i: GitIdentity) {
    const r = await Service.TestGitIdentity(i.id);
    if (r.ok) await this.load(); // it remembered the account
    return r;
  }

  /** Uses an identity in a pane (the focused one by default). */
  use(i: GitIdentity, pane: Pane | null | undefined = tabs.activePane) {
    if (runInPane(pane, useIdentityCommand(i)) === 'tab') toast.show(`Opened a new tab as ${i.name}: that one was busy`);
  }

  openTab(i: GitIdentity) {
    tabs.open({ command: useIdentityCommand(i) });
  }

  stop(pane: Pane | null | undefined = tabs.activePane) {
    runInPane(pane, CLEAR_IDENTITY_COMMAND);
  }

  /**
   * The command that puts a restored, split or duplicated pane back on its
   * identity, or '' (with a note) when the identity is gone.
   */
  async commandFor(name: string): Promise<string> {
    try {
      await this.load();
    } catch {
      // fall through: treated as gone
    }
    const i = this.byName(name);
    if (i) return useIdentityCommand(i);
    toast.show(`Git identity “${name}” no longer exists: this shell uses your own git and ssh config`, 8000);
    return '';
  }
}

export const gitIdentities = new GitIdentities();
