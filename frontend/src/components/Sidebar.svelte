<script lang="ts">
  import FileTree from './FileTree.svelte';
  import HistoryView from './HistoryView.svelte';
  import PinsView from './PinsView.svelte';
  import ShareView from './ShareView.svelte';
  import GenerateView from './GenerateView.svelte';
  import Icon, { type IconName } from './Icon.svelte';
  import NetworkView from './NetworkView.svelte';
  import ProcessView from './ProcessView.svelte';
  import AwsView from './AwsView.svelte';
  import KubeView from './KubeView.svelte';
  import SnippetsView from './SnippetsView.svelte';
  import SshView from './SshView.svelte';
  import { isMac } from '../lib/platform';
  import { openManual } from '../lib/help';
  import { DEFAULT_WIDTH, sidebar, type SidebarTab } from '../lib/sidebar.svelte';

  const TABS: { id: SidebarTab; label: string; icon: IconName; key: string }[] = [
    { id: 'files', label: 'Files', icon: 'folder', key: isMac ? '⌘⇧E' : 'Ctrl+Shift+Alt+E' },
    { id: 'ssh', label: 'SSH Hosts', icon: 'server', key: isMac ? '⌘⇧S' : 'Ctrl+Shift+Alt+S' },
    { id: 'aws', label: 'AWS Profiles', icon: 'cloud', key: isMac ? '⌘⇧U' : 'Ctrl+Shift+Alt+U' },
    { id: 'kube', label: 'Kubernetes', icon: 'wheel', key: isMac ? '⌘⇧K' : 'Ctrl+Shift+Alt+K' },
    { id: 'snippets', label: 'Snippets', icon: 'scroll', key: isMac ? '⌘⇧Y' : 'Ctrl+Shift+Alt+Y' },
    { id: 'workspaces', label: 'Workspaces', icon: 'layers', key: isMac ? '⌘⇧L' : 'Ctrl+Shift+Alt+L' },
    { id: 'network', label: 'Network', icon: 'network', key: isMac ? '⌘⇧O' : 'Ctrl+Shift+Alt+O' },
    { id: 'processes', label: 'Processes', icon: 'cpu', key: isMac ? '⌘⇧A' : 'Ctrl+Shift+Alt+A' },
    { id: 'history', label: 'History', icon: 'history', key: isMac ? '⌘⇧H' : 'Ctrl+Shift+Alt+H' },
    { id: 'pins', label: 'Pinned Output', icon: 'pin', key: isMac ? '⌘⇧I' : 'Ctrl+Shift+Alt+I' },
    { id: 'share', label: 'Share & Record', icon: 'shield', key: isMac ? '⌘⇧J' : 'Ctrl+Shift+Alt+J' },
    { id: 'generate', label: 'Generate Keys & Passwords', icon: 'key', key: isMac ? '⌘⇧N' : 'Ctrl+Shift+Alt+N' },
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
  <!-- A vertical rail of tab icons, like VS Code's activity bar; help at the bottom. -->
  <div class="tabs" role="tablist" aria-label="Side pane" aria-orientation="vertical">
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
    {:else if sidebar.tab === 'snippets'}
      <SnippetsView view="snippets" />
    {:else if sidebar.tab === 'workspaces'}
      <SnippetsView view="workspaces" />
    {:else if sidebar.tab === 'network'}
      <NetworkView />
    {:else if sidebar.tab === 'processes'}
      <ProcessView />
    {:else if sidebar.tab === 'history'}
      <HistoryView />
    {:else if sidebar.tab === 'pins'}
      <PinsView />
    {:else if sidebar.tab === 'share'}
      <ShareView />
    {:else if sidebar.tab === 'generate'}
      <GenerateView />
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
    ondblclick={() => sidebar.setWidth(DEFAULT_WIDTH)}
  ></div>
</aside>

<style>
  .sidebar {
    --wails-draggable: no-drag;
    position: relative;
    display: flex;
    flex: none;
    min-height: 0;
    margin: 0 0 8px 8px;
  }
  .tabs {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    flex: none;
    min-height: 0;
    padding: 0 6px 0 0;
    border-right: 1px solid var(--hairline);
    /* A short window scrolls the icons rather than cutting off the last ones. */
    overflow-y: auto;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar {
    display: none;
  }
  .tabs button {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
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
  .tabs button.on {
    background: var(--accent-soft);
  }
  /* The active tab's marker: a short bar beside its icon. */
  .tabs button.on::after {
    content: '';
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: -6px;
    width: 2px;
    border-radius: 1px;
    background: var(--accent);
  }

  .spacer {
    flex: 1;
  }
  .tabs button.help {
    height: 28px;
    margin-bottom: 2px;
    font-size: 13px;
    font-weight: 700;
  }
  .content {
    flex: 1;
    min-width: 0;
    min-height: 0;
    padding-left: 4px;
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
