<script lang="ts">
  import { tick, untrack } from 'svelte';
  import Icon from './Icon.svelte';
  import PausedNote from './PausedNote.svelte';
  import RefreshControls from './RefreshControls.svelte';
  import { prettyPath } from '../lib/backend';
  import { cdTo, entryMenu, insertPath, pathForShell } from '../lib/fileActions';
  import { overlay } from '../lib/overlay.svelte';
  import { settings } from '../lib/settings.svelte';
  import { baseName, parentOf, REMOTE_PROCESSES } from '../lib/shell';
  import { fileTree, sidebar, type Entry } from '../lib/sidebar.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { activeTerminal } from '../lib/terminals';

  type Row = { kind: 'entry'; entry: Entry; depth: number } | { kind: 'more'; path: string; depth: number };

  let filter = $state('');
  let list = $state<HTMLDivElement>();

  const pane = $derived(tabs.activePane);
  const root = $derived(sidebar.pinned ?? pane?.cwd ?? '');
  const remote = $derived(!sidebar.pinned && !!pane && REMOTE_PROCESSES.has(pane.process));
  const rootFolder = $derived(root ? fileTree.folders.get(root) : undefined);

  // A new root lists right away.
  $effect(() => {
    if (root) fileTree.load(root);
  });

  // Commands change files (touch, git checkout…), so refresh after each
  // one — unless auto-refresh is off.
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    if (!pane?.lastCommand || !settings.config.sidebarAutoRefresh) return;
    untrack(() => {
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(() => root && fileTree.refresh(root), 250);
    });
  });

  /** Visible rows: expanded folders' children, narrowed by the filter. */
  const rows = $derived.by(() => {
    const out: Row[] = [];
    const q = filter.trim().toLowerCase();
    const walk = (path: string, depth: number): boolean => {
      const folder = fileTree.folders.get(path);
      if (!folder?.entries) return false;
      let any = false;
      for (const entry of folder.entries) {
        const at = out.length;
        out.push({ kind: 'entry', entry, depth });
        const childMatched = entry.dir && fileTree.expanded.has(entry.path) && walk(entry.path, depth + 1);
        // Keep a row that matches, or that leads to a match.
        if (q && !entry.name.toLowerCase().includes(q) && !childMatched) out.length = at;
        else any = true;
      }
      if (folder.truncated && !q) out.push({ kind: 'more', path, depth });
      return any;
    };
    if (root) walk(root, 0);
    return out;
  });

  const entryRows = $derived(rows.filter((r): r is Extract<Row, { kind: 'entry' }> => r.kind === 'entry'));

  function activate(entry: Entry, clicks: number) {
    fileTree.selected = entry.path;
    if (entry.dir) {
      // The first click of a double-click already toggled the folder; the
      // second puts it back and cds instead.
      fileTree.toggle(entry.path);
      if (clicks === 2) cdTo(entry.path);
    } else if (clicks === 1) {
      insertPath(entry.path);
    }
  }

  function onContextMenu(e: MouseEvent, entry: Entry) {
    e.preventDefault();
    fileTree.selected = entry.path;
    overlay.openMenu(e.clientX, e.clientY, entryMenu(entry));
  }

  function onDragStart(e: DragEvent, entry: Entry) {
    if (!e.dataTransfer) return;
    e.dataTransfer.setData('application/x-wtf-path', entry.path);
    e.dataTransfer.setData('text/plain', pathForShell(entry.path));
    e.dataTransfer.effectAllowed = 'copy';
  }

  async function select(path: string) {
    fileTree.selected = path;
    await tick();
    list?.querySelector('.row.selected')?.scrollIntoView({ block: 'nearest' });
  }

  function onKeydown(e: KeyboardEvent) {
    const i = entryRows.findIndex((r) => r.entry.path === fileTree.selected);
    const current = entryRows[i]?.entry;
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        if (entryRows.length) select(entryRows[Math.min(i + 1, entryRows.length - 1)].entry.path);
        break;
      case 'ArrowUp':
        e.preventDefault();
        if (entryRows.length) select(entryRows[Math.max(i - 1, 0)].entry.path);
        break;
      case 'ArrowRight':
        e.preventDefault();
        if (current?.dir && !fileTree.expanded.has(current.path)) fileTree.toggle(current.path);
        else if (current?.dir && entryRows[i + 1]) select(entryRows[i + 1].entry.path);
        break;
      case 'ArrowLeft':
        e.preventDefault();
        if (current?.dir && fileTree.expanded.has(current.path)) fileTree.toggle(current.path);
        else if (current && parentOf(current.path) !== root) select(parentOf(current.path));
        break;
      case 'Enter':
        e.preventDefault();
        if (current) activate(current, 1);
        break;
      case 'Escape':
        e.preventDefault();
        activeTerminal()?.focus();
        break;
    }
  }

  function togglePin() {
    sidebar.pinned = sidebar.pinned ? null : root || null;
  }
