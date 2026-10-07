<script lang="ts">
  import { settings } from '../lib/settings.svelte';

  /** Tells how old the data is while auto-refresh is off. */
  let { updatedAt }: { updatedAt: number | null } = $props();

  const time = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', second: '2-digit' });
</script>

{#if !settings.config.sidebarAutoRefresh}
  <div class="paused" role="status">
    {updatedAt ? `Paused · updated ${time(updatedAt)}` : 'Paused'}
  </div>
{/if}

<style>
  .paused {
    flex: none;
    margin: 0 10px 4px;
    color: var(--chrome-muted);
    font-size: 11px;
  }
</style>
