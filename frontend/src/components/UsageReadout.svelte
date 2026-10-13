<script lang="ts">
  import Icon from './Icon.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { formatBytes, formatCPU, resourceUsage, wtfTotal } from '../lib/usage.svelte';

  // wtf's own CPU and memory in the address bar (show-usage). Sampling runs
  // only while this is on screen; click for the breakdown.

  $effect(() => resourceUsage.watch());

  const total = $derived(resourceUsage.current ? wtfTotal(resourceUsage.current) : null);
</script>

<button
  class="usage"
  title="wtf's CPU (% of one core) and memory: the app and its windows, not the programs you run in it. Click for the breakdown."
  aria-label="Resource usage"
  onclick={() => overlay.show({ kind: 'usage' })}
>
  <Icon name="activity" size={12} stroke={2} />
  {#if total}
    <span>{formatCPU(total.cpu)}</span><span class="sep">·</span><span>{formatBytes(total.memory)}</span>
  {:else}
    <span>…</span>
  {/if}
</button>

<style>
  .usage {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: none;
    height: 20px;
    padding: 0 8px 0 7px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 11.5px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .usage:hover {
    background: var(--tab-hover);
    color: var(--text);
  }
  .sep {
    opacity: 0.6;
  }
</style>
