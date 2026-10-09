<script lang="ts">
  import Icon from './Icon.svelte';
  import { broadcast } from '../lib/broadcast.svelte';
  import { isMac } from '../lib/platform';
  import { tabs } from '../lib/tabs.svelte';

  // Shown whenever input is being broadcast, so it's never on by surprise.

  // Closing or ungrouping tabs ends their broadcast.
  $effect(() => {
    void tabs.list.map((t) => t.groupId);
    if (broadcast.on) broadcast.prune();
  });
</script>

{#if broadcast.on}
  <div class="bar" role="status">
    <Icon name="radio" size={13} stroke={2} />
    <span class="text"><strong>Broadcasting input</strong> — typing goes to {broadcast.describe()}</span>
    <button onclick={() => broadcast.stopAll()} title="Stop broadcasting ({isMac ? '⌥⌘B' : 'Ctrl+Shift+Alt+B'})">
      Stop <span class="key">{isMac ? '⌥⌘B' : 'Ctrl+Shift+Alt+B'}</span>
    </button>
  </div>
{/if}

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: none;
    margin: 0 8px 4px;
    padding: 4px 6px 4px 10px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--g-orange) 22%, transparent);
    color: var(--text);
    font-size: 12px;
  }
  .bar :global(svg) {
    flex: none;
    color: var(--g-orange);
  }
  .text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  strong {
    font-weight: 650;
  }
  button {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 22px;
    padding: 0 8px;
    border: 0;
    border-radius: 6px;
    background: var(--g-orange);
    color: var(--group-ink);
    font-size: 11.5px;
    font-weight: 650;
  }
  .key {
    opacity: 0.7;
    font-weight: 500;
  }
</style>
