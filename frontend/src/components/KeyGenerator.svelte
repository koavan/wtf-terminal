<script lang="ts">
  import { onMount } from 'svelte';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
  import Dialog from './Dialog.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { freeKeyName, KEY_TYPES, keygenCommand, message, runInShell, sshKeys, type KeyDraft } from '../lib/sshAdmin.svelte';

  // A new key pair: ssh-keygen runs in the focused shell (a new tab if it's
  // busy) and asks for the passphrase there.

  let draft = $state<KeyDraft>({ type: 'ed25519', name: freeKeyName('id_ed25519', sshKeys.keys), comment: '', addToAgent: true });
  let error = $state('');
  let nameInput = $state<HTMLInputElement>();
  /** The file name follows the type until the user types one. */
  let nameTouched = false;

  onMount(() => nameInput?.select());

  function setType(type: KeyDraft['type']) {
    draft.type = type;
    if (!nameTouched) draft.name = freeKeyName(KEY_TYPES.find((t) => t.id === type)!.file, sshKeys.keys);
  }

  async function generate() {
    error = '';
    try {
      if (!(await Service.KeyNameFree(draft.name.trim()))) {
        error = `~/.ssh/${draft.name.trim()} already exists — choose another name`;
        return;
      }
    } catch (e) {
      error = message(e);
      return;
    }
    const command = keygenCommand(draft);
    overlay.close();
    runInShell(command);
  }
</script>

<Dialog title="New SSH Key" subtitle="Creates a key pair in ~/.ssh" onsubmit={generate}>
  <label class="field">
    <span>Type</span>
    <select value={draft.type} onchange={(e) => setType((e.currentTarget as HTMLSelectElement).value as KeyDraft['type'])}>
      {#each KEY_TYPES as t (t.id)}<option value={t.id}>{t.label}</option>{/each}
    </select>
  </label>

  <label class="field">
    <span>File name</span>
    <input bind:this={nameInput} bind:value={draft.name} oninput={() => (nameTouched = true)} spellcheck="false" autocomplete="off" />
    <small>Saved as <code>~/.ssh/{draft.name.trim() || '…'}</code> and <code>.pub</code></small>
  </label>

  <label class="field">
    <span>Comment</span>
    <input bind:value={draft.comment} placeholder="you@this-computer" spellcheck="false" autocomplete="off" />
    <small>A label stored with the key, shown in authorized_keys on servers.</small>
  </label>

  <label class="check">
    <input type="checkbox" bind:checked={draft.addToAgent} />
    Load it into ssh-agent afterwards{isMac ? ', remembering the passphrase in the Keychain' : ''}
  </label>

  <p class="hint">ssh-keygen runs in your shell (a new tab if it’s busy) and asks for a passphrase there. Leave it empty for none.</p>
  <div class="command">{keygenCommand(draft)}</div>
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={generate} disabled={!draft.name.trim()}>Generate in Terminal</button>
  {/snippet}
</Dialog>
