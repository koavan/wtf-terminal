<script lang="ts">
  import { onMount } from 'svelte';
  import Popover from './Popover.svelte';
  import Icon from './Icon.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { GROUP_COLORS, tabs } from '../lib/tabs.svelte';

  let { groupId, anchor }: { groupId: number; anchor: DOMRect } = $props();

  const group = $derived(tabs.group(groupId));
  let input = $state<HTMLInputElement>();

  onMount(() => {
    input?.focus();
    input?.select();
  });

  // The group can disappear underneath us (e.g. its last shell exited).
  $effect(() => {
    if (!group) overlay.close();
  });

  function act(action: () => void) {
    overlay.close();
    action();
  }
</script>

{#if group}
  <Popover x={anchor.left} y={anchor.bottom + 6} label="Edit tab group">
    <div class="editor">
      <input
        bind:this={input}
        bind:value={group.name}
        placeholder="Name this group"
        spellcheck="false"
        autocomplete="off"
        onkeydown={(e) => {
          if (e.key === 'Enter') overlay.close();
        }}
      />

      <div class="swatches" role="radiogroup" aria-label="Group color">
        {#each GROUP_COLORS as color (color)}
          <button
            class="swatch"
            class:selected={group.color === color}
            style:--group-color="var(--g-{color})"
            role="radio"
            aria-checked={group.color === color}
            aria-label={color}
            title={color}
            onclick={() => (group.color = color)}
          ></button>
        {/each}
      </div>

      <div class="separator"></div>

      <button class="action" onclick={() => act(() => tabs.newTabInGroup(groupId))}>
        <Icon name="plus" size={14} /> New tab in group
      </button>
      <button class="action" onclick={() => act(() => tabs.ungroup(groupId))}>
        <Icon name="ungroup" size={14} /> Ungroup
      </button>
      <button class="action danger" onclick={() => act(() => tabs.closeGroup(groupId))}>
        <Icon name="x" size={14} /> Close group
      </button>
    </div>
  </Popover>
{/if}

<style>
  .editor {
    display: flex;
    flex-direction: column;
    width: 236px;
  }

  input {
    height: 30px;
    margin: 4px 4px 0;
    padding: 0 10px;
    border: 0;
    border-radius: 7px;
    background: var(--field);
    box-shadow: inset 0 0 0 1px var(--hairline-strong);
    color: var(--text);
    font: inherit;
    font-size: 13px;
    -webkit-user-select: text;
    user-select: text;
  }
  input:focus {
    outline: none;
    box-shadow: inset 0 0 0 1.5px var(--accent);
  }
  input::placeholder {
    color: var(--text-muted);
  }

  .swatches {
    display: flex;
    justify-content: space-between;
    padding: 10px 6px 6px;
  }
  .swatch {
    width: 18px;
    height: 18px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: var(--group-color);
    transition: transform 120ms ease, box-shadow 120ms ease;
  }
  .swatch:hover {
    transform: scale(1.12);
  }
  .swatch.selected {
    box-shadow: 0 0 0 2px var(--popover-solid), 0 0 0 3.5px var(--group-color);
  }

  .separator {
    height: 1px;
    margin: 4px 6px;
    background: var(--hairline-strong);
  }

  .action {
    display: flex;
    align-items: center;
    gap: 9px;
    height: 28px;
    padding: 0 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    font-size: 13px;
  }
  .action:hover {
    background: var(--accent);
    color: var(--on-accent);
  }
  .action.danger:hover {
    background: var(--danger);
  }
</style>
