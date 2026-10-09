<script lang="ts">
  import ForwardRow from './ForwardRow.svelte';
  import Icon from './Icon.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { sshTunnels } from '../lib/sshAdmin.svelte';

  // Saved ssh tunnels. The backend runs them and reports every change
  // (see internal/sshhosts/tunnels.go), so nothing polls here. kubectl
  // port-forwards are listed in the Network tab's Forwards.
</script>

<div class="tunnels">
  <header>
    <span class="title">Port forwards</span>
    <button class="tool" title="New tunnel" aria-label="New tunnel" onclick={() => overlay.show({ kind: 'tunnel', tunnel: null, host: null })}>
      <Icon name="plus" size={14} />
    </button>
  </header>

  <div class="list" role="list">
    {#each sshTunnels.list.filter((x) => x.kind !== 'kubectl') as t (t.id)}
      <ForwardRow {t} />
    {:else}
      {#if sshTunnels.loaded}
        <div class="note">
          <strong>No tunnels</strong>
          Forward a port through ssh: reach a database behind a bastion on <code>localhost</code>, expose a local dev
          server to a server, or browse through a SOCKS proxy. Press + to add one, or right-click a host.
        </div>
      {/if}
    {/each}
  </div>
</div>

<style>
  .tunnels {
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
    font-size: 11.5px;
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
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
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
  code {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11px;
  }
</style>
