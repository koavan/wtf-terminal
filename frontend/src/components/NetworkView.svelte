<script lang="ts">
  import { untrack } from 'svelte';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import PausedNote from './PausedNote.svelte';
  import RefreshControls from './RefreshControls.svelte';
  import { isExposed, mergeListeners, network, serviceName, splitAddress, type Conn } from '../lib/network.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { tabs, tabTitle } from '../lib/tabs.svelte';
  import { prettyPath } from '../lib/backend';
  import { poll, sidebarInterval } from '../lib/poller';
  import { settings } from '../lib/settings.svelte';
  import { toast } from '../lib/toast.svelte';
  import ForwardRow from './ForwardRow.svelte';
  import { describeTunnel, localPort, sshTunnels, type TunnelStatus } from '../lib/sshAdmin.svelte';

  let showOther = $state(false);

  // Port forwards (ssh tunnels, kubectl port-forwards): run by wtf, which
  // reports every change, so nothing polls for them.
  $effect(() => {
    if (!sshTunnels.loaded) untrack(() => sshTunnels.load().catch(() => {}));
  });

  const activeForward = (t: TunnelStatus) => t.state === 'up' || t.state === 'connecting' || t.state === 'retrying';
  /** The running forward listening on a local port, if any. */
  function forwardOn(port: string): TunnelStatus | undefined {
    return sshTunnels.list.find((t) => activeForward(t) && localPort(t) === port);
  }
  const forwards = $derived(
    sshTunnels.list.filter((t) => {
      const q = network.filter.trim().toLowerCase();
      return !q || `${t.name} ${describeTunnel(t)} ${t.command}`.toLowerCase().includes(q);
    }),
  );

  // Polls while shown; with auto-refresh off, loads once (see lib/poller.ts).
  $effect(() => {
    const ms = sidebarInterval(settings.config);
    return untrack(() => poll(() => network.refresh(), ms));
  });

  /** Where a connection came from, in this window's terms. */
  function owner(c: Conn): { label: string; tabId: number | null } | null {
    if (!c.session) return null;
    const found = tabs.bySession(c.session);
    if (!found) return { label: 'Another window', tabId: null };
    const title = tabTitle(found.tab, prettyPath);
    return { label: title, tabId: found.tab.id };
  }

  const visible = $derived.by(() => {
    const q = network.filter.trim().toLowerCase();
    // "pid:1234" matches one process exactly (the Processes tab links here that way).
    const pid = /^pid:\s*(\d+)$/.exec(q)?.[1];
    return network.conns.filter((c) => {
      if (pid) return c.pid === Number(pid);
      // A forward's listener counts as yours: wtf runs it for you.
      if (network.scope === 'terminals' && !c.session && !(c.state === 'LISTEN' && forwardOn(splitAddress(c.local).port))) return false;
      if (!q) return true;
      return `${c.process} ${c.local} ${c.remote} ${c.pid}`.toLowerCase().includes(q);
    });
  });
  const listening = $derived(mergeListeners(visible.filter((c) => c.state === 'LISTEN')));
  // Probe tools like ping have no connected peer; their target counts.
  const connected = $derived(visible.filter((c) => c.state !== 'LISTEN' && (c.remote || c.target)));
  const other = $derived(visible.filter((c) => c.state !== 'LISTEN' && !c.remote && !c.target));

  /** Where a connection goes: its peer, or a probe tool's target. */
  function destination(c: Conn): { host: string; port: string; name: string; note: string } {
    if (c.remote) return { ...splitAddress(c.remote), name: '', note: '' };
    const ip = c.targetIP || c.target;
    const name = c.targetIP && c.target !== c.targetIP ? c.target : '';
    const note = c.targetResolved
      ? `${c.target} resolved here to ${c.targetIP}; ${c.process} may be using another of its addresses`
      : c.targetIP
        ? ''
        : `Couldn't resolve ${c.target}`;
    return { host: ip, port: '', name, note };
  }

  function jump(c: Conn) {
    const found = c.session ? tabs.bySession(c.session) : undefined;
    if (!found) return;
    tabs.activate(found.tab.id);
    tabs.focusPane(found.tab.id, found.pane.id);
  }

  function copy(text: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${text}`));
  }

  function endProcess(c: Conn) {
    const { port } = splitAddress(c.local);
    overlay.confirm({
      title: `End ${c.process}?`,
      message: `Process ${c.pid} will be asked to quit${c.state === 'LISTEN' ? `, freeing port ${port}` : ''}.`,
      items: [`${c.protocol} ${c.local}${c.remote ? ` → ${c.remote}` : ''}`],
      confirmLabel: 'End Process',
      onConfirm: () =>
        network
          .kill(c.pid)
          .then(() => {
            toast.show(`Asked ${c.process} to quit`);
            setTimeout(() => network.refresh(), 500);
          })
          .catch((e) => toast.show(`Couldn't end ${c.process}: ${e}`)),
    });
  }

  function menu(e: MouseEvent, c: Conn) {
    e.preventDefault();
    const { port } = splitAddress(c.local);
    const entries: MenuEntry[] = [];
    if (c.state === 'LISTEN' && c.protocol === 'TCP') {
      entries.push({ label: `Open localhost:${port} in Browser`, action: () => Browser.OpenURL(`http://localhost:${port}`) });
    }
    if (owner(c)?.tabId != null) entries.push({ label: 'Go to Tab', action: () => jump(c) });
    if (entries.length) entries.push('separator');
    entries.push({ label: 'Copy Local Address', action: () => copy(c.local) });
    if (c.remote) entries.push({ label: 'Copy Remote Address', action: () => copy(c.remote) });
    if (c.targetIP) entries.push({ label: `Copy ${c.targetIP}`, action: () => copy(c.targetIP) });
    if (c.target && c.target !== c.targetIP) entries.push({ label: `Copy ${c.target}`, action: () => copy(c.target) });
    entries.push({ label: `Copy PID (${c.pid})`, action: () => copy(String(c.pid)) });
    entries.push('separator', { label: `End ${c.process}…`, danger: true, action: () => endProcess(c) });
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  const key = (c: Conn) => `${c.pid}|${c.protocol}|${c.local}|${c.remote}|${c.state}`;
</script>

{#snippet ownerChip(c: Conn)}
  {@const o = owner(c)}
  {#if o}
    <button
      class="owner"
      title={o.tabId != null ? `Go to “${o.label}”` : 'Started from a terminal in another window'}
      disabled={o.tabId == null}
      onclick={(e) => {
        e.stopPropagation();
        jump(c);
      }}>{o.label}</button
    >
  {/if}
{/snippet}

<div class="net">
  <header>
    <div class="scope" role="radiogroup" aria-label="Show connections from">
      <button role="radio" aria-checked={network.scope === 'terminals'} class:on={network.scope === 'terminals'} onclick={() => (network.scope = 'terminals')}>Terminals</button>
      <button role="radio" aria-checked={network.scope === 'all'} class:on={network.scope === 'all'} onclick={() => (network.scope = 'all')}>All</button>
    </div>
    <RefreshControls onrefresh={() => network.refresh()} />
  </header>
  <PausedNote updatedAt={network.updatedAt} />

  <div class="filter">
    <Icon name="search" size={12} stroke={2} />
    <input bind:value={network.filter} placeholder="Filter by port, host, process, PID" spellcheck="false" autocomplete="off" />
  </div>

  <div class="list">
    {#if network.error}
      <div class="note"><strong>Can't list connections</strong>{network.error}</div>
    {:else if network.loaded && visible.length === 0}
      <div class="note">
        {#if network.filter}
          No connections match “{network.filter}”.
        {:else if network.scope === 'terminals'}
          <strong>Nothing from your terminals</strong>Programs you start here — dev servers, ssh, database clients — show up as they connect.
          <button class="link" onclick={() => (network.scope = 'all')}>Show all connections</button>
        {:else}
          No network connections.
        {/if}
      </div>
    {/if}

    <h3>
      <Icon name="tunnel" size={12} /> Forwards <span class="count">{forwards.length}</span>
      <button class="add" title="New port forward (ssh tunnel or kubectl port-forward)" aria-label="New port forward" onclick={() => overlay.show({ kind: 'tunnel', tunnel: null, host: null })}>
        <Icon name="plus" size={12} />
      </button>
    </h3>
    {#each forwards as t (t.id)}
      <ForwardRow {t} />
    {:else}
      {#if sshTunnels.loaded && !network.filter}
        <div class="hint-row">None yet. + adds an ssh tunnel or a kubectl port-forward that runs in the background.</div>
      {/if}
    {/each}

    {#if listening.length}
      <h3><Icon name="radio" size={12} /> Listening <span class="count">{listening.length}</span></h3>
      {#each listening as c (key(c))}
        {@const a = splitAddress(c.local)}
        {@const exposed = isExposed(a.host)}
        <div class="row" role="listitem" title="{c.process} (pid {c.pid}) listening on {c.local}" oncontextmenu={(e) => menu(e, c)} ondblclick={() => jump(c)}>
          <span class="port">:{a.port}</span>
          <span class="main">
            <span class="proc">{c.process}</span>
            <span class="sub">
              {c.protocol}{serviceName(a.port) ? ` · ${serviceName(a.port)}` : ''}
              <span class="scope-pill" class:exposed title={exposed ? 'Reachable from other machines on your network' : 'Only reachable from this computer'}>{exposed ? 'network' : 'local'}</span>
              {#if forwardOn(a.port)}
                {@const f = forwardOn(a.port)!}
                <span class="forward-pill" title={describeTunnel(f)}>forward · {f.name || describeTunnel(f)}</span>
              {/if}
            </span>
          </span>
          {@render ownerChip(c)}
        </div>
      {/each}
    {/if}

    {#if connected.length}
      <h3><Icon name="arrowUpRight" size={12} /> Connections <span class="count">{connected.length}</span></h3>
      {#each connected as c (key(c))}
        {@const r = destination(c)}
        <div
          class="row"
          role="listitem"
          title="{c.process} (pid {c.pid}) → {r.host}{r.port ? `:${r.port}` : ''}{r.note ? `\n${r.note}` : ''}"
          oncontextmenu={(e) => menu(e, c)}
          ondblclick={() => jump(c)}
        >
          <span class="main">
            <span class="dest">{r.host}{#if r.port}<span class="dim">:{r.port}</span>{/if}{#if c.targetResolved}<span class="approx" aria-label="resolved address">≈</span>{/if}</span>
            <span class="sub">
              {#if r.name}<span class="target-name">{r.name}</span> ·{/if}
              {c.process} · {c.protocol}{serviceName(r.port) ? ` · ${serviceName(r.port)}` : ''}
              {#if c.state && c.state !== 'ESTABLISHED'}<span class="state">{c.state.toLowerCase().replace(/_/g, ' ')}</span>{/if}
            </span>
          </span>
          {@render ownerChip(c)}
        </div>
      {/each}
    {/if}

    {#if other.length}
      <button class="h3 toggle" onclick={() => (showOther = !showOther)} aria-expanded={showOther}>
        <span class="chev" class:open={showOther}><Icon name="chevron" size={11} stroke={2.2} /></span>
        Other sockets <span class="count">{other.length}</span>
      </button>
      {#if showOther}
        {#each other as c (key(c))}
          <div class="row" role="listitem" oncontextmenu={(e) => menu(e, c)}>
            <span class="main">
              <span class="proc">{c.process}</span>
              <span class="sub">{c.protocol} · {c.local}</span>
            </span>
            {@render ownerChip(c)}
          </div>
        {/each}
      {/if}
    {/if}
  </div>
</div>

<style>
  .net {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  header {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 30px;
    padding: 0 6px 0 8px;
  }
  .scope {
    display: flex;
    flex: 1;
    padding: 2px;
    border-radius: 7px;
    background: var(--tab-hover);
  }
  .scope button {
    flex: 1;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--chrome-muted);
    font-size: 11.5px;
    font-weight: 600;
  }
  .scope button.on {
    background: var(--tab-active);
    color: var(--chrome-text);
    box-shadow: var(--tab-active-shadow);
  }

  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 26px;
    margin: 2px 8px 4px;
    padding: 0 8px;
    border-radius: 7px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
  }
  .filter input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--chrome-text);
    font: inherit;
    font-size: 12px;
    -webkit-user-select: text;
    user-select: text;
  }
  .filter input:focus {
    outline: none;
  }
  .filter input::placeholder {
    color: var(--chrome-muted);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 6px 12px;
  }

  h3,
  .h3 {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    margin: 10px 0 4px;
    padding: 0 6px;
    border: 0;
    background: none;
    color: var(--chrome-muted);
    font-size: 11px;
    font-weight: 650;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    text-align: left;
  }
  .count {
    padding: 0 5px;
    border-radius: 5px;
    background: var(--tab-hover);
    font-size: 10.5px;
    letter-spacing: 0;
  }
  .chev {
    display: grid;
    transition: transform 120ms ease;
  }
  .chev.open {
    transform: rotate(90deg);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 9px;
    min-height: 38px;
    padding: 4px 6px 4px 8px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }

  .port {
    flex: none;
    min-width: 50px;
    color: var(--accent);
    font: 600 13px/1 'JetBrains Mono Variable', monospace;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .proc,
  .dest {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 12.5px;
    font-weight: 550;
  }
  .dest {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 12px;
  }
  .dim {
    color: var(--chrome-muted);
  }
  .approx {
    margin-left: 4px;
    color: var(--chrome-muted);
    font-family: inherit;
    font-size: 11px;
  }
  .target-name {
    color: var(--chrome-text);
    font-weight: 550;
  }
  .sub {
    display: flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
    white-space: nowrap;
    color: var(--chrome-muted);
    font-size: 11px;
  }
  .scope-pill,
  .state {
    padding: 0 5px;
    border-radius: 4px;
    background: var(--tab-hover);
    font-size: 10px;
    font-weight: 600;
  }
  .scope-pill.exposed {
    background: color-mix(in srgb, var(--g-orange) 22%, transparent);
    color: var(--g-orange);
  }

  .owner {
    flex: none;
    max-width: 42%;
    height: 20px;
    padding: 0 7px;
    overflow: hidden;
    border: 0;
    border-radius: 6px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .owner:hover:not(:disabled) {
    background: var(--accent);
    color: var(--on-accent);
  }
  .owner:disabled {
    background: var(--tab-hover);
    color: var(--chrome-muted);
  }

  .note {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
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
  .link {
    margin-top: 4px;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font-size: 12px;
    font-weight: 600;
  }
  .add {
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    margin-left: auto;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .add:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .hint-row {
    padding: 2px 10px 6px;
    color: var(--chrome-muted);
    font-size: 11.5px;
    line-height: 1.4;
  }
  .forward-pill {
    margin-left: 4px;
    padding: 0 5px;
    border-radius: 4px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 10px;
    font-weight: 600;
  }
</style>
