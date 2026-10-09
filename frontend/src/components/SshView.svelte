<script lang="ts">
  import SshHosts from './SshHosts.svelte';
  import SshKeys from './SshKeys.svelte';
  import SshTunnels from './SshTunnels.svelte';
  import { sidebar, type SshSection } from '../lib/sidebar.svelte';
  import { sshTunnels } from '../lib/sshAdmin.svelte';

  // The SSH tab: hosts, keys and tunnels.

  const SECTIONS: { id: SshSection; label: string }[] = [
    { id: 'hosts', label: 'Hosts' },
    { id: 'keys', label: 'Keys' },
    { id: 'tunnels', label: 'Tunnels' },
  ];

  // Tunnels run in the background, so their count shows from any section.
  $effect(() => {
    if (!sshTunnels.loaded) sshTunnels.load().catch(() => {});
  });
</script>

<div class="ssh-view">
  <div class="sections" role="tablist" aria-label="SSH">
    {#each SECTIONS as s (s.id)}
      <button
        role="tab"
        aria-selected={sidebar.sshSection === s.id}
        class:on={sidebar.sshSection === s.id}
        onclick={() => (sidebar.sshSection = s.id)}
      >
        {s.label}
        {#if s.id === 'tunnels' && sshTunnels.running('ssh')}<span class="badge" title="{sshTunnels.running('ssh')} running">{sshTunnels.running('ssh')}</span>{/if}
      </button>
    {/each}
  </div>

  <div class="body">
    {#if sidebar.sshSection === 'keys'}
      <SshKeys />
    {:else if sidebar.sshSection === 'tunnels'}
      <SshTunnels />
    {:else}
      <SshHosts />
    {/if}
  </div>
</div>

<style>
  .ssh-view {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .sections {
    display: flex;
    gap: 2px;
    flex: none;
    margin: 0 8px 6px;
    padding: 2px;
    border-radius: 8px;
    background: var(--tab-hover);
  }
  .sections button {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    flex: 1;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
    font-size: 11.5px;
    font-weight: 550;
  }
  .sections button:hover:not(.on) {
    color: var(--chrome-text);
  }
  .sections button.on {
    background: var(--surface);
    color: var(--chrome-text);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.12);
  }
  .badge {
    min-width: 14px;
    padding: 0 4px;
    border-radius: 7px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 10px;
    font-weight: 700;
    line-height: 14px;
  }
  .body {
    flex: 1;
    min-height: 0;
  }
</style>