</script>

<div class="tree">
  <header>
    <span class="root" title={root}>{root ? baseName(prettyPath(root)) || '/' : ''}</span>
    <button
      class="tool"
      class:on={!!sidebar.pinned}
      title={sidebar.pinned ? 'Unpin: follow the current folder' : 'Pin this folder'}
      aria-pressed={!!sidebar.pinned}
      onclick={togglePin}><Icon name="pin" size={13} /></button
    >
    <RefreshControls onrefresh={() => root && fileTree.refresh(root)} />
    <button class="tool" title="Collapse all" onclick={() => fileTree.collapseAll()}><Icon name="collapse" size={13} /></button>
  </header>

  <PausedNote updatedAt={null} />

  <div class="filter">
    <Icon name="search" size={12} stroke={2} />
    <input bind:value={filter} placeholder="Filter" spellcheck="false" autocomplete="off" onkeydown={(e) => {
      if (e.key === 'ArrowDown') { e.preventDefault(); list?.focus(); onKeydown(e); }
      if (e.key === 'Escape') { filter = ''; activeTerminal()?.focus(); }
    }} />
  </div>

  {#if remote}
    <div class="note">
      <strong>Remote session</strong>
      This pane is connected to another machine with {pane?.process}; its files aren't shown here.
    </div>
  {:else if !root}
    <div class="note">Waiting for the shell…</div>
  {:else if rootFolder?.error}
    <div class="note"><strong>Can't show {prettyPath(root)}</strong>{rootFolder.error}</div>
  {:else}
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="rows" role="tree" tabindex="0" bind:this={list} onkeydown={onKeydown}>
      {#each rows as row (row.kind === 'entry' ? row.entry.path : row.path + '…')}
        {#if row.kind === 'more'}
          <div class="more" style:padding-left="{row.depth * 14 + 26}px">Some entries not shown</div>
        {:else}
          {@const e = row.entry}
          {@const open = e.dir && fileTree.expanded.has(e.path)}
          <div
            class="row"
            class:selected={fileTree.selected === e.path}
            class:dim={e.ignored || e.hidden}
            class:link={e.symlink}
            style:padding-left="{row.depth * 14 + 6}px"
            role="treeitem"
            aria-selected={fileTree.selected === e.path}
            aria-expanded={e.dir ? open : undefined}
            tabindex="-1"
            title={e.path}
            draggable="true"
            onclick={(ev) => activate(e, ev.detail)}
            oncontextmenu={(ev) => onContextMenu(ev, e)}
            ondragstart={(ev) => onDragStart(ev, e)}
            onkeydown={() => {}}
          >
            <span class="chevron" class:open class:hidden={!e.dir}><Icon name="chevron" size={11} stroke={2.2} /></span>
            <span class="icon" class:folder={e.dir}><Icon name={e.dir ? (open ? 'folderOpen' : 'folder') : 'file'} size={13} /></span>
            <span class="name">{e.name}</span>
          </div>
        {/if}
      {:else}
        {#if rootFolder?.entries}
          <div class="note">{filter ? 'No matches in open folders' : 'Empty folder'}</div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .tree {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  header {
    display: flex;
    align-items: center;
    gap: 2px;
    flex: none;
    height: 30px;
    padding: 0 6px 0 12px;
  }
  .root {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--chrome-text);
    font-size: 11.5px;
    font-weight: 650;
    letter-spacing: 0.02em;
    text-transform: uppercase;
  }
  .tool {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .tool:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tool.on {
    color: var(--accent);
  }

  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 26px;
    margin: 2px 8px 6px;
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

  .rows {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 6px 10px;
  }
  .rows:focus {
    outline: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 4px;
    height: 24px;
    padding-right: 8px;
    border-radius: 6px;
    color: var(--chrome-text);
    font-size: 12.5px;
    white-space: nowrap;
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .row.selected {
    background: var(--tab-hover-strong);
  }
  .rows:focus-within .row.selected {
    background: var(--accent);
    color: var(--on-accent);
  }
  .row.dim {
    opacity: 0.5;
  }
  .row.link .name {
    font-style: italic;
  }

  .chevron {
    display: grid;
    place-items: center;
    flex: none;
    width: 14px;
    opacity: 0.6;
    transition: transform 120ms ease;
  }
  .chevron.open {
    transform: rotate(90deg);
  }
  .chevron.hidden {
    visibility: hidden;
  }
  .icon {
    display: grid;
    place-items: center;
    flex: none;
    color: var(--chrome-muted);
  }
  .icon.folder {
    color: var(--accent);
  }
  .rows:focus-within .row.selected .icon {
    color: inherit;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .more {
    height: 22px;
    color: var(--chrome-muted);
    font-size: 11.5px;
    font-style: italic;
  }

  .note {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 14px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
</style>
