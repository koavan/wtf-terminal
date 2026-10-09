<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { ssh, type Host } from '../lib/ssh.svelte';
  import {
    draftOfTunnel, emptyTunnel, message, sshTunnels, tunnelSpec, type TunnelDraft, type TunnelKind, type TunnelStatus,
  } from '../lib/sshAdmin.svelte';
  import { toast } from '../lib/toast.svelte';

  let { tunnel, host }: { tunnel: TunnelStatus | null; host: Host | null } = $props();

  const editing = untrack(() => tunnel);
  const from = untrack(() => host);
  let draft = $state<TunnelDraft>(
    editing ? draftOfTunnel(editing) : emptyTunnel(from?.alias ?? '', from?.known && from.port !== '22' ? from.port : ''),
  );
  let error = $state('');
  let busy = $state(false);
  let firstInput = $state<HTMLInputElement>();

  onMount(() => {
    if (!ssh.loaded) ssh.load();
    firstInput?.focus();
  });

  const KINDS: { id: TunnelKind; label: string; flag: string }[] = [
    { id: 'local', label: 'Local', flag: '-L' },
    { id: 'remote', label: 'Remote', flag: '-R' },
    { id: 'dynamic', label: 'SOCKS', flag: '-D' },
  ];
  const aliases = $derived([...new Set(ssh.hosts.filter((h) => !h.known).map((h) => h.alias))]);
  const via = $derived(draft.host.trim() || 'the host');

  async function save(start: boolean) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await sshTunnels.save(editing?.id ?? '', draft);
      const id = editing?.id ?? sshTunnels.list.at(-1)?.id;
      if (start && id) await sshTunnels.start(id);
      overlay.close();
      if (!start) toast.show(editing ? 'Saved tunnel' : 'Added tunnel');
    } catch (e) {
      error = message(e);
    } finally {
      busy = false;
    }
  }

  const running = editing && editing.state !== 'stopped' && editing.state !== 'failed';
</script>

<Dialog title={editing ? 'Edit Tunnel' : 'New Tunnel'} subtitle="A port forward over ssh, run in the background while it’s on" onsubmit={() => save(!editing)}>
  <div class="kinds" role="radiogroup" aria-label="Kind">
    {#each KINDS as k (k.id)}
      <button role="radio" aria-checked={draft.kind === k.id} class:on={draft.kind === k.id} onclick={() => (draft.kind = k.id)}>
        {k.label} <span class="flag">{k.flag}</span>
      </button>
    {/each}
  </div>
  <p class="hint explain">
    {#if draft.kind === 'local'}
      Reach a service near the server — a database, an internal web app — on a port of this computer.
    {:else if draft.kind === 'remote'}
      Let the server reach a service on this computer, through a port on the server.
    {:else}
      A SOCKS proxy on this computer that sends traffic out through the server — point a browser at it.
    {/if}
  </p>

  <div class="row">
    <label class="field grow">
      <span>Through host</span>
      <input bind:this={firstInput} bind:value={draft.host} list="tunnel-hosts" placeholder="bastion" spellcheck="false" autocomplete="off" />
      <datalist id="tunnel-hosts">{#each aliases as a (a)}<option value={a}></option>{/each}</datalist>
    </label>
    <label class="field port">
      <span>SSH port</span>
      <input bind:value={draft.port} placeholder="config" inputmode="numeric" spellcheck="false" autocomplete="off" />
    </label>
  </div>

  {#if draft.kind === 'dynamic'}
    <label class="field">
      <span>SOCKS port here</span>
      <input bind:value={draft.listenPort} placeholder="1080" spellcheck="false" autocomplete="off" />
    </label>
  {:else}
    <div class="row">
      <label class="field port">
        <span>{draft.kind === 'local' ? 'Port here' : 'Port on server'}</span>
        <input bind:value={draft.listenPort} placeholder="8080" spellcheck="false" autocomplete="off" />
      </label>
      <span class="arrow">→</span>
      <label class="field grow">
        <span>{draft.kind === 'local' ? `Destination, as seen from ${via}` : 'Destination, as seen from here'}</span>
        <input bind:value={draft.destHost} placeholder="localhost" spellcheck="false" autocomplete="off" />
      </label>
      <label class="field port">
        <span>Port</span>
        <input bind:value={draft.destPort} placeholder="80" inputmode="numeric" spellcheck="false" autocomplete="off" />
      </label>
    </div>
  {/if}

  <label class="field">
    <span>Name</span>
    <input bind:value={draft.name} placeholder="optional, e.g. staging db" spellcheck="false" autocomplete="off" />
  </label>

  <label class="check">
    <input type="checkbox" bind:checked={draft.autoStart} />
    Start when wtf opens
  </label>

  <div class="command">ssh -N {KINDS.find((k) => k.id === draft.kind)?.flag} {tunnelSpec(draft) || '…'} {draft.port ? `-p ${draft.port} ` : ''}{draft.host || '…'}</div>
  <p class="hint">Runs without prompts, so the host must accept a key from ssh-agent or a key without a passphrase.{running ? ' Saving restarts it.' : ''}</p>
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    {#if !editing}<button class="secondary" onclick={() => save(false)} disabled={busy}>Save</button>{/if}
    <button class="primary" onclick={() => save(!editing)} disabled={busy}>{editing ? 'Save' : 'Save & Start'}</button>
  {/snippet}
</Dialog>

<style>
  .kinds {
    display: flex;
    gap: 2px;
    margin: 0 0 6px;
    padding: 2px;
    border-radius: 8px;
    background: var(--identity);
  }
  .kinds button {
    flex: 1;
    height: 26px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
    font-size: 12.5px;
    font-weight: 550;
  }
  .kinds button.on {
    background: var(--popover);
    color: var(--text);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.15);
  }
  .flag {
    opacity: 0.6;
    font: 11px 'JetBrains Mono Variable', monospace;
  }
  .explain {
    margin: 0 0 12px;
  }
  .port {
    width: 84px;
    flex: none;
  }
  .arrow {
    align-self: center;
    margin-top: 10px;
    color: var(--text-muted);
  }
</style>
