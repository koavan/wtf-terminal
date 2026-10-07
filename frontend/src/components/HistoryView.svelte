<script lang="ts">
  import { Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { history, type HistoryEntry } from '../lib/history.svelte';
  import { leaves } from '../lib/layout';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { sidebar } from '../lib/sidebar.svelte';
  import { CLEAR_HISTORY_KEYS } from '../lib/shell';
  import { paneTitle, tabs, type Pane } from '../lib/tabs.svelte';
  import { activeTerminal, terminalFor } from '../lib/terminals';
  import { formatDuration, toast } from '../lib/toast.svelte';

  let filter = $state('');

  const pane = $derived(tabs.activePane);

  /** Every pane in this window, with the tab it lives in. */
  const panes = $derived(tabs.list.flatMap((tab) => leaves(tab.root).map((p) => ({ tab, pane: p }))));

  const entries = $derived.by(() => {
    const list = sidebar.historyScope === 'pane' ? (pane ? history.of(pane.id) : []) : history.merged(panes.map((p) => p.pane.id));
    const q = filter.trim().toLowerCase();
    const shown = q ? list.filter((e) => `${e.command} ${e.cwd}`.toLowerCase().includes(q)) : list;
    return [...shown].reverse(); // newest first
  });

  function where(e: HistoryEntry) {
    return panes.find((p) => p.pane.id === e.paneId);
  }

  const time = (ms: number) => {
    const d = new Date(ms);
    const sameDay = d.toDateString() === new Date().toDateString();
    const t = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', second: '2-digit' });
    return sameDay ? t : `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${t}`;
  };

  /** Whether the focused pane's shell is waiting at its prompt. */
  const idle = () => !!tabs.activePane && tabs.activePane.isShell && !tabs.activePane.running;

  function insert(e: HistoryEntry) {
    const term = activeTerminal();
    if (!term) return;
    term.paste(e.command);
    term.focus();
  }

  function runAgain(e: HistoryEntry) {
    const term = activeTerminal();
    if (!term) return;
    if (!idle()) {
      toast.show('The shell is busy — finish the running program first');
      return;
    }
    term.type(`\x15${e.command}\r`); // Ctrl-U clears anything half-typed first
    term.focus();
  }

  function reveal(e: HistoryEntry) {
    const w = where(e);
    if (!w) return;
    tabs.activate(w.tab.id);
    tabs.focusPane(w.tab.id, w.pane.id);
    if (!terminalFor(w.pane.id)?.revealCommand(e.id)) toast.show('That command has scrolled out of the terminal');
  }

  function menu(ev: MouseEvent, e: HistoryEntry) {
    ev.preventDefault();
    const entries: MenuEntry[] = [
      { label: 'Run Again', hint: 'double-click', action: () => runAgain(e) },
      { label: 'Insert at Prompt', hint: 'click', action: () => insert(e) },
      { label: 'Show in Terminal', action: () => reveal(e) },
      'separator',
      { label: 'Copy Command', action: () => Clipboard.SetText(e.command).then(() => toast.show('Copied command')) },
      { label: 'Copy Folder', action: () => Clipboard.SetText(e.cwd).then(() => toast.show('Copied folder')) },
    ];
    overlay.openMenu(ev.clientX, ev.clientY, entries);
  }

  /**
   * Clears the list and, for shells sitting at their prompt, the shell's own
   * history for this session (↑ / Ctrl-R) — after asking.
   */
  function clear() {
    const targets = sidebar.historyScope === 'pane' ? (pane ? [pane] : []) : panes.map((p) => p.pane);
    const count = targets.reduce((n, p) => n + history.of(p.id).length, 0);
    const shellIdle = (p: Pane) => p.integrated && p.isShell && !p.running;
    const busy = targets.filter((p) => p.integrated && !shellIdle(p));
    const label = (p: Pane) => paneTitle(p, prettyPath);

    overlay.confirm({
      title: targets.length > 1 ? 'Clear command history for all panes?' : 'Clear command history?',
      message:
        `Removes ${count} ${count === 1 ? 'command' : 'commands'} from this list and from the shell's own ` +
        `history for this session (↑ and Ctrl-R). History from earlier sessions stays, and so do ` +
        `commands your shell already wrote to its history file.`,
      items: busy.map((p) => `${label(p)}: a program is running — only the list will be cleared`),
      confirmLabel: 'Clear History',
      onConfirm: () => {
        let shells = 0;
        for (const p of targets) {
          history.clear(p.id);
          if (shellIdle(p)) {
            terminalFor(p.id)?.type(CLEAR_HISTORY_KEYS);
            shells++;
          }
        }
        toast.show(
          busy.length
            ? `Cleared — ${busy.length} busy ${busy.length === 1 ? "shell's" : "shells'"} own history left as is`
            : shells
              ? `Cleared this session's history${shells > 1 ? ` in ${shells} shells` : ''}`
              : 'Cleared the list',
        );
      },
    });
  }

  const status = (e: HistoryEntry) => (e.finishedAt === null ? 'running' : e.exitCode === 0 ? 'ok' : 'fail');
</script>

<div class="hist">
  <header>
    <div class="scope" role="radiogroup" aria-label="Show commands from">
      <button role="radio" aria-checked={sidebar.historyScope === 'pane'} class:on={sidebar.historyScope === 'pane'} onclick={() => (sidebar.historyScope = 'pane')}>This pane</button>
      <button role="radio" aria-checked={sidebar.historyScope === 'all'} class:on={sidebar.historyScope === 'all'} onclick={() => (sidebar.historyScope = 'all')}>All panes</button>
    </div>
    <button class="tool" title="Clear history" aria-label="Clear history" disabled={entries.length === 0 && !filter} onclick={clear}>
      <Icon name="trash" size={13} />
    </button>
  </header>

  <div class="filter">
    <Icon name="search" size={12} stroke={2} />
    <input bind:value={filter} placeholder="Filter commands" spellcheck="false" autocomplete="off" />
  </div>

  <div class="list">
    {#each entries as e (`${e.paneId}:${e.id}:${e.startedAt}`)}
      {@const s = status(e)}
      {@const w = sidebar.historyScope === 'all' ? where(e) : undefined}
      <div
        class="row"
        role="button"
        title="{e.command}&#10;in {prettyPath(e.cwd)}&#10;started {time(e.startedAt)}"
        onclick={(ev) => ev.detail === 1 && insert(e)}
        ondblclick={() => runAgain(e)}
        oncontextmenu={(ev) => menu(ev, e)}
        onkeydown={() => {}}
        tabindex="-1"
      >
        <span class="mark {s}" aria-label={s === 'running' ? 'running' : s === 'ok' ? 'succeeded' : `failed with exit code ${e.exitCode}`}>
          {#if s === 'running'}<span class="pulse"></span>{:else}<Icon name={s === 'ok' ? 'check' : 'x'} size={11} stroke={2.6} />{/if}
        </span>
        <span class="main">
          <span class="cmd">{e.command}</span>
          <span class="sub">
            {#if e.finishedAt !== null}
              <span>{time(e.finishedAt)}</span>
              <span>· {formatDuration(e.durationMs ?? 0)}</span>
              {#if e.exitCode !== 0}<span class="exit">· exit {e.exitCode}</span>{/if}
            {:else}
              <span>running · since {time(e.startedAt)}</span>
            {/if}
            <span class="cwd">· {prettyPath(e.cwd)}</span>
          </span>
        </span>
        {#if w}<span class="owner" title="In “{paneTitle(w.pane, prettyPath)}”">{paneTitle(w.pane, prettyPath)}</span>{/if}
      </div>
    {:else}
      <div class="note">
        {#if filter}
          No commands match “{filter}”.
        {:else if sidebar.historyScope === 'pane' && pane && !pane.integrated}
          <strong>No history for this shell</strong>
          History comes from shell integration, which supports bash and zsh. Check that
          <code>shell-integration</code> is on in your settings; it applies to new panes.
        {:else}
          <strong>No commands yet</strong>Commands you run here appear with when they finished and how long they took.
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .hist {
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
    padding: 0 6px 0 8px;
  }
  .scope {
    display: flex;
    flex: 1;
    padding: 2px;
    border-radius: 7px;
    background: var(--tab-hover);
  }
  .scope button {
    flex: 1;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--chrome-muted);
    font-size: 11.5px;
    font-weight: 600;
    white-space: nowrap;
  }
  .scope button.on {
    background: var(--tab-active);
    color: var(--chrome-text);
    box-shadow: var(--tab-active-shadow);
  }
  .tool {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
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

  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 26px;
    margin: 2px 8px 4px;
    padding: 0 8px;
    border-radius: 7px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
  }
  .filter input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--chrome-text);
    font: inherit;
    font-size: 12px;
    -webkit-user-select: text;
    user-select: text;
  }
  .filter input:focus {
    outline: none;
  }
  .filter input::placeholder {
    color: var(--chrome-muted);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 38px;
    padding: 4px 6px 4px 8px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .row:focus {
    outline: none;
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
  .mark.running {
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
  .cwd {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .owner {
    flex: none;
    max-width: 38%;
    padding: 1px 6px;
    overflow: hidden;
    border-radius: 6px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
    text-overflow: ellipsis;
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
  code {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11px;
  }
</style>
