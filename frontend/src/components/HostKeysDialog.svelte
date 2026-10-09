<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { ssh, type Host } from '../lib/ssh.svelte';
  import { message, type HostKey, type HostKeys } from '../lib/sshAdmin.svelte';
  import { toast } from '../lib/toast.svelte';

  // A server's host keys: what known_hosts holds, what the server presents
  // now, and forgetting or (re)trusting them — e.g. after a rebuild, when ssh
  // refuses with "REMOTE HOST IDENTIFICATION HAS CHANGED". It shows facts
  // (fingerprints, matches) and leaves the judgement to the user.

  let { host }: { host: Host } = $props();
  const h = untrack(() => host);
  const port = h.known ? h.port : '';

  let info = $state<(HostKeys & { stored: HostKey[] }) | null>(null);
  let scanned = $state.raw<HostKey[] | null>(null);
  let error = $state('');
  let busy = $state(false);
  let confirmForget = $state(false);

  onMount(load);

  async function load() {
    try {
      const r = await Service.HostKeys(h.alias, port);
      info = { ...r, stored: r.stored ?? [], files: r.files ?? [] };
    } catch (e) {
      error = message(e);
    }
  }

  async function act(what: () => Promise<unknown>) {
    busy = true;
    error = '';
    try {
      await what();
    } catch (e) {
      error = message(e);
    } finally {
      busy = false;
    }
  }

  const scan = () =>
    act(async () => {
      scanned = null;
      scanned = (await Service.ScanHostKeys(h.alias, port)) ?? [];
    });

  const forget = () =>
    act(async () => {
      await Service.ForgetHostKeys(h.alias, port);
      confirmForget = false;
      toast.show(`Forgot ${info?.target}’s keys`);
      await load();
      if (h.known) ssh.load();
    });

  const trust = () =>
    act(async () => {
      await Service.TrustHostKeys(h.alias, port, (scanned ?? []).map((k) => k.fingerprint));
      toast.show(`Saved ${info?.target}’s keys`);
      await load();
    });

  const stored = $derived(new Set((info?.stored ?? []).filter((k) => !k.marker).map((k) => k.fingerprint)));
  const revoked = $derived(new Set((info?.stored ?? []).filter((k) => k.marker === 'revoked').map((k) => k.fingerprint)));
  /** How the scan compares with what's stored, as plain facts. */
  const verdict = $derived.by(() => {
    if (!scanned) return '';
    if (scanned.length === 0) return 'The server offered no keys.';
    const known = scanned.filter((k) => stored.has(k.fingerprint)).length;
    if (stored.size === 0) return 'None of these keys are stored yet.';
    if (known === scanned.length) return 'All of the server’s keys match the stored ones.';
    if (known > 0) return `${known} of the server’s ${scanned.length} keys match the stored ones.`;
    return 'None of the server’s keys match the stored ones.';
  });
  const allStored = $derived(!!scanned && scanned.length > 0 && scanned.every((k) => stored.has(k.fingerprint)) && stored.size === scanned.length);

  function copy(fp: string) {
    Clipboard.SetText(fp).then(() => toast.show('Copied fingerprint'));
  }
</script>

{#snippet keyRow(k: HostKey, note: string)}
  <li>
    <span class="type">{k.type}{k.bits && k.type === 'RSA' ? ` ${k.bits}` : ''}</span>
    <button class="fp mono" title="Copy fingerprint" onclick={() => copy(k.fingerprint)}>{k.fingerprint}</button>
    {#if note}<span class="note" class:bad={note === 'revoked'}>{note}</span>{/if}
  </li>
{/snippet}

<Dialog title="Host Keys — {h.alias}" subtitle={info ? `Stored in known_hosts as ${info.target}` : 'Reading known_hosts…'} width={520}>
  <section>
    <h3>Stored</h3>
    {#if info && info.stored.length}
      <ul>
        {#each info.stored as k, i (i)}
          {@render keyRow(k, k.marker || `${prettyPath(k.file)}:${k.line}`)}
        {/each}
      </ul>
    {:else if info}
      <p class="hint">No keys stored for {info.target}. ssh will ask you to confirm the server’s key when you connect.</p>
    {/if}
  </section>

  <section>
    <h3>
      On the server now
      {#if !info?.via}<button class="secondary small" onclick={scan} disabled={busy || !info}>{scanned ? 'Scan Again' : 'Scan'}</button>{/if}
    </h3>
    {#if info?.via}
      <p class="hint">Reached through <code>{info.via}</code>, so wtf can’t ask it directly. Connect, and ssh shows the key it’s offered.</p>
    {:else if scanned}
      {#if scanned.length}
        <ul>
          {#each scanned as k, i (i)}
            {@render keyRow(k, revoked.has(k.fingerprint) ? 'revoked' : stored.has(k.fingerprint) ? 'stored' : 'not stored')}
          {/each}
        </ul>
      {/if}
      <p class="hint">{verdict} Compare fingerprints with ones from the server’s admin (<code>ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub</code>) before trusting new keys.</p>
    {:else}
      <p class="hint">Ask {info?.hostName ?? h.alias} on port {info?.port ?? '22'} which keys it presents (ssh-keyscan). Nothing is saved until you choose.</p>
    {/if}
  </section>

  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    {#if info?.stored.some((k) => !k.marker)}
      {#if confirmForget}
        <button class="danger" onclick={forget} disabled={busy}>Really Forget?</button>
      {:else}
        <button class="danger" onclick={() => (confirmForget = true)} disabled={busy} title="Removes the entries with ssh-keygen -R; the old file is kept as known_hosts.old">Forget Stored Keys</button>
      {/if}
    {/if}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Close</button>
    {#if scanned?.length && !allStored}
      <button class="primary" onclick={trust} disabled={busy} title="Replaces the stored keys with these">
        {stored.size ? 'Replace with Server’s Keys' : 'Trust Server’s Keys'}
      </button>
    {/if}
  {/snippet}
</Dialog>

<style>
  section {
    margin-bottom: 12px;
  }
  h3 {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 0 0 6px;
    color: var(--text-muted);
    font-size: 11.5px;
    font-weight: 600;
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0 0 6px;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
    font-size: 12px;
  }
  .type {
    flex: none;
    width: 74px;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 600;
  }
  .fp {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    padding: 2px 4px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text);
    text-align: left;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fp:hover {
    background: var(--tab-hover);
  }
  .note {
    flex: none;
    max-width: 40%;
    overflow: hidden;
    color: var(--text-muted);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note.bad {
    color: var(--danger);
  }
  .small {
    height: 22px !important;
    padding: 0 10px !important;
    font-size: 11.5px !important;
  }
</style>
