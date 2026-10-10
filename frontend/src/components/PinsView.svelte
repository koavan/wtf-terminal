<script lang="ts">
  import { Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { leaves } from '../lib/layout';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { pins, type Pin } from '../lib/pins.svelte';
  import { tabs, tabTitle } from '../lib/tabs.svelte';
  import { terminalFor } from '../lib/terminals';
  import { formatDuration, toast } from '../lib/toast.svelte';

  // The Pinned tab: output kept in view while you work (see lib/pins).
  // Pins of commands still running are refreshed while this tab is on
  // screen; when it isn't, they catch up when the command finishes.

  /** How often a running command's pin is refreshed while it's on screen. */
  const LIVE_MS = 1500;

  $effect(() => {
    if (pins.live.length === 0) return;
    const timer = setInterval(() => {
      for (const p of pins.live) {
        const text = p.blockId === null ? null : (terminalFor(p.paneId)?.blockOutput(p.blockId) ?? null);
        if (text !== null && text !== p.text) pins.update(p.paneId, p.blockId!, text);
      }
    }, LIVE_MS);
    return () => clearInterval(timer);
  });

  /** The tab a pin's pane is in, while it exists. */
  function whereIs(p: Pin) {
    for (const tab of tabs.list) if (leaves(tab.root).some((x) => x.id === p.paneId)) return tab;
    return null;
  }

  const time = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', second: '2-digit' });

  const status = (p: Pin) => (p.blockId === null ? 'selection' : p.running ? 'running' : p.exitCode === 0 ? 'ok' : 'fail');

  function copy(p: Pin) {
    Clipboard.SetText(p.text).then(() => toast.show('Copied pinned output'));
  }

  function reveal(p: Pin) {
    const tab = whereIs(p);
    if (!tab) return;
    tabs.activate(tab.id);
    tabs.focusPane(tab.id, p.paneId);
    if (p.blockId !== null && !terminalFor(p.paneId)?.revealCommand(p.blockId)) toast.show('That command has scrolled out of the terminal');
  }

  function menu(ev: MouseEvent, p: Pin) {
    ev.preventDefault();
    const entries: MenuEntry[] = [];
    if (whereIs(p)) entries.push({ label: p.blockId === null ? 'Show Tab' : 'Show in Terminal', action: () => reveal(p) });
    entries.push({ label: 'Copy Output', action: () => copy(p) });
    if (p.blockId !== null) {
      entries.push(
        { label: 'Copy Command', action: () => Clipboard.SetText(p.title).then(() => toast.show('Copied command')) },
        { label: 'Save as Snippet…', action: () => overlay.show({ kind: 'snippetEdit', snippet: null, command: p.title }) },
      );
    }
    entries.push('separator', { label: p.collapsed ? 'Expand' : 'Collapse', action: () => (p.collapsed = !p.collapsed) });
    entries.push({ label: 'Unpin', action: () => pins.remove(p.id) });
    overlay.openMenu(ev.clientX, ev.clientY, entries);
  }

  function clearAll() {
    const n = pins.list.length;
    overlay.confirm({
      title: 'Unpin everything?',
      message: `Removes ${n} pinned ${n === 1 ? 'output' : 'outputs'}. The terminals keep their own scrollback.`,
      items: [],
      confirmLabel: 'Unpin All',
      onConfirm: () => pins.clear(),
    });
  }
</script>

<div class="pins">
  <header>
    <span class="title">Pinned output</span>
    <button class="tool" title="Unpin all" aria-label="Unpin all" disabled={pins.list.length === 0} onclick={clearAll}>
      <Icon name="trash" size={13} />
    </button>
  </header>

  <div class="list">
    {#each pins.list as p (p.id)}
      {@const s = status(p)}
      {@const tab = whereIs(p)}
      <section class="pin" aria-label="Pinned: {p.title}">
        <div class="head" role="presentation" oncontextmenu={(ev) => menu(ev, p)} ondblclick={() => (p.collapsed = !p.collapsed)}>
          <span class="mark {s}" aria-label={s === 'selection' ? 'selection' : s === 'running' ? 'running' : s === 'ok' ? 'succeeded' : `failed with exit code ${p.exitCode}`}>
            {#if s === 'running'}<span class="pulse"></span>
            {:else if s === 'selection'}<Icon name="pin" size={11} stroke={2.2} />
            {:else}<Icon name={s === 'ok' ? 'check' : 'x'} size={11} stroke={2.6} />{/if}
          </span>
          <span class="main">
            <span class="cmd" class:sel={s === 'selection'} title={p.title}>{p.title}</span>
            <span class="sub">
              {#if s === 'running'}
                <span>running · following it</span>
              {:else if p.durationMs !== undefined}
                <span>{formatDuration(p.durationMs)}</span>
                {#if p.exitCode !== 0}<span class="exit">· exit {p.exitCode}</span>{/if}
              {:else}
                <span>pinned {time(p.pinnedAt)}</span>
              {/if}
              <span class="where" title={prettyPath(p.cwd)}>· {tab ? tabTitle(tab, prettyPath) : 'closed tab'}</span>
            </span>
          </span>
          <button class="tool" title="Copy" aria-label="Copy pinned output" onclick={() => copy(p)}><Icon name="copy" size={12} /></button>
          <button class="tool" title={p.collapsed ? 'Expand' : 'Collapse'} aria-label={p.collapsed ? 'Expand' : 'Collapse'} aria-expanded={!p.collapsed} onclick={() => (p.collapsed = !p.collapsed)}>
            <Icon name={p.collapsed ? 'chevronDown' : 'chevronUp'} size={12} />
          </button>
          <button class="tool" title="Unpin" aria-label="Unpin" onclick={() => pins.remove(p.id)}><Icon name="x" size={12} /></button>
        </div>
        {#if !p.collapsed}
          {#if p.truncated}<div class="cut">Showing the last {Math.round(p.text.length / 1000)}k characters</div>{/if}
          {#if p.text}
            <pre class="out">{p.text}</pre>
          {:else}
            <div class="empty">{s === 'running' ? 'No output yet' : 'No output'}</div>
          {/if}
        {/if}
      </section>
    {:else}
      <div class="note">
        <strong>Nothing pinned</strong>
        Keep a command's output in view while you work on: click its ✓/✗ mark (or right-click its output) and choose
        <em>Pin Output</em>, or select text and right-click → <em>Pin Selection</em>. A command still running keeps its
        pin up to date.
      </div>
    {/each}
  </div>
</div>

<style>
  .pins {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  header {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 30px;
    padding: 0 6px 0 10px;
  }
  .title {
    flex: 1;
    color: var(--chrome-text);
    font-size: 12px;
    font-weight: 600;
  }
  .tool {
    display: grid;
    place-items: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .tool:hover:not(:disabled) {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tool:disabled {
    opacity: 0.4;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }
  .pin {
    flex: none;
    border: 1px solid var(--hairline);
    border-radius: 8px;
    overflow: hidden;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 4px 5px 8px;
    color: var(--chrome-text);
  }
  .head:hover {
    background: var(--tab-hover);
  }

  .mark {
    display: grid;
    place-items: center;
    flex: none;
    width: 16px;
    height: 16px;
    border-radius: 50%;
  }
  .mark.ok {
    color: var(--mark-ok);
  }
  .mark.fail {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger);
  }
  .mark.running,
  .mark.selection {
    color: var(--accent);
  }
  .pulse {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: currentColor;
    animation: pulse 1.4s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.25;
      transform: scale(0.7);
    }
  }

  .main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .cmd {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font: 500 12px 'JetBrains Mono Variable', monospace;
  }
  .cmd.sel {
    font-family: inherit;
    font-weight: 600;
  }
  .sub {
    display: flex;
    gap: 4px;
    overflow: hidden;
    white-space: nowrap;
    color: var(--chrome-muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .exit {
    color: var(--danger);
  }
  .where {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .out {
    height: 180px;
    min-height: 60px;
    max-height: 70vh;
    margin: 0;
    padding: 6px 9px;
    overflow: auto;
    border-top: 1px solid var(--hairline);
    background: var(--identity);
    color: var(--chrome-text);
    font: 11.5px/1.45 'JetBrains Mono Variable', monospace;
    white-space: pre;
    resize: vertical;
    -webkit-user-select: text;
    user-select: text;
  }
  .cut,
  .empty {
    padding: 4px 9px;
    border-top: 1px solid var(--hairline);
    color: var(--chrome-muted);
    font-size: 11px;
  }

  .note {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 8px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .note em {
    color: var(--chrome-text);
    font-style: normal;
    font-weight: 600;
  }
</style>
