<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
  import Dialog from './Dialog.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { hostKey, ssh, type Host } from '../lib/ssh.svelte';
  import { installKeyCommand, message, runInTab, sshKeys, type Key } from '../lib/sshAdmin.svelte';

  // Puts a public key in a server's authorized_keys with ssh-copy-id, which
  // asks for the password (or uses a key that already works) in a new tab.

  let { key, host }: { key: Key | null; host: Host | null } = $props();

  let keyPath = $state(untrack(() => key?.path ?? ''));
  let hostId = $state(untrack(() => (host ? hostKey(host) : '')));
  let error = $state('');

  onMount(() => {
    if (!sshKeys.loaded) sshKeys.load();
    if (!ssh.loaded) ssh.load();
  });

  const files = $derived(sshKeys.keys.filter((k) => k.path));
  const chosenKey = $derived(files.find((k) => k.path === keyPath) ?? null);
  const chosenHost = $derived(ssh.hosts.find((h) => hostKey(h) === hostId) ?? untrack(() => host));

  // Default to the first key once the list is in.
  $effect(() => {
    if (!keyPath && files.length) keyPath = (files.find((k) => k.inAgent) ?? files[0]).path;
  });

  async function install() {
    if (!chosenKey || !chosenHost) return;
    error = '';
    try {
      // ssh-copy-id reads the .pub file; make it if only the private key exists.
      if (!chosenKey.hasPub) await Service.WritePublicKey(chosenKey.path);
    } catch (e) {
      error = message(e);
      return;
    }
    const command = installKeyCommand(chosenKey, chosenHost);
    overlay.close();
    runInTab(command);
  }
</script>

<Dialog title="Install Public Key" subtitle="Adds the key to the server’s ~/.ssh/authorized_keys, so you can log in with it" onsubmit={install}>
  <label class="field">
    <span>Key</span>
    <select bind:value={keyPath}>
      {#each files as k (k.path)}<option value={k.path}>{k.name} — {k.type}{k.comment ? ` · ${k.comment}` : ''}</option>{/each}
    </select>
  </label>

  <label class="field">
    <span>Host</span>
    <select bind:value={hostId}>
      <option value="" disabled>Choose a host</option>
      {#each ssh.hosts as h (hostKey(h))}<option value={hostKey(h)}>{h.alias}{h.known && h.port ? `:${h.port}` : ''}</option>{/each}
    </select>
  </label>

  <p class="hint">ssh-copy-id runs in a new tab and asks for your password on the server if it needs it. Keys already there aren’t added twice.</p>
  {#if chosenKey && chosenHost}<div class="command">{installKeyCommand(chosenKey, chosenHost)}</div>{/if}
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={install} disabled={!chosenKey || !chosenHost}>Install in Terminal</button>
  {/snippet}
</Dialog>
