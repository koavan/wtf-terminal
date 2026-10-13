<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Dialogs } from '@wailsio/runtime';
  import Dialog from './Dialog.svelte';
  import { homeDir, prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { type Host } from '../lib/ssh.svelte';
  import { downloadCommand, message, runInShell, uploadCommand } from '../lib/sshAdmin.svelte';

  // Copies files to or from a host with scp, run in the focused shell (a new
  // tab if it's busy) so its progress (and any password prompt) shows there.

  let { host, direction, locals = [] }: { host: Host; direction: 'upload' | 'download'; locals?: string[] } = $props();

  const h = untrack(() => host);
  const upload = untrack(() => direction) === 'upload';
  let files = $state<string[]>(untrack(() => locals));
  let remote = $state('');
  let localDir = $state(homeDir() ? `${homeDir()}/Downloads` : '');
  let error = $state('');
  let remoteInput = $state<HTMLInputElement>();

  onMount(() => {
    if (upload && files.length === 0) pickFiles();
    else remoteInput?.focus();
  });

  async function pickFiles() {
    try {
      const picked = await Dialogs.OpenFile({
        Title: `Upload to ${h.alias}`,
        ButtonText: 'Choose',
        CanChooseFiles: true,
        CanChooseDirectories: true,
        AllowsMultipleSelection: true,
        ShowHiddenFiles: true,
      });
      if (picked?.length) files = [...new Set([...files, ...picked])];
      remoteInput?.focus();
    } catch (e) {
      error = message(e);
    }
  }

  async function pickFolder() {
    try {
      const picked = await Dialogs.OpenFile({
        Title: 'Download into',
        ButtonText: 'Choose',
        CanChooseFiles: false,
        CanChooseDirectories: true,
        CanCreateDirectories: true,
        Directory: localDir,
      });
      if (picked) localDir = picked;
    } catch (e) {
      error = message(e);
    }
  }

  const ready = $derived(upload ? files.length > 0 : remote.trim() !== '' && localDir !== '');
  const command = $derived(ready ? (upload ? uploadCommand(h, files, remote) : downloadCommand(h, remote, localDir)) : '');

  function start() {
    if (!ready) return;
    const c = command;
    overlay.close();
    runInShell(c);
  }
</script>

<Dialog title={upload ? `Upload to ${h.alias}` : `Download from ${h.alias}`} subtitle="Copies with scp in your shell (a new tab if it’s busy), where you’ll see its progress" width={480} onsubmit={start}>
  {#if upload}
    <div class="field">
      <span>Files and folders</span>
      {#if files.length}
        <ul>
          {#each files as f (f)}
            <li>
              <span class="mono">{prettyPath(f)}</span>
              <button class="remove" aria-label="Remove {f}" onclick={() => (files = files.filter((x) => x !== f))}>×</button>
            </li>
          {/each}
        </ul>
      {/if}
      <div><button class="secondary" onclick={pickFiles}>Choose…</button></div>
    </div>
    <label class="field">
      <span>Into folder on {h.alias}</span>
      <input bind:this={remoteInput} bind:value={remote} placeholder="your home folder" spellcheck="false" autocomplete="off" />
      <small>A path on the server, like <code>/tmp</code> or <code>~/deploy</code>. Empty means your home folder.</small>
    </label>
  {:else}
    <label class="field">
      <span>File or folder on {h.alias}</span>
      <input bind:this={remoteInput} bind:value={remote} placeholder="/var/log/app.log" spellcheck="false" autocomplete="off" />
    </label>
    <div class="field">
      <span>Into folder here</span>
      <div class="row">
        <input class="grow" value={prettyPath(localDir)} readonly />
        <button class="secondary" onclick={pickFolder}>Choose…</button>
      </div>
    </div>
  {/if}

  {#if command}<div class="command">{command}</div>{/if}
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={start} disabled={!ready}>{upload ? 'Upload' : 'Download'}</button>
  {/snippet}
</Dialog>

<style>
  ul {
    max-height: 120px;
    overflow: auto;
    margin: 0 0 4px;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 22px;
    font-size: 12px;
  }
  li .mono {
    flex: 1;
    min-width: 0;
  }
  .remove {
    flex: none;
    width: 20px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--text-muted);
  }
  .remove:hover {
    background: var(--tab-hover);
    color: var(--text);
  }
</style>
