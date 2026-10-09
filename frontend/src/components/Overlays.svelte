<script lang="ts">
  import Confirm from './Confirm.svelte';
  import ContextMenu from './ContextMenu.svelte';
  import GroupEditor from './GroupEditor.svelte';
  import AwsProfileEditor from './AwsProfileEditor.svelte';
  import AwsRotate from './AwsRotate.svelte';
  import CommandPromptDialog from './CommandPromptDialog.svelte';
  import EksAddDialog from './EksAddDialog.svelte';
  import HostEditor from './HostEditor.svelte';
  import HostKeysDialog from './HostKeysDialog.svelte';
  import KeyGenerator from './KeyGenerator.svelte';
  import KeyInstall from './KeyInstall.svelte';
  import TransferDialog from './TransferDialog.svelte';
  import TunnelEditor from './TunnelEditor.svelte';
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
{:else if overlay.current?.kind === 'sshHost'}
  <HostEditor host={overlay.current.host} prefill={overlay.current.prefill} />
{:else if overlay.current?.kind === 'keyGen'}
  <KeyGenerator />
{:else if overlay.current?.kind === 'keyInstall'}
  <KeyInstall key={overlay.current.key} host={overlay.current.host} />
{:else if overlay.current?.kind === 'hostKeys'}
  <HostKeysDialog host={overlay.current.host} />
{:else if overlay.current?.kind === 'tunnel'}
  <TunnelEditor tunnel={overlay.current.tunnel} host={overlay.current.host} />
{:else if overlay.current?.kind === 'transfer'}
  <TransferDialog host={overlay.current.host} direction={overlay.current.direction} locals={overlay.current.locals} />
{:else if overlay.current?.kind === 'awsProfile'}
  <AwsProfileEditor profile={overlay.current.profile} />
{:else if overlay.current?.kind === 'awsRotate'}
  <AwsRotate profile={overlay.current.profile} />
{:else if overlay.current?.kind === 'commandPrompt'}
  <CommandPromptDialog request={overlay.current} />
{:else if overlay.current?.kind === 'eksAdd'}
  <EksAddDialog />
{:else if overlay.current?.kind === 'group'}
  {#key overlay.current.groupId}
    <GroupEditor groupId={overlay.current.groupId} anchor={overlay.current.anchor} />
  {/key}
{/if}
