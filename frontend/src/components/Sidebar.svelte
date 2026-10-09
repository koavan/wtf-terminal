<script lang="ts">
  import FileTree from './FileTree.svelte';
  import HistoryView from './HistoryView.svelte';
  import Icon, { type IconName } from './Icon.svelte';
  import NetworkView from './NetworkView.svelte';
  import ProcessView from './ProcessView.svelte';
  import AwsView from './AwsView.svelte';
  import KubeView from './KubeView.svelte';
  import SshView from './SshView.svelte';
  import { isMac } from '../lib/platform';
  import { openManual } from '../lib/help';
  import { sidebar, type SidebarTab } from '../lib/sidebar.svelte';

  const TABS: { id: SidebarTab; label: string; icon: IconName; key: string }[] = [
    { id: 'files', label: 'Files', icon: 'folder', key: isMac ? '⌘⇧E' : 'Ctrl+Shift+Alt+E' },
    { id: 'ssh', label: 'SSH Hosts', icon: 'server', key: isMac ? '⌘⇧S' : 'Ctrl+Shift+Alt+S' },
    { id: 'aws', label: 'AWS Profiles', icon: 'cloud', key: isMac ? '⌘⇧U' : 'Ctrl+Shift+Alt+U' },
    { id: 'kube', label: 'Kubernetes', icon: 'wheel', key: isMac ? '⌘⇧K' : 'Ctrl+Shift+Alt+K' },
    { id: 'network', label: 'Network', icon: 'network', key: isMac ? '⌘⇧O' : 'Ctrl+Shift+Alt+O' },
    { id: 'processes', label: 'Processes', icon: 'cpu', key: isMac ? '⌘⇧A' : 'Ctrl+Shift+Alt+A' },
    { id: 'history', label: 'History', icon: 'history', key: isMac ? '⌘⇧H' : 'Ctrl+Shift+Alt+H' },
  ];

  let resizing = $state(false);
  let startX = 0;
  let startWidth = 0;

  function startResize(e: PointerEvent) {
    if (e.button !== 0) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    resizing = true;
    startX = e.clientX;
    startWidth = sidebar.width;
  }

  function onResize(e: PointerEvent) {
    if (resizing) sidebar.setWidth(startWidth + e.clientX - startX);
  }
</script>

<aside class="sidebar" style:width="{sidebar.width}px" aria-label="Side pane">
  <div class="tabs" role="tablist" aria-label="Side pane">
    {#each TABS as t (t.id)}
      <!-- Icons only, like VS Code; the name is in the tooltip and label. -->
      <button
        role="tab"
        class:on={sidebar.tab === t.id}
        aria-selected={sidebar.tab === t.id}
        aria-label={t.label}
        title="{t.label} ({t.key})"
        onclick={() => (sidebar.tab = t.id)}
      >
        <Icon name={t.icon} size={16} stroke={1.8} />
      </button>
    {/each}
    <span class="spacer"></span>
    <!-- Help for the tab on screen. -->
    <button class="help" aria-label="Help" title="Help for this tab ({isMac ? '⌘?' : 'F1'})" onclick={() => openManual()}>?</button>
  </div>

  <div class="content" role="tabpanel">
    {#if sidebar.tab === 'files'}
      <FileTree />
    {:else if sidebar.tab === 'ssh'}
      <SshView />
    {:else if sidebar.tab === 'aws'}
      <AwsView />
    {:else if sidebar.tab === 'kube'}
      <KubeView />
    {:else if sidebar.tab === 'network'}
      <NetworkView />
    {:else if sidebar.tab === 'processes'}
      <ProcessView />
    {:else if sidebar.tab === 'history'}
      <HistoryView />
    {/if}
  </div>

  <div
    class="resize"
    class:active={resizing}
    role="separator"
    aria-orientation="vertical"
    aria-label="Resize side pane"
    onpointerdown={startResize}
    onpointermove={onResize}
    onpointerup={() => (resizing = false)}
    onpointercancel={() => (resizing = false)}
    ondblclick={() => sidebar.setWidth(260)}
  ></div>
</aside>

<style>
  .sidebar {
    --wails-draggable: no-drag;
    position: relative;
    display: flex;
    flex-direction: column;
    flex: none;
    min-height: 0;
    margin: 0 0 8px 8px;
  }
  .tabs {
    display: flex;
    gap: 2px;
    flex: none;
    padding: 0 8px 6px;
  }
  .tabs button {
    position: relative;
    display: grid;
    place-items: center;
    width: 32px;
    height: 28px;
    padding: 0;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .tabs button:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tabs button.on {
    color: var(--accent);
  }
  /* The active tab's marker: a short bar under its icon. */
  .tabs button.on::after {
    content: '';
    position: absolute;
    left: 9px;
    right: 9px;
    bottom: -3px;
    height: 2px;
    border-radius: 1px;
    background: var(--accent);
  }

  .spacer {
    flex: 1;
  }
  .tabs button.help {
    width: 24px;
    font-size: 13px;
    font-weight: 700;
  }
  .content {
    flex: 1;
    min-height: 0;
  }

  /* The grab area sits in the gap between the side pane and the card. */
  .resize {
    position: absolute;
    top: 0;
    bottom: 0;
    right: -8px;
    width: 8px;
    cursor: col-resize;
  }
  .resize::after {
    content: '';
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 3px;
    width: 2px;
    border-radius: 1px;
    background: transparent;
    transition: background-color 120ms ease;
  }
  .resize:hover::after,
  .resize.active::after {
    background: var(--accent);
  }
</style>
