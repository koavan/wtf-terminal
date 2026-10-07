<script lang="ts">
  import Popover from './Popover.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';

  let { x, y, entries }: { x: number; y: number; entries: MenuEntry[] } = $props();

  function run(action: () => void) {
    overlay.close();
    action();
  }
</script>

<Popover {x} {y} label="Menu">
  <div class="menu" role="menu">
    {#each entries as entry, i (i)}
      {#if entry === 'separator'}
        <div class="separator" role="separator"></div>
      {:else if 'heading' in entry}
        <div class="heading">{entry.heading}</div>
      {:else}
        <button class="item" class:danger={entry.danger} role="menuitem" onclick={() => run(entry.action)}>
          {#if entry.color}
            <span class="dot" style:--group-color="var(--g-{entry.color})"></span>
          {/if}
          <span class="label">{entry.label}</span>
          {#if entry.hint}<span class="hint">{entry.hint}</span>{/if}
        </button>
      {/if}
    {/each}
  </div>
</Popover>

<style>
  .menu {
    display: flex;
    flex-direction: column;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 9px;
    height: 28px;
    padding: 0 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    text-align: left;
    font-size: 13px;
  }
  .item:hover,
  .item:focus-visible {
    outline: none;
    background: var(--accent);
    color: var(--on-accent);
  }
  .item.danger:hover {
    background: var(--danger);
  }

  .label {
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .hint {
    opacity: 0.55;
    font-size: 12px;
  }

  .dot {
    flex: none;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--group-color);
  }

  .heading {
    padding: 6px 10px 3px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
  }

  .separator {
    height: 1px;
    margin: 4px 6px;
    background: var(--hairline-strong);
  }
</style>
