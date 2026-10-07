<script lang="ts">
  import { onMount } from 'svelte';
  import { Events, Window } from '@wailsio/runtime';
  import AddressBar from './components/AddressBar.svelte';
  import ContextMenu from './components/ContextMenu.svelte';
  import GroupEditor from './components/GroupEditor.svelte';
  import Palette from './components/Palette.svelte';
  import TabPanes from './components/TabPanes.svelte';
  import TabStrip from './components/TabStrip.svelte';
  import Toast from './components/Toast.svelte';
  import { installShortcuts, runCommand } from './lib/commands';
  import { overlay } from './lib/overlay.svelte';
  import { isMac } from './lib/platform';
  import { autosave, restoreSession } from './lib/session.svelte';
  import { settings } from './lib/settings.svelte';
  import { applyChrome } from './lib/themes';
  import { tabs } from './lib/tabs.svelte';

  let fullscreen = $state(false);

  onMount(() => {
    const cleanups = [
      Events.On('app:command', (e) => runCommand(e.data)),
      Events.On('terminal:exit', (e) => tabs.onSessionExit(e.data.id)),
      Events.On('terminal:info', (e) => tabs.applyInfo(e.data)),
      installShortcuts(),
    ];

    // Traffic lights disappear in fullscreen, so the strip reclaims their space.
    const onResize = () => Window.IsFullscreen().then((f) => (fullscreen = f));
    window.addEventListener('resize', onResize);

    let stopSaving: (() => void) | undefined;
    restoreSession().then((restored) => {
      if (!restored) tabs.open();
      stopSaving = autosave();
    });

    return () => {
      stopSaving?.();
      cleanups.forEach((off) => off());
      window.removeEventListener('resize', onResize);
    };
  });

  // The whole window follows the active theme.
  $effect(() => applyChrome(settings.theme));
  $effect(() => {
    document.documentElement.style.setProperty('--dim-unfocused', String(settings.config.unfocusedSplitDim));
  });

  // Clicking the chrome must not pull keyboard focus away from the terminal.
  const keepFocus = (e: MouseEvent) => e.preventDefault();
</script>

<div class="app" class:mac={isMac} class:fullscreen>
  <header class="strip" onmousedown={keepFocus} oncontextmenu={(e) => e.preventDefault()} role="presentation">
    <TabStrip />
  </header>

  <main class="card">
    <div onmousedown={keepFocus} role="presentation">
      <AddressBar />
    </div>
    <div class="terminals">
      {#each tabs.list as tab (tab.id)}
        <TabPanes {tab} visible={tab.id === tabs.activeId} />
      {/each}
    </div>
  </main>
</div>

<Toast />

{#if overlay.current?.kind === 'menu'}
  <ContextMenu x={overlay.current.x} y={overlay.current.y} entries={overlay.current.entries} />
{:else if overlay.current?.kind === 'palette'}
  {#key overlay.current.mode}
    <Palette mode={overlay.current.mode} />
  {/key}
{:else if overlay.current?.kind === 'group'}
  {#key overlay.current.groupId}
    <GroupEditor groupId={overlay.current.groupId} anchor={overlay.current.anchor} />
  {/key}
{/if}

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--chrome);
  }

  .strip {
    --wails-draggable: drag;
    display: flex;
    align-items: center;
    flex: none;
    height: var(--strip-height);
    padding: 0 10px;
  }
  /* Room for the traffic lights. */
  .mac:not(.fullscreen) .strip {
    padding-left: var(--traffic-lights-width);
  }

  .card {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    margin: 0 8px 8px;
    border-radius: var(--radius-card);
    background: var(--surface);
    box-shadow: var(--card-shadow);
    overflow: hidden;
  }

  .terminals {
    position: relative;
    flex: 1;
    min-height: 0;
  }
</style>
