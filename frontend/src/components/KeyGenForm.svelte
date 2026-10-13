<script lang="ts">
  import { onMount } from 'svelte';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import { Service, type KeyGenResult } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { copySecret, generatePassword } from '../lib/passwords';
  import { isMac } from '../lib/platform';
  import { redaction } from '../lib/redaction.svelte';
  import { agentAddCommand, freeKeyName, KEY_TYPES, message, quotePath, runInShell, sshKeys, type KeyType } from '../lib/sshAdmin.svelte';
  import { toast } from '../lib/toast.svelte';

  // A new SSH key pair from a form: wtf makes it (internal/sshhosts/keygen.go)
  // and writes ~/.ssh/<name> and <name>.pub, never replacing a file. The
  // passphrase only encrypts the new key; it isn't kept.

  let type = $state<KeyType>('ed25519');
  let name = $state('');
  let comment = $state('');
  let passphrase = $state('');
  let confirm = $state('');
  let showPass = $state(false);
  let addToAgent = $state(true);
  let error = $state('');
  let busy = $state(false);
  let result = $state<KeyGenResult | null>(null);
  /** The file name follows the type until the user types one. */
  let nameTouched = false;

  onMount(async () => {
    if (!sshKeys.loaded) await sshKeys.load();
    if (!nameTouched) name = freeKeyName(KEY_TYPES[0].file, sshKeys.keys);
  });

  function setType(t: KeyType) {
    type = t;
    if (!nameTouched) name = freeKeyName(KEY_TYPES.find((k) => k.id === t)!.file, sshKeys.keys);
  }

  const mismatch = $derived(passphrase !== '' && confirm !== '' && passphrase !== confirm);
  const tooShort = $derived(passphrase !== '' && passphrase.length < 5);
  const ready = $derived(!!name.trim() && !busy && passphrase === confirm && !tooShort);

  /** A strong passphrase, shown so it can be saved in a password manager. */
  function suggest() {
    passphrase = confirm = generatePassword({ length: 24, lower: true, upper: true, digits: true, symbols: false, avoidAmbiguous: true });
    showPass = !redaction.on; // not on screen while Hide Secrets is on
    if (redaction.on) toast.show('Passphrase filled in. Hidden while Hide Secrets is on: copy it to your password manager');
  }

  async function generate() {
    if (!ready) return;
    busy = true;
    error = '';
    try {
      result = await Service.GenerateKey({ type, name: name.trim(), comment: comment.trim(), passphrase, addToAgent });
      passphrase = confirm = '';
      showPass = false;
      await sshKeys.load();
      toast.show(`Created ${prettyPath(result.path)}`);
    } catch (e) {
      error = message(e);
    } finally {
      busy = false;
    }
  }

  function another() {
    result = null;
    nameTouched = false;
    comment = '';
    name = freeKeyName(KEY_TYPES.find((k) => k.id === type)!.file, sshKeys.keys);
  }

  async function copyPublic() {
    if (!result) return;
    await Clipboard.SetText(result.publicKey);
    toast.show('Copied the public key');
  }

  async function addToGitHub() {
    await copyPublic();
    Browser.OpenURL('https://github.com/settings/ssh/new');
  }

  const newKey = () => sshKeys.keys.find((k) => k.path === result?.path) ?? null;
</script>

