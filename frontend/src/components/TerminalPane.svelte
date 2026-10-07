<script lang="ts">
  import { onMount } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { SearchAddon } from '@xterm/addon-search';
  import { SerializeAddon } from '@xterm/addon-serialize';
  import { WebglAddon } from '@xterm/addon-webgl';
  import { WebLinksAddon } from '@xterm/addon-web-links';
  import { Unicode11Addon } from '@xterm/addon-unicode11';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import SearchBar from './SearchBar.svelte';
  import { Service, endpoint } from '../lib/backend';
  import { notifyCommandDone } from '../lib/notify';
  import { isMac } from '../lib/platform';
  import { PromptTracker } from '../lib/prompts';
  import { search } from '../lib/search.svelte';
  import { settings } from '../lib/settings.svelte';
  import { detachedSessions, tabs, type Pane } from '../lib/tabs.svelte';
  import { registerTerminal } from '../lib/terminals';
  import { xtermTheme } from '../lib/themes';
  import { thisWindow } from '../lib/window';

  let {
    pane,
    tabId,
    visible,
    focused,
    dimmed,
  }: {
    pane: Pane;
    tabId: number;
    /** On screen: its tab is active and it isn't hidden by a zoomed sibling. */
    visible: boolean;
    /** Has keyboard focus within the active tab. */
    focused: boolean;
    /** Unfocused pane in a split. */
    dimmed: boolean;
  } = $props();

  /** Acknowledge rendered output every this many bytes (see stream.go). */
  const ACK_EVERY = 64 * 1024;

  let el: HTMLDivElement;
  let term = $state.raw<Terminal>();
  let fit: FitAddon;
  let searchAddon = $state.raw<SearchAddon>();
  let webgl: WebglAddon | null = null;
  let webglBroken = false;
  let ws: WebSocket | null = null;
  let sessionId: string | null = null;
  let disposed = false;

  const encoder = new TextEncoder();

  /** The configured font first, then the bundled one, then system monospace. */
  const fontStack = (family: string) =>
    `"${family.replace(/"/g, '')}", "JetBrains Mono Variable", Menlo, "DejaVu Sans Mono", monospace`;

  onMount(() => {
    const cfg = settings.config;
    const t = new Terminal({
      fontFamily: fontStack(cfg.fontFamily),
      fontSize: settings.fontSize,
      fontWeight: 400,
      fontWeightBold: 600,
      lineHeight: cfg.lineHeight,
      cursorBlink: cfg.cursorBlink,
      cursorStyle: cfg.cursorStyle as 'bar' | 'block' | 'underline',
      cursorWidth: 2,
      cursorInactiveStyle: 'outline',
      scrollback: cfg.scrollback,
      macOptionIsMeta: cfg.optionAsMeta,
      allowProposedApi: true, // unicode11, search decorations
      macOptionClickForcesSelection: true,
      drawBoldTextInBrightColors: false,
      theme: xtermTheme(settings.theme),
    });

    fit = new FitAddon();
    t.loadAddon(fit);
    const s = new SearchAddon({ highlightLimit: 2000 });
    t.loadAddon(s);
    const serializer = new SerializeAddon();
    t.loadAddon(serializer);
    t.loadAddon(new Unicode11Addon());
    t.unicode.activeVersion = '11';
    // ⌘-click (Ctrl-click elsewhere) opens links, like most terminals.
    t.loadAddon(
      new WebLinksAddon((e, uri) => {
        if (isMac ? e.metaKey : e.ctrlKey) Browser.OpenURL(uri);
      }),
    );

    t.open(el);

    // ⌘-combos belong to the app menu; don't let xterm swallow them.
    t.attachCustomKeyEventHandler((e) => !(isMac && e.metaKey));

    t.onData((data) => send(encoder.encode(data)));
    t.onBinary((data) => send(Uint8Array.from(data, (c) => c.charCodeAt(0))));
    t.onResize(({ cols, rows }) => {
      if (sessionId) Service.Resize(sessionId, cols, rows);
    });
    t.onTitleChange((title) => tabs.setOscTitle(pane.id, title));
    t.onSelectionChange(() => {
      if (settings.config.copyOnSelect && t.hasSelection()) Clipboard.SetText(t.getSelection());
    });
    // Clicking into a pane focuses it.
    t.textarea?.addEventListener('focus', () => tabs.focusPane(tabId, pane.id));

    const prompts = new PromptTracker(t, {
      onPrompt: () => tabs.markIntegrated(pane.id),
      onCommandStart: (text) => tabs.commandStarted(pane.id, text),
      onCommandEnd: (exitCode, durationMs) => {
        tabs.commandFinished(pane.id, exitCode, durationMs);
        notifyCommandDone(pane, exitCode, durationMs, visible);
      },
    });

    const unregister = registerTerminal(pane.id, {
      focus: () => t.focus(),
      clear: () => t.clear(),
      selectAll: () => t.selectAll(),
      getSelection: () => t.getSelection(),
      paste: (text) => t.paste(text),
      jumpPrompt: (direction) => prompts.jump(direction),
      lastOutput: () => prompts.lastOutput(),
      serialize: () => serializer.serialize({ scrollback: 5000 }),
      snapshot: (rows) => {
        const buf = t.buffer.active;
        // Bottom of the screen, skipping trailing blank rows.
        let end = buf.baseY + t.rows;
        while (end > 0 && !buf.getLine(end - 1)?.translateToString(true).trim()) end--;
        const lines: string[] = [];
        for (let y = Math.max(0, end - rows); y < end; y++) lines.push(buf.getLine(y)?.translateToString(true) ?? '');
        return lines;
      },
    });

    const resizeObserver = new ResizeObserver(scheduleFit);
    resizeObserver.observe(el);

    term = t;
    searchAddon = s;
    start(t);

    return () => {
      disposed = true;
      resizeObserver.disconnect();
      unregister();
      prompts.dispose();
      if (search.paneId === pane.id) search.close();
      ws?.close();
      // A tab moving to another window keeps its shells running.
      if (sessionId && !detachedSessions.delete(sessionId)) Service.Close(sessionId);
      webgl?.dispose();
      t.dispose();
    };
  });

  async function start(t: Terminal) {
    fit.fit();
    if (pane.sessionId) {
      // Moved here from another window: show its scrollback, take over its shell.
      const id = pane.sessionId;
      if (pane.restoreBuffer) {
        t.write(pane.restoreBuffer);
        pane.restoreBuffer = undefined;
      }
      sessionId = id;
      await Service.Adopt(id, thisWindow.name);
      await Service.Resize(id, t.cols, t.rows);
      connect(t, id);
      return;
    }
    const id = await Service.Create(thisWindow.name, t.cols, t.rows, pane.initialCwd);
    if (disposed) {
      Service.Close(id);
      return;
    }
    sessionId = id;
    tabs.setSession(pane.id, id);
    connect(t, id);
  }

  async function connect(t: Terminal, id: string, attempt = 0) {
    const { url, token } = await endpoint();
    if (disposed) return;
    const socket = new WebSocket(`${url}?id=${encodeURIComponent(id)}&token=${token}`);
    socket.binaryType = 'arraybuffer';
    ws = socket;

    // A session taken over from another window stays attached there until
    // that window lets go, so the first attempts may be refused.
    let opened = false;
    socket.onopen = () => (opened = true);
    socket.onclose = () => {
      if (!opened && !disposed && attempt < 40) setTimeout(() => connect(t, id, attempt + 1), 100);
    };

    let rendered = 0;
    let acked = 0;
    socket.onmessage = (ev) => {
      const data = new Uint8Array(ev.data as ArrayBuffer);
      t.write(data, () => {
        rendered += data.length;
        if (rendered - acked >= ACK_EVERY && socket.readyState === WebSocket.OPEN) {
          acked = rendered;
          socket.send('a' + rendered);
        }
      });
      if (!visible) tabs.markActivity(tabId);
    };
  }

  function send(bytes: Uint8Array) {
    if (ws?.readyState === WebSocket.OPEN) ws.send(bytes);
  }

  let fitQueued = false;
  function scheduleFit() {
    if (fitQueued) return;
    fitQueued = true;
    requestAnimationFrame(() => {
      fitQueued = false;
      if (!term || disposed) return;
      const dims = fit.proposeDimensions();
      if (dims && (dims.cols !== term.cols || dims.rows !== term.rows)) fit.fit();
    });
  }

  function attachWebgl(t: Terminal) {
    if (webgl || webglBroken) return;
    try {
      const addon = new WebglAddon();
      addon.onContextLoss(() => {
        addon.dispose();
        if (webgl === addon) webgl = null;
      });
      t.loadAddon(addon);
      webgl = addon;
    } catch {
      // No WebGL (e.g. some WebKitGTK setups): the DOM renderer still works.
      webglBroken = true;
    }
  }

  // WebGL contexts are limited per page, so only on-screen panes hold one.
  $effect(() => {
    if (!term) return;
    if (visible) {
      attachWebgl(term);
      scheduleFit();
    } else if (webgl) {
      webgl.dispose();
      webgl = null;
    }
  });

  $effect(() => {
    if (term && focused && search.paneId !== pane.id) term.focus();
  });

  $effect(() => {
    if (!term) return;
    term.options.theme = xtermTheme(settings.theme);
  });

  $effect(() => {
    if (!term) return;
    term.options.fontSize = settings.fontSize;
    scheduleFit();
  });

  // Settings that apply live when the config file changes.
  $effect(() => {
    if (!term) return;
    const cfg = settings.config;
    Object.assign(term.options, {
      lineHeight: cfg.lineHeight,
      cursorBlink: cfg.cursorBlink,
      cursorStyle: cfg.cursorStyle,
      scrollback: cfg.scrollback,
      macOptionIsMeta: cfg.optionAsMeta,
    });
    scheduleFit();
  });

  // A new font must be loaded before xterm measures it.
  $effect(() => {
    if (!term) return;
    const t = term;
    const family = settings.config.fontFamily;
    const stack = fontStack(family);
    if (t.options.fontFamily === stack) return;
    document.fonts.load(`${settings.fontSize}px "${family}"`).finally(() => {
      if (disposed) return;
      t.options.fontFamily = stack;
      scheduleFit();
    });
  });
</script>

<div class="pane" class:dimmed>
  <div class="host" bind:this={el}></div>
  {#if searchAddon && search.paneId === pane.id}
    <SearchBar addon={searchAddon} />
  {/if}
</div>

<style>
  .pane {
    position: absolute;
    inset: 0;
    padding: 10px 6px 6px 14px;
  }
  /* Unfocused panes in a split recede a little. */
  .pane::after {
    content: '';
    position: absolute;
    inset: 0;
    background: var(--surface);
    opacity: 0;
    pointer-events: none;
    transition: opacity 160ms ease;
  }
  .pane.dimmed::after {
    opacity: var(--dim-unfocused);
  }

  /* FitAddon measures the terminal's parent, so padding lives on .pane. */
  .host {
    width: 100%;
    height: 100%;
  }
  .host :global(.xterm) {
    height: 100%;
  }
  .host :global(.xterm-viewport) {
    background-color: transparent !important;
  }
</style>
