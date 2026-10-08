<script lang="ts">
  import { untrack } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import PausedNote from './PausedNote.svelte';
  import RefreshControls from './RefreshControls.svelte';
  import { prettyPath } from '../lib/backend';
  import { closeTab } from '../lib/closing';
  import { network } from '../lib/network.svelte';
  import { poll, sidebarInterval } from '../lib/poller';
  import { settings } from '../lib/settings.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { formatBytes, formatElapsed, processes, processTree, sortProcs, type Proc, type ProcessSort } from '../lib/processes.svelte';
  import { sidebar } from '../lib/sidebar.svelte';
  import { tabs, tabTitle } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  /** Rows shown in "All"; the filter narrows the rest. */
  const ALL_LIMIT = 300;

  let filter = $state('');

  // Polls while shown; with auto-refresh off, loads once (see lib/poller.ts).
  $effect(() => {
    const ms = sidebarInterval(settings.config);
    return untrack(() => poll(() => processes.refresh(), ms));
  });

  const q = $derived(filter.trim().toLowerCase());
  const matches = (p: Proc) => !q || `${p.name} ${p.command} ${p.pid}`.toLowerCase().includes(q);

  interface Group {
    session: string;
    label: string;
    tabId: number | null;
    order: number;
    rows: { proc: Proc; depth: number }[];
  }

  /** Terminals view: one tree per tab, in tab-strip order. */
  const groups = $derived.by((): Group[] => {
    const bySession = new Map<string, Proc[]>();
    for (const p of processes.procs) {
      if (!p.session) continue;
      const list = bySession.get(p.session) ?? [];
      list.push(p);
      bySession.set(p.session, list);
    }
    const out: Group[] = [];
    for (const [session, list] of bySession) {
      const found = tabs.bySession(session);
      const rows = q ? sortProcs(list.filter(matches), 'name').map((proc) => ({ proc, depth: 0 })) : processTree(list);
      if (!rows.length) continue;
      out.push({
        session,
        label: found ? tabTitle(found.tab, prettyPath) : 'Another window',
        tabId: found?.tab.id ?? null,
        order: found ? tabs.list.indexOf(found.tab) : Number.MAX_SAFE_INTEGER,
        rows,
      });
    }
    return out.sort((a, b) => a.order - b.order);
  });

  const allRows = $derived(sortProcs(processes.procs.filter(matches), processes.sort));

  function jump(p: Proc) {
    const found = p.session ? tabs.bySession(p.session) : undefined;
    if (!found) return;
    tabs.activate(found.tab.id);
    tabs.focusPane(found.tab.id, found.pane.id);
  }

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function end(p: Proc, force: boolean) {
    overlay.confirm({
      title: force ? `Force quit ${p.name}?` : `End ${p.name}?`,
      message: force
        ? `Process ${p.pid} will stop immediately, without a chance to save or clean up.`
        : `Process ${p.pid} will be asked to quit.`,
      items: [p.command],
      confirmLabel: force ? 'Force Quit' : 'End Process',
      onConfirm: () =>
        processes
          .kill(p.pid, force)
          .then(() => {
            toast.show(force ? `Force quit ${p.name}` : `Asked ${p.name} to quit`);
            setTimeout(() => processes.refresh(), 400);
          })
          .catch((e) => toast.show(`Couldn't end ${p.name}: ${e}`)),
    });
  }

  function showConnections(p: Proc) {
    network.filter = `pid:${p.pid}`;
    sidebar.tab = 'network';
  }

  function menu(e: MouseEvent, p: Proc) {
    e.preventDefault();
    const found = p.session ? tabs.bySession(p.session) : undefined;
    const entries: MenuEntry[] = [];
    if (found) entries.push({ label: 'Go to Tab', action: () => jump(p) });
    entries.push({ label: 'Show Network Connections', action: () => showConnections(p) });
    entries.push(
      'separator',
      { label: `Copy PID (${p.pid})`, action: () => copy(String(p.pid), 'PID') },
      { label: 'Copy Command', action: () => copy(p.command, 'command') },
      'separator',
    );
    if (p.shell && found) {
      // Interactive shells ignore SIGTERM; closing the tab is what ends one.
      entries.push({ label: 'Close Tab…', danger: true, action: () => closeTab(found.tab.id) });
    } else {
      entries.push(
        { label: `End ${p.name}…`, danger: true, action: () => end(p, false) },
        { label: `Force Quit ${p.name}…`, danger: true, action: () => end(p, true) },
      );
    }
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  const cpuClass = (cpu: number) => (cpu >= 50 ? 'hot' : cpu >= 10 ? 'warm' : '');
</script>

{#snippet row(p: Proc, depth: number)}
  <div
    class="row"
    class:shell={p.shell}
    role="listitem"
    title="{p.command}&#10;PID {p.pid} · parent {p.ppid} · running {formatElapsed(p.elapsed)}"
    style:padding-left="{8 + depth * 14}px"
    oncontextmenu={(e) => menu(e, p)}
    ondblclick={() => jump(p)}
  >
    {#if depth > 0}<span class="branch" aria-hidden="true"></span>{/if}
    <span class="name">{p.name}</span>
    <span class="pid">{p.pid}</span>
    <span class="cpu {cpuClass(p.cpu)}">{p.cpu.toFixed(p.cpu < 10 ? 1 : 0)}%</span>
    <span class="mem">{formatBytes(p.memory)}</span>
  </div>
{/snippet}

<div class="procs">
  <header>
    <div class="scope" role="radiogroup" aria-label="Show processes from">
      <button role="radio" aria-checked={processes.scope === 'terminals'} class:on={processes.scope === 'terminals'} onclick={() => (processes.scope = 'terminals')}>Terminals</button>
      <button role="radio" aria-checked={processes.scope === 'all'} class:on={processes.scope === 'all'} onclick={() => (processes.scope = 'all')}>All</button>
    </div>
    <RefreshControls onrefresh={() => processes.refresh()} />
  </header>
  <PausedNote updatedAt={processes.updatedAt} />

  <div class="filter">
    <Icon name="search" size={12} stroke={2} />
    <input bind:value={filter} placeholder="Filter by name, command, PID" spellcheck="false" autocomplete="off" />
  </div>

  {#if processes.scope === 'all'}
    <div class="columns">
      <span class="name">Process</span>
      {#each [['cpu', 'CPU'], ['memory', 'Memory']] as [key, label] (key)}
        <button class:on={processes.sort === key} onclick={() => (processes.sort = key as ProcessSort)}>{label}</button>
      {/each}
      <button class:on={processes.sort === 'name'} onclick={() => (processes.sort = 'name')}>A–Z</button>
    </div>
  {/if}

  <div class="list" role="list">
    {#if processes.error}
      <div class="note"><strong>Can't list processes</strong>{processes.error}</div>
    {:else if processes.scope === 'terminals'}
      {#each groups as g (g.session)}
        <button
          class="group"
          disabled={g.tabId === null}
          title={g.tabId === null ? 'A terminal in another window' : `Go to “${g.label}”`}
          onclick={() => g.tabId !== null && tabs.activate(g.tabId)}
        >
          <Icon name="terminal" size={12} />
          <span>{g.label}</span>
        </button>
        {#each g.rows as r (r.proc.pid)}
          {@render row(r.proc, r.depth)}
        {/each}
      {:else}
        {#if processes.loaded}
          <div class="note">{q ? `No processes match “${filter}”.` : 'No terminal processes yet.'}</div>
        {/if}
      {/each}
    {:else}
      {#each allRows.slice(0, ALL_LIMIT) as p (p.pid)}
        {@render row(p, 0)}
      {:else}
        {#if processes.loaded}<div class="note">No processes match “{filter}”.</div>{/if}
      {/each}
      {#if allRows.length > ALL_LIMIT}
        <div class="note">{allRows.length - ALL_LIMIT} more — filter to find them.</div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .procs {
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

  .columns {
    display: flex;
    align-items: center;
    gap: 2px;
    flex: none;
    margin: 2px 6px 2px;
    padding: 0 6px 0 8px;
    color: var(--chrome-muted);
    font-size: 10.5px;
    font-weight: 650;
    letter-spacing: 0.03em;
    text-transform: uppercase;
  }
  .columns .name {
    flex: 1;
  }
  .columns button {
    height: 20px;
    padding: 0 6px;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
  }
  .columns button.on {
    background: var(--tab-hover-strong);
    color: var(--chrome-text);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 6px 12px;
  }

  .group {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    margin: 10px 0 2px;
    padding: 2px 6px;
    border: 0;
    border-radius: 6px;
    background: none;
    color: var(--accent);
    font-size: 11.5px;
    font-weight: 650;
    text-align: left;
  }
  .group span {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .group:hover:not(:disabled) {
    background: var(--tab-hover);
  }
  .group:disabled {
    color: var(--chrome-muted);
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 26px;
    padding-right: 6px;
    border-radius: 6px;
    color: var(--chrome-text);
    font-size: 12.5px;
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .row.shell .name {
    color: var(--chrome-muted);
  }
  /* A small elbow marks a child process. */
  .branch {
    width: 7px;
    height: 7px;
    margin-left: -11px;
    margin-right: -4px;
    border-left: 1px solid var(--hairline-strong);
    border-bottom: 1px solid var(--hairline-strong);
    border-bottom-left-radius: 3px;
    transform: translateY(-3px);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .pid,
  .cpu,
  .mem {
    flex: none;
    color: var(--chrome-muted);
    font: 11px 'JetBrains Mono Variable', monospace;
    text-align: right;
  }
  .pid {
    width: 42px;
    opacity: 0.7;
  }
  .cpu {
    width: 40px;
  }
  .cpu.warm {
    color: var(--chrome-text);
  }
  .cpu.hot {
    color: var(--g-orange);
    font-weight: 700;
  }
  .mem {
    width: 50px;
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
</style>
