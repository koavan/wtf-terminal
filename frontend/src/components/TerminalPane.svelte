<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { SearchAddon } from '@xterm/addon-search';
  import { SerializeAddon } from '@xterm/addon-serialize';
  import { WebglAddon } from '@xterm/addon-webgl';
  import { WebLinksAddon } from '@xterm/addon-web-links';
  import { Unicode11Addon } from '@xterm/addon-unicode11';
  import { Browser, Clipboard, Events } from '@wailsio/runtime';
  import SearchBar from './SearchBar.svelte';
  import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
  import { Service, endpoint } from '../lib/backend';
  import { blockMenu, commandAndOutput, terminalMenu } from '../lib/blocks';
  import { broadcast, isTerminalReport } from '../lib/broadcast.svelte';
  import { openInEditor } from '../lib/fileActions';
  import { tokenMenu } from '../lib/logActions';
  import { findJson, type LogToken } from '../lib/logs';
  import { LogView, logColors, readLogicalLine, tokenAt } from '../lib/logview';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { toast } from '../lib/toast.svelte';
  import { history } from '../lib/history.svelte';
  import { parseEnv, resolveContext } from '../lib/infra';
  import { notifyCommandDone } from '../lib/notify';
  import { isMac } from '../lib/platform';
  import { PromptTracker } from '../lib/prompts';
  import { search } from '../lib/search.svelte';
  import { settings } from '../lib/settings.svelte';
  import { detachedSessions, tabs, type Pane } from '../lib/tabs.svelte';
  import { commandFinished, registerTerminal, terminalFor } from '../lib/terminals';
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
  /** A file from the tree is being dragged over this pane. */
  let dropping = $state(false);
  /** Context variables from the shell's last prompt (see lib/infra). */
  let reportedEnv: Record<string, string> | null = null;
  /** Colors and ⌘-click actions for logs (see lib/logview). */
  let logView: LogView | undefined;

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

    logView = new LogView(t, {
      colors: logColors(settings.theme),
      onActivate: (token, e) => openTokenMenu(token, e.clientX, e.clientY),
      resolveFile,
      isLinkClick: (e) => (isMac ? e.metaKey : e.ctrlKey),
    });

    // ⌘-combos belong to the app menu; don't let xterm swallow them.
    t.attachCustomKeyEventHandler((e) => !(isMac && e.metaKey));

    t.onData((data) => {
      const bytes = encoder.encode(data);
      send(bytes);
      fanOut(t, data, bytes);
    });
    t.onBinary((data) => {
      const bytes = Uint8Array.from(data, (c) => c.charCodeAt(0));
      send(bytes);
      fanOut(t, data, bytes);
    });
    t.onResize(({ cols, rows }) => {
      if (sessionId) Service.Resize(sessionId, cols, rows);
      tracker?.relayout();
    });
    t.onTitleChange((title) => tabs.setOscTitle(pane.id, title));
    t.onSelectionChange(() => {
      if (settings.config.copyOnSelect && t.hasSelection()) Clipboard.SetText(t.getSelection());
    });
    // Clicking into a pane focuses it.
    t.textarea?.addEventListener('focus', () => tabs.focusPane(tabId, pane.id));

    const prompts = (tracker = new PromptTracker(t, {
      onPrompt: () => tabs.markIntegrated(pane.id),
      onCommandStart: (text, id) => {
        tabs.commandStarted(pane.id, text);
        history.started(pane.id, id, text, pane.cwd);
      },
      onCommandEnd: (exitCode, durationMs) => {
        history.finished(pane.id, exitCode, durationMs);
        tabs.commandFinished(pane.id, exitCode, durationMs);
        notifyCommandDone(pane, exitCode, durationMs, visible);
        commandFinished(pane.command, exitCode);
      },
      onMark: (id, e) => openMenu(e.clientX, e.clientY, id, false),
      onEnv: (text) => {
        reportedEnv = parseEnv(text);
        tabs.setEnv(pane.id, reportedEnv);
        refreshContext();
      },
    }));

    const unregister = registerTerminal(pane.id, {
      focus: () => t.focus(),
      clear: () => t.clear(),
      selectAll: () => t.selectAll(),
      getSelection: () => t.getSelection(),
      paste: (text) => t.paste(text),
      type: (text) => t.input(text),
      sendInput: (bytes) => send(bytes),
      jumpPrompt: (direction) => prompts.jump(direction),
      lastOutput: () => prompts.lastOutput(),
      revealCommand: (id) => prompts.reveal(id),
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

    // An AWS account lookup finished (see internal/infra).
    const offInfra = Events.On('infra:changed', refreshContext);

    const resizeObserver = new ResizeObserver(scheduleFit);
    resizeObserver.observe(el);

    term = t;
    searchAddon = s;
    start(t);

    return () => {
      disposed = true;
      offInfra();
      resizeObserver.disconnect();
      unregister();
      prompts.dispose();
      logView?.dispose();
      // The history belongs to this shell; a tab moving windows starts fresh there.
      history.clear(pane.id);
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
      syncInfo(id);
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
    syncInfo(id);
  }

  /** The folder and program now: the backend reports only changes, and one
   *  sent before this pane knew its session (a slow first launch) was lost. */
  function syncInfo(id: string) {
    Service.Info(id)
      .then((info) => tabs.applyInfo(info))
      .catch(() => {});
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
    socket.onopen = () => {
      opened = true;
      // The shell reads this once it's ready; the tty holds it until then.
      if (pane.initialCommand) {
        socket.send(encoder.encode(pane.initialCommand + '\r'));
        pane.initialCommand = undefined;
      }
    };
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

  /** Works out the shell's cloud/cluster context from what it last reported. */
  function refreshContext() {
    if (reportedEnv && settings.config.contextBadges) {
      resolveContext(pane.id, reportedEnv, (ctx) => tabs.setInfra(pane.id, ctx));
    }
  }

  /** The command-block tracker, once the terminal exists. */
  let tracker: PromptTracker | undefined;
  /** Undoes the highlight of the block a menu is about. */
  let unhighlight: (() => void) | null = null;

  // The highlight goes when the menu does.
  $effect(() => {
    if (overlay.current?.kind !== 'menu' && unhighlight) {
      unhighlight();
      unhighlight = null;
    }
  });

  /** The buffer line under the mouse. */
  function lineAt(clientY: number): number | null {
    const screen = el?.querySelector('.xterm-screen');
    if (!term || !screen) return null;
    const r = screen.getBoundingClientRect();
    const row = Math.floor(((clientY - r.top) / r.height) * term.rows);
    if (row < 0 || row >= term.rows) return null;
    return term.buffer.active.viewportY + row;
  }

  function copyText(text: string, what: string) {
    Clipboard.SetText(text).then(() => {
      const lines = text ? text.split('\n').length : 0;
      toast.show(lines > 1 ? `Copied ${what} (${lines} lines)` : `Copied ${what}`);
    });
  }

  /**
   * The terminal's menu: the actions of the command block at a point (from
   * its mark, or a right-click in its output), then — for a right-click —
   * copy, paste and the rest.
   */
  function openMenu(x: number, y: number, id: number | null, general: boolean, first: MenuEntry[] = []) {
    const t = term;
    if (!t || !tracker) return;
    const p = tracker;
    const entries: MenuEntry[] = [...first];
    const info = id !== null ? p.info(id) : null;
    if (info) {
      if (entries.length) entries.push('separator');
      entries.push(
        ...blockMenu(info, {
          copyCommand: () => copyText(info.text, 'command'),
          copyOutput: () => copyText(p.output(info.id) ?? '', 'output'),
          copyBoth: () => copyText(commandAndOutput(info.text, p.output(info.id) ?? ''), 'command and output'),
          selectOutput: () => {
            if (!p.selectOutput(info.id)) toast.show('Its output has left the scrollback');
          },
          runAgain: () => {
            if (!pane.isShell || pane.running) {
              toast.show('The shell is busy — finish the running program first');
              return;
            }
            // Ctrl-U clears anything half-typed first. Sent to this pane only.
            send(encoder.encode(`\x15${info.text}\r`));
            t.focus();
          },
          saveSnippet: () => overlay.show({ kind: 'snippetEdit', snippet: null, command: info.text }),
        }),
      );
      unhighlight?.();
      unhighlight = p.highlight(info.id);
    }
    if (general) {
      if (entries.length) entries.push('separator');
      entries.push(
        ...terminalMenu(t.hasSelection(), {
          copy: () => Clipboard.SetText(t.getSelection()),
          paste: () => Clipboard.Text().then((text) => text && t.paste(text)),
          selectAll: () => t.selectAll(),
          clear: () => t.clear(),
          find: () => {
            tabs.focusPane(tabId, pane.id);
            search.open();
          },
        }),
      );
    }
    if (entries.length) overlay.openMenu(x, y, entries);
  }

  /** The buffer column under the mouse. */
  function columnAt(clientX: number): number | null {
    const screen = el?.querySelector('.xterm-screen');
    if (!term || !screen) return null;
    const r = screen.getBoundingClientRect();
    const col = Math.floor(((clientX - r.left) / r.width) * term.cols);
    return col < 0 || col >= term.cols ? null : col;
  }

  async function onContextMenu(e: MouseEvent) {
    e.preventDefault();
    tabs.focusPane(tabId, pane.id);
    const line = lineAt(e.clientY);
    const first = await outputEntries(line, columnAt(e.clientX));
    openMenu(e.clientX, e.clientY, line === null || !tracker ? null : tracker.blockAt(line), true, first);
  }

  /**
   * For a right-click in the output: the actions of the token under the
   * mouse (a timestamp, IP, pod…), and Pretty-print JSON when the selection
   * or the line holds JSON.
   */
  async function outputEntries(line: number | null, col: number | null): Promise<MenuEntry[]> {
    const t = term;
    if (!t || line === null) return [];
    const buf = t.buffer.active;
    const entries: MenuEntry[] = [];
    const json = findJson(t.hasSelection() ? t.getSelection() : (readLogicalLine(buf, line)?.text ?? ''));
    let token = col === null ? null : (tokenAt(buf, line, col)?.token ?? null);
    // file:line has actions only for a file on this machine.
    if (token?.kind === 'path') {
      const path = await resolveFile(token.path);
      token = path ? { ...token, path } : null;
    }
    if (token) entries.push(...tokenMenu(token, kubeTarget(), tokenActions));
    if (json) {
      if (entries.length) entries.push('separator');
      entries.push({ label: 'Pretty-print JSON', action: () => overlay.show({ kind: 'json', text: json }) });
    }
    return entries;
  }

  /** The cluster and namespace this shell reported, for kubectl commands from its output. */
  function kubeTarget() {
    return { context: pane.infra?.kubeContext ?? '', namespace: pane.infra?.kubeNamespace ?? '', env: pane.env ?? {} };
  }

  const tokenActions = {
    copy: copyText,
    openUrl: (url: string) => Browser.OpenURL(url),
    runInTab: (command: string) => tabs.open({ command, cwd: pane.cwd || undefined }),
    openInEditor,
    openFile: (path: string) => Files.Open(path).catch((err: unknown) => toast.show(`Couldn't open it: ${err}`)),
    reveal: (path: string) => Files.Reveal(path).catch((err: unknown) => toast.show(`Couldn't reveal it: ${err}`)),
    showSecret: (name: string) => {
      const k = kubeTarget();
      overlay.show({ kind: 'secretView', name, ...k });
    },
  };

  function openTokenMenu(token: LogToken, x: number, y: number) {
    if (token.kind === 'url') {
      Browser.OpenURL(token.text);
      return;
    }
    tabs.focusPane(tabId, pane.id);
    const entries = tokenMenu(token, kubeTarget(), tokenActions);
    if (entries.length) overlay.openMenu(x, y, entries);
  }

  /** Files that file:line references name, briefly remembered: the mouse asks again and again. */
  const resolved = new Map<string, { path: string | null; at: number }>();
  async function resolveFile(name: string): Promise<string | null> {
    const key = `${pane.cwd}\0${name}`;
    const hit = resolved.get(key);
    if (hit && performance.now() - hit.at < 5000) return hit.path;
    const path = (await Files.ResolveFile(pane.cwd, name).catch(() => '')) || null;
    if (resolved.size > 200) resolved.clear();
    resolved.set(key, { path, at: performance.now() });
    return path;
  }

  function send(bytes: Uint8Array) {
    if (ws?.readyState === WebSocket.OPEN) ws.send(bytes);
  }

  /**
   * Broadcast input: what the user types here also goes to the other panes
   * of the broadcast. Only typing — not replies xterm sends for the program
   * — and only from the pane with keyboard focus.
   */
  function fanOut(t: Terminal, data: string, bytes: Uint8Array) {
    if (!broadcast.on || document.activeElement !== t.textarea || isTerminalReport(data)) return;
    for (const id of broadcast.targets(pane.id)) terminalFor(id)?.sendInput(bytes);
  }

  const broadcasting = $derived(broadcast.on && broadcast.includes(pane.id));

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
      // Another tab may have switched the shared kubeconfig meanwhile.
      untrack(refreshContext);
    } else if (webgl) {
      webgl.dispose();
      webgl = null;
    }
  });

  $effect(() => {
    if (term && focused && search.paneId !== pane.id) term.focus();
  });

  $effect(() => {
    if (settings.config.contextBadges) untrack(refreshContext);
    else untrack(() => tabs.setInfra(pane.id, null));
  });

  $effect(() => {
    if (!term) return;
    term.options.theme = xtermTheme(settings.theme);
    if (logView) logView.colors = logColors(settings.theme);
  });

  $effect(() => {
    if (!term || !logView) return;
    logView.highlight = settings.config.logHighlighting;
    logView.visible = visible;
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

<div
  class="pane"
  class:dimmed
  class:drop-target={dropping}
  class:broadcasting
  role="presentation"
  ondragover={(e) => {
    if (!e.dataTransfer?.types.includes('application/x-wtf-path')) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    dropping = true;
  }}
  ondragleave={() => (dropping = false)}
  ondrop={(e) => {
    dropping = false;
    const text = e.dataTransfer?.getData('text/plain');
    if (!text || !term) return;
    e.preventDefault();
    tabs.focusPane(tabId, pane.id);
    term.paste(text + ' ');
    term.focus();
  }}
>
  <div class="host" bind:this={el} oncontextmenu={onContextMenu} role="presentation"></div>
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
  .pane.drop-target::after {
    opacity: 1;
    background: var(--accent-soft);
    box-shadow: inset 0 0 0 2px var(--accent);
    border-radius: 8px;
  }

  /* Part of a broadcast: every keystroke here goes to the other panes too. */
  .pane.broadcasting::before {
    content: '';
    position: absolute;
    inset: 2px;
    border: 1.5px solid var(--g-orange);
    border-radius: 8px;
    pointer-events: none;
    z-index: 1;
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
