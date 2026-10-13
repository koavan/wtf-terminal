<script lang="ts">
  import { missingTool, showTools } from '../lib/tools';

  // An "Install…" link next to an error that says a program is missing.
  // Shows nothing for any other error.

  let { error }: { error: string | null | undefined } = $props();

  const tool = $derived(missingTool(error));
</script>

{#if tool}
  <button
    class="install"
    title="Show how to install it"
    onclick={(e) => (e.stopPropagation(), showTools(tool))}
    ondblclick={(e) => e.stopPropagation()}>Install…</button
  >
{/if}

<style>
  .install {
    display: inline;
    margin-left: 6px;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    font-weight: 600;
    white-space: nowrap;
    cursor: pointer;
  }
  .install:hover {
    text-decoration: underline;
  }
</style>
