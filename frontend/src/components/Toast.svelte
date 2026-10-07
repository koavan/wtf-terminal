<script lang="ts">
  import { cubicOut } from 'svelte/easing';
  import { toast } from '../lib/toast.svelte';

  function rise(_node: HTMLElement) {
    return {
      duration: 160,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: translate(-50%, ${(1 - t) * 8}px);`,
    };
  }
</script>

{#if toast.message}
  {#key toast.message}
    <div class="toast" role="status" aria-live="polite" in:rise out:rise>{toast.message}</div>
  {/key}
{/if}

<style>
  .toast {
    position: fixed;
    bottom: 24px;
    left: 50%;
    z-index: 120;
    transform: translateX(-50%);
    padding: 7px 14px;
    border-radius: 999px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(24px) saturate(180%);
    backdrop-filter: blur(24px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
    font-size: 12.5px;
    font-weight: 500;
    pointer-events: none;
    white-space: nowrap;
  }
</style>
