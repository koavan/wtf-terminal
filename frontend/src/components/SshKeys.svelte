<script lang="ts">
  import { onMount } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import {
    agentAddCommand, changePassphraseCommand, message, quotePath, runInShell, sshKeys, type Key,
  } from '../lib/sshAdmin.svelte';
  import { sidebar } from '../lib/sidebar.svelte';
  import { toast } from '../lib/toast.svelte';

  // Key pairs in ~/.ssh and what ssh-agent holds. Reading needs no
  // passphrase; actions that do run in a terminal tab.

  onMount(() => {
    sshKeys.load();
  });

  const files = $derived(sshKeys.keys.filter((k) => k.path));
  const agentOnly = $derived(sshKeys.keys.filter((k) => !k.path));
  const inAgent = $derived(sshKeys.keys.filter((k) => k.inAgent).length);

  /** Runs a quick backend action, then reloads the list. */
  async function act(what: () => Promise<unknown>, done: string) {
    try {
      await what();
      toast.show(done);
    } catch (e) {
      toast.show(message(e), 6000);
    }
    sshKeys.load();
  }

  async function copyPublic(k: Key) {
    try {
      await Clipboard.SetText(await Service.PublicKey(k.path));
      toast.show(`Copied ${k.name}.pub`);
    } catch (e) {
      toast.show(message(e), 6000);
    }
  }

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function confirmDelete(k: Key) {
    overlay.confirm({
      title: `Delete ${k.name}?`,
      message:
        `Moves the key${k.hasPub ? ' and its .pub' : ''} out of ~/.ssh into wtf’s cache folder (ssh-backups/keys), ` +
        `where you can still get it back. Servers that only accept this key will refuse you.` +
        (k.inAgent ? ' The agent keeps its copy until you unload it.' : ''),
      items: k.usedBy.length ? k.usedBy.map((h) => `Used by ${h}`) : [],
      confirmLabel: 'Delete Key',
      onConfirm: () => act(() => Service.DeleteKey(k.path), `Moved ${k.name} to wtf’s cache folder`),
    });
  }

  function menu(e: MouseEvent, k: Key) {
    e.preventDefault();
    const entries: MenuEntry[] = [];
    if (k.path) {
      entries.push(
        { label: 'Copy Public Key', hint: 'double-click', action: () => copyPublic(k) },
        { label: 'Install on Host…', action: () => overlay.show({ kind: 'keyInstall', key: k, host: null }) },
        'separator',
      );
      if (!k.inAgent) {
        entries.push({ label: 'Load into Agent', action: () => runInShell(agentAddCommand(quotePath(k.path), false)) });
        if (isMac) entries.push({ label: 'Load into Agent and Keychain', action: () => runInShell(agentAddCommand(quotePath(k.path), true)) });
      }
    }
    if (k.inAgent) entries.push({ label: 'Unload from Agent', action: () => act(() => Service.AgentRemove(k.fingerprint), `Unloaded ${k.name}`) });
    if (k.path) {
      entries.push({ label: k.encrypted ? 'Change Passphrase…' : 'Set Passphrase…', action: () => runInShell(changePassphraseCommand(k)) });
      if (k.unprotected) entries.push({ label: 'Make Private (chmod 600)', action: () => act(() => Service.ProtectKey(k.path), `${k.name} is now private`) });
      if (!k.hasPub) entries.push({ label: 'Create .pub File', action: () => act(() => Service.WritePublicKey(k.path), `Wrote ${k.name}.pub`) });
    }
    entries.push('separator', { label: 'Copy Fingerprint', action: () => copy(k.fingerprint, 'fingerprint') });
    if (k.path) {
      entries.push(
        { label: isMac ? 'Reveal in Finder' : 'Show in Folder', action: () => Files.Reveal(k.path) },
        'separator',
        { label: 'Delete Key…', danger: true, action: () => confirmDelete(k) },
      );
    }
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  function unloadAll() {
    overlay.confirm({
      title: 'Unload all keys from ssh-agent?',
      message: 'The key files stay in ~/.ssh. Connections already open keep working.',
      items: [],
      confirmLabel: 'Unload All',
      onConfirm: () => act(() => Service.AgentRemoveAll(), 'Unloaded every key'),
    });
  }

  function details(k: Key): string {
    // ED25519 is always 256 bits; RSA and ECDSA keys vary.
    const type = k.bits && /^(RSA|ECDSA)/.test(k.type) ? `${k.type} ${k.bits}` : k.type;
    const made = written(k);
    return [type, made && `from ${made.toLocaleDateString(undefined, { month: 'short', year: 'numeric' })}`, k.comment && k.comment !== k.name ? k.comment : '']
      .filter(Boolean)
      .join(' · ');
  }

  function written(k: Key): Date | null {
    const t = k.modified ? Date.parse(k.modified) : NaN;
    return Number.isNaN(t) ? null : new Date(t);
  }

  function tooltip(k: Key): string {
    const lines = [k.fingerprint];
    if (k.path) lines.push(k.encrypted ? 'Protected by a passphrase' : 'No passphrase (or not recorded in the file)');
    const made = written(k);
    if (made) lines.push(`File written ${made.toLocaleDateString(undefined, { dateStyle: 'medium' })}: when the key was made, unless its passphrase was changed since`);
    if (k.usedBy.length) lines.push(`IdentityFile for ${k.usedBy.join(', ')}`);
    if (k.unprotected) lines.push(`Readable by other users (${k.unprotected}): ssh won’t use it until it’s made private`);
    return lines.join('\n');
  }
</script>

{#snippet row(k: Key)}
  <div
    class="row"
    role="listitem"
    title={tooltip(k)}
    oncontextmenu={(e) => menu(e, k)}
    ondblclick={() => (k.path ? copyPublic(k) : copy(k.fingerprint, 'fingerprint'))}
  >
    <span class="icon" class:agent={k.inAgent}><Icon name="key" size={13} /></span>
    <span class="main">
      <span class="name">{k.path ? prettyPath(k.path).replace(/^~\/\.ssh\//, '') : k.name}</span>
      <span class="sub">{details(k)}</span>
    </span>
    <span class="pills">
      {#if k.unprotected}<span class="pill bad" title="ssh refuses keys other users can read">{k.unprotected}</span>{/if}
      {#if k.inAgent}<span class="pill on">agent</span>{/if}
      {#if k.usedBy.length}<span class="pill">{k.usedBy.length} {k.usedBy.length === 1 ? 'host' : 'hosts'}</span>{/if}
    </span>
  </div>
{/snippet}

<div class="keys">
  <header>
    <span class="title">~/.ssh</span>
    <button class="tool" title="New key" aria-label="New key" onclick={() => sidebar.openGenerator('key')}>
      <Icon name="plus" size={14} />
    </button>
    <button class="tool" title="Reload" aria-label="Reload" disabled={sshKeys.loading} onclick={() => sshKeys.load()}>
      <Icon name="refresh" size={13} />
    </button>
  </header>

  <div class="list" role="list">
    {#each files as k (k.path)}
      {@render row(k)}
    {:else}
      {#if sshKeys.loaded}
        <div class="note">
          <strong>No keys in ~/.ssh</strong>
          A key lets you log in without a password. Press + to make one in the Generate tab, then install it on your servers.
        </div>
      {/if}
    {/each}

    {#if agentOnly.length}
      <div class="section" title="Keys ssh-agent holds that aren’t files in ~/.ssh — from a password manager or a security key, say">Agent only</div>
      {#each agentOnly as k (k.fingerprint)}
        {@render row(k)}
      {/each}
    {/if}

    {#each sshKeys.problems as p (p)}
      <div class="problem">{p}</div>
    {/each}
  </div>

  {#if sshKeys.loaded}
    <footer title={sshKeys.agentSocket ? `SSH_AUTH_SOCK=${sshKeys.agentSocket}` : 'SSH_AUTH_SOCK isn’t set'}>
      {#if sshKeys.agentError}
        <span class="agent-off">ssh-agent: {sshKeys.agentError}</span>
      {:else}
        <span>ssh-agent: {inAgent === 0 ? 'no keys loaded' : `${inAgent} ${inAgent === 1 ? 'key' : 'keys'} loaded`}</span>
        {#if inAgent > 0}<button class="link" onclick={unloadAll}>Unload All</button>{/if}
      {/if}
    </footer>
  {/if}
</div>

<style>
  .keys {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  header {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
    padding: 0 6px 4px 14px;
  }
  .title {
    flex: 1;
    color: var(--chrome-muted);
    font: 11.5px 'JetBrains Mono Variable', monospace;
  }
  .tool {
    display: grid;
    place-items: center;
    flex: none;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .tool:hover:not(:disabled) {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tool:disabled {
    opacity: 0.4;
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }
  .section {
    padding: 10px 8px 4px;
    color: var(--chrome-muted);
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 34px;
    padding: 3px 6px 3px 8px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .icon {
    display: grid;
    place-items: center;
    flex: none;
    color: var(--chrome-muted);
  }
  .icon.agent {
    color: var(--accent);
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .name {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font: 500 12px 'JetBrains Mono Variable', monospace;
  }
  .sub {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--chrome-muted);
    font-size: 11px;
  }
  .pills {
    display: flex;
    gap: 3px;
    flex: none;
  }
  .pill {
    padding: 0 5px;
    border-radius: 4px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
    font-size: 10px;
    font-weight: 600;
    line-height: 16px;
  }
  .pill.on {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .pill.bad {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger);
  }

  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex: none;
    padding: 8px 8px 2px 14px;
    border-top: 1px solid var(--hairline);
    color: var(--chrome-muted);
    font-size: 11.5px;
  }
  .agent-off {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font-size: 11.5px;
  }
  .note {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 8px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .problem {
    padding: 8px;
    color: var(--danger);
    font-size: 11px;
  }
</style>
