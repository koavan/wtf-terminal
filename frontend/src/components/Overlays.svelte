<script lang="ts">
  import Confirm from './Confirm.svelte';
  import ContextMenu from './ContextMenu.svelte';
  import GroupEditor from './GroupEditor.svelte';
  import Palette from './Palette.svelte';
  import TabOverview from './TabOverview.svelte';
  import { overlay } from '../lib/overlay.svelte';
</script>

<!-- The one popover, menu or dialog open at a time (see lib/overlay.svelte.ts). -->
{#if overlay.current?.kind === 'menu'}
  <ContextMenu x={overlay.current.x} y={overlay.current.y} entries={overlay.current.entries} />
{:else if overlay.current?.kind === 'confirm'}
  <Confirm request={overlay.current} />
{:else if overlay.current?.kind === 'overview'}
  <TabOverview />
{:else if overlay.current?.kind === 'palette'}
  {#key overlay.current.mode}
    <Palette mode={overlay.current.mode} />
  {/key}
{:else if overlay.current?.kind === 'group'}
  {#key overlay.current.groupId}
    <GroupEditor groupId={overlay.current.groupId} anchor={overlay.current.anchor} />
  {/key}
{/if}