{#if result}
  <div class="done">
    <p class="created"><Icon name="check" size={13} stroke={2.4} /> Created <code>{prettyPath(result.path)}</code> and <code>.pub</code></p>
    <p class="hint">
      {result.fingerprint}<br />
      {#if result.inAgent}Loaded into ssh-agent until it restarts.{:else if result.agentError}Not loaded into ssh-agent: {result.agentError}{/if}
    </p>

    <div class="field">
      <span>Public key: this is what servers and GitHub need</span>
      <div class="box">{result.publicKey}</div>
    </div>
    <div class="actions">
      <button class="primary" onclick={copyPublic}><Icon name="copy" size={12} /> Copy Public Key</button>
      <button class="secondary" onclick={addToGitHub}>Add to GitHub…</button>
      <button class="secondary" onclick={() => overlay.show({ kind: 'keyInstall', key: newKey(), host: null })}>Install on Host…</button>
      <button class="secondary" onclick={() => overlay.show({ kind: 'gitIdentity', identity: null, keyPath: result!.path })}>Use for a Git Identity…</button>
      {#if isMac}
        <button class="secondary" title="Runs ssh-add in your shell, which asks for the passphrase and keeps it in the Keychain" onclick={() => runInShell(agentAddCommand(quotePath(result!.path), true))}>
          Load into Agent and Keychain
        </button>
      {/if}
    </div>
    <button class="link" onclick={another}>Make another key</button>
  </div>
{:else}
  <form onsubmit={(e) => (e.preventDefault(), generate())}>
    <label class="field">
      <span>Type</span>
      <select value={type} onchange={(e) => setType((e.currentTarget as HTMLSelectElement).value as KeyType)}>
        {#each KEY_TYPES as t (t.id)}<option value={t.id}>{t.label}</option>{/each}
      </select>
    </label>

    <label class="field">
      <span>File name</span>
      <input bind:value={name} oninput={() => (nameTouched = true)} spellcheck="false" autocomplete="off" />
      <small>Saved as <code>~/.ssh/{name.trim() || '…'}</code> and <code>.pub</code>. Existing files are never replaced.</small>
    </label>

    <label class="field">
      <span>Comment</span>
      <input bind:value={comment} placeholder="you@this-computer" spellcheck="false" autocomplete="off" />
      <small>A label stored with the key, e.g. which account or machine it's for.</small>
    </label>

    <div class="field">
      <span>Passphrase</span>
      <div class="pass">
        <input
          type={showPass ? 'text' : 'password'}
          bind:value={passphrase}
          placeholder="none"
          autocomplete="new-password"
          spellcheck="false"
          aria-label="Passphrase"
        />
        <button type="button" class="icon-btn" title={showPass ? 'Hide' : 'Show'} aria-label={showPass ? 'Hide passphrase' : 'Show passphrase'} onclick={() => (showPass = !showPass)}>
          <Icon name="eye" size={13} />
        </button>
      </div>
      <input
        type={showPass ? 'text' : 'password'}
        bind:value={confirm}
        placeholder="again"
        autocomplete="new-password"
        spellcheck="false"
        aria-label="Passphrase again"
      />
      <small>
        Encrypts the key file, so a copy of it is useless without the passphrase. Leave both empty for none.
        <button type="button" class="link" onclick={suggest}>Suggest one</button>
        {#if passphrase && !mismatch}· <button type="button" class="link" onclick={() => copySecret(passphrase).then(() => toast.show('Copied. The clipboard clears in 30 seconds'))}>Copy</button>{/if}
      </small>
      {#if mismatch}<small class="bad">The two passphrases differ.</small>{/if}
      {#if tooShort}<small class="bad">Use at least 5 characters, or none.</small>{/if}
    </div>

    <label class="check">
      <input type="checkbox" bind:checked={addToAgent} />
      Load it into ssh-agent
    </label>

    {#if error}<div class="error" role="alert">{error}</div>{/if}

    <div class="actions">
      <button class="primary" type="submit" disabled={!ready}>{busy ? 'Generating…' : 'Generate Key'}</button>
    </div>
    <p class="hint">
      The passphrase is used once, to encrypt the file, and isn't stored. Prefer it never pass through wtf?
      <button type="button" class="link" onclick={() => overlay.show({ kind: 'keyGen' })}>Generate in a terminal instead</button>
    </p>
  </form>
{/if}

<style>
  .pass {
    display: flex;
    gap: 4px;
  }
  .pass input {
    flex: 1;
    min-width: 0;
  }
  .icon-btn {
    display: grid;
    place-items: center;
    flex: none;
    width: 28px;
    height: 28px;
    padding: 0;
    border: 1px solid var(--hairline);
    border-radius: 7px;
    background: var(--identity);
    color: var(--chrome-muted);
  }
  .icon-btn:hover {
    color: var(--chrome-text);
  }
  .bad {
    color: var(--danger) !important;
  }
  .created {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin: 4px 0 6px;
    font-size: 12.5px;
    font-weight: 600;
  }
  .created :global(svg) {
    color: var(--mark-ok);
  }
  .done .hint {
    margin: 0 0 12px;
    font-family: 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  input[type='checkbox'] {
    accent-color: var(--accent);
  }
</style>
