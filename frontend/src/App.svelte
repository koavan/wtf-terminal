<script lang="ts">
  import { onMount } from 'svelte';
  import { Events, Window } from '@wailsio/runtime';
  import AddressBar from './components/AddressBar.svelte';
  import Icon from './components/Icon.svelte';
  import BroadcastBar from './components/BroadcastBar.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import Overlays from './components/Overlays.svelte';
  import TabPanes from './components/TabPanes.svelte';
  import TabStrip from './components/TabStrip.svelte';
  import Toast from './components/Toast.svelte';
  import { prettyPath } from './lib/backend';
  import { closeWindow } from './lib/closing';
  import { installShortcuts, runCommand } from './lib/commands';
  import { overlay } from './lib/overlay.svelte';
  import { showTools } from './lib/tools';
  import { Service as Tools } from '../bindings/github.com/dayanraj/wtf/internal/tools';
  import { isMac } from './lib/platform';
  import { autosave, restoreSession } from './lib/session.svelte';
  import { sidebar } from './lib/sidebar.svelte';
  import { settings } from './lib/settings.svelte';
  import { applyChrome } from './lib/themes';
  import { tabs, tabTitle } from './lib/tabs.svelte';
  import { closeThisWindow, thisWindow, Windows } from './lib/window';

  let fullscreen = $state(false);

  onMount(() => {
    const me = thisWindow.name;
    tabs.onEmpty = closeThisWindow;
    const cleanups = [
      // Events reach every window; each acts only on its own.
      Events.On('app:command', (e) => e.data.window === me && runCommand(e.data.name)),
      Events.On('window:closeRequested', (e) => e.data === me && closeWindow(closeThisWindow)),
      Events.On('window:adopt', (e) => e.data.window === me && tabs.adopt(JSON.parse(e.data.payload))),
      Events.On('app:quitRequested', (e) => {
        if (e.data.window !== me) return;
        const running = e.data.running ?? [];
        overlay.confirm({
          title: 'Quit wtf?',
          message:
            running.length === 1
              ? 'A program is still running. Quitting will end it.'
              : `${running.length} programs are still running. Quitting will end them.`,
          items: running.map((r) => `${r.process} — ${prettyPath(r.cwd)}`),
          confirmLabel: 'Quit',
          onConfirm: () => Windows.Quit(),
        });
      }),
      Events.On('terminal:exit', (e) => tabs.onSessionExit(e.data.id)),
      Events.On('terminal:info', (e) => tabs.applyInfo(e.data)),
      installShortcuts(),
    ];

    // Traffic lights disappear in fullscreen, so the strip reclaims their space.
    const onResize = () => Window.IsFullscreen().then((f) => (fullscreen = f));
    window.addEventListener('resize', onResize);

    restoreSession(me).then((restored) => {
      if (!restored) tabs.open();
      autosave(me);
      // Once ever: say which tools features use aren't installed.
      Tools.FirstRun('').then((missing) => {
        if (missing?.length && !overlay.current) showTools(undefined, missing);
      });
    });

    return () => {
      cleanups.forEach((off) => off());
      window.removeEventListener('resize', onResize);
    };
  });

  // The window title (Window menu, Mission Control) follows the active tab.
  $effect(() => {
    const tab = tabs.active;
    const title = tab ? tabTitle(tab, prettyPath) : 'wtf';
    Windows.SetTitle(thisWindow.name, title);
  });

  // The whole window follows the active theme.
  $effect(() => applyChrome(settings.theme));
  $effect(() => {
    document.documentElement.style.setProperty('--dim-unfocused', String(settings.config.unfocusedSplitDim));
  });

  // Clicking the chrome must not pull keyboard focus away from the terminal.
  // Text fields in the chrome (e.g. renaming a tab) still take clicks.
  const keepFocus = (e: MouseEvent) => {
    if (!(e.target as HTMLElement).closest('input, textarea')) e.preventDefault();
  };
</script>

<div class="app" class:mac={isMac} class:fullscreen>
  <header class="strip" onmousedown={keepFocus} oncontextmenu={(e) => e.preventDefault()} role="presentation">
    <button
      class="sidebar-toggle"
      class:on={sidebar.open}
      title="Toggle Sidebar ({isMac ? '⌘B' : 'Ctrl+Shift+B'})"
      aria-label="Toggle sidebar"
      aria-pressed={sidebar.open}
      onclick={() => sidebar.toggle()}
    >
      <Icon name="sidebar" size={15} />
    </button>
    <TabStrip />
  </header>

  <div class="body">
    {#if sidebar.open}
      <Sidebar />
    {/if}
    <main class="card">
      <div onmousedown={keepFocus} role="presentation">
        <AddressBar />
      </div>
      <BroadcastBar />
      <div class="terminals">
        {#each tabs.list as tab (tab.id)}
          <TabPanes {tab} visible={tab.id === tabs.activeId} />
        {/each}
      </div>
    </main>
  </div>
</div>

<Toast />

<Overlays />

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

  .sidebar-toggle {
    --wails-draggable: no-drag;
    display: grid;
    place-items: center;
    flex: none;
    width: 28px;
    height: 28px;
    margin-right: 6px;
    padding: 0;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .sidebar-toggle:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .sidebar-toggle.on {
    color: var(--chrome-text);
  }

  .body {
    display: flex;
    flex: 1;
    min-height: 0;
  }

  .card {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
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
