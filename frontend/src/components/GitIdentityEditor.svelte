<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { gitIdentities, useIdentityCommand, type GitIdentity } from '../lib/gitIdentities.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { message, sshKeys } from '../lib/sshAdmin.svelte';
  import { sidebar } from '../lib/sidebar.svelte';
  import { toast } from '../lib/toast.svelte';

  // Adds or edits a git identity. Only wtf's own list changes: nothing in
  // ~/.ssh or ~/.gitconfig.

  let { identity, keyPath = '' }: { identity: GitIdentity | null; keyPath?: string } = $props();

  const editing = untrack(() => identity);
  let draft = $state<GitIdentity>(
    editing ? { ...editing } : { id: '', name: '', authorName: '', authorEmail: '', keyPath: untrack(() => keyPath), githubUser: '', testedAt: 0 },
  );
  let useNow = $state(!editing);
  let error = $state('');
  let busy = $state(false);
  let nameInput = $state<HTMLInputElement>();

  onMount(() => {
    nameInput?.focus();
    if (!sshKeys.loaded) sshKeys.load();
  });

  const keys = $derived(sshKeys.keys.filter((k) => k.path));
  /** A key the identity names that isn't in ~/.ssh any more (or lives elsewhere). */
  const missingKey = $derived(!!draft.keyPath && sshKeys.loaded && !keys.some((k) => k.path === draft.keyPath));

  async function save() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await gitIdentities.save(draft);
      const saved = gitIdentities.byName(draft.name.trim());
      overlay.close();
      if (useNow && saved) gitIdentities.use(saved);
      else toast.show(editing ? `Saved ${draft.name.trim()}` : `Added ${draft.name.trim()}`);
    } catch (e) {
      error = message(e);
    } finally {
      busy = false;
    }
  }

  /** The Generate tab makes the key, and offers "Use for a Git Identity…" once it exists. */
  function newKey() {
    overlay.close();
    sidebar.openGenerator('key');
  }
</script>

<Dialog
  title={editing ? `Edit ${editing.name}` : 'New Git Identity'}
  subtitle="An SSH key for GitHub and the author to commit as, for the tabs you choose"
  width={470}
  onsubmit={save}
>
  <label class="field">
    <span>Name</span>
    <input bind:this={nameInput} bind:value={draft.name} placeholder="work" spellcheck="false" autocomplete="off" />
    <small>Shown in the address bar of tabs using it.</small>
  </label>

  <label class="field">
    <span>SSH key</span>
    <select bind:value={draft.keyPath}>
      <option value="" disabled>Choose a key…</option>
      {#if missingKey}<option value={draft.keyPath}>{prettyPath(draft.keyPath)} (not found in ~/.ssh)</option>{/if}
      {#each keys as k (k.path)}
        <option value={k.path}>{prettyPath(k.path)}{k.comment && k.comment !== k.name ? ` — ${k.comment}` : ''}</option>
      {/each}
    </select>
    <small>
      git offers GitHub only this key, so GitHub knows which account you are.
      <button type="button" class="inline-link" onclick={newKey}>New key…</button>
    </small>
  </label>

  <label class="field">
    <span>Commit as</span>
    <input bind:value={draft.authorName} placeholder="Your Name (blank: from git config)" spellcheck="false" autocomplete="off" />
  </label>

  <label class="field">
    <span>Email</span>
    <input bind:value={draft.authorEmail} placeholder="you@example.com (blank: from git config)" spellcheck="false" autocomplete="off" />
    <small>For GitHub to link commits to the account, use an email on it (or its noreply address).</small>
  </label>

  <label class="check">
    <input type="checkbox" bind:checked={useNow} />
    Use it in this tab after saving
  </label>

  {#if draft.keyPath && draft.name.trim()}
    <p class="hint">A tab using it runs:</p>
    <div class="command">{useIdentityCommand({ ...draft, name: draft.name.trim(), authorName: draft.authorName.trim(), authorEmail: draft.authorEmail.trim() })}</div>
  {/if}
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={save} disabled={busy || !draft.name.trim() || !draft.keyPath}>
      {editing ? 'Save' : 'Add Identity'}
    </button>
  {/snippet}
</Dialog>

<style>
  .inline-link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
  }
</style>
