<script lang="ts">
  import { Clipboard } from '@wailsio/runtime';
  import { Service as ConfigService } from '../../bindings/github.com/dayanraj/wtf/internal/config';
  import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { openManual } from '../lib/help';
  import { leaves } from '../lib/layout';
  import { isMac } from '../lib/platform';
  import { recordings, saveOutputAsText, saveTranscript, type SavedFile } from '../lib/recording.svelte';
  import { redaction } from '../lib/redaction.svelte';
  import { settings } from '../lib/settings.svelte';
  import { tabs, tabTitle, type Pane, type Tab } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // The Share & Record tab: Hide Secrets and recording, as buttons, for
  // anyone who'd rather not remember their shortcuts.

  const pane = $derived(tabs.activePane);
  const tab = $derived(tabs.active);

  /** Every pane of this window being recorded, with its tab. */
  const recorded = $derived(
    tabs.list.flatMap((t) => leaves(t.root).filter((p) => recordings.of(p.id)).map((p) => ({ tab: t, pane: p }))),
  );

  /** Ticks once a second while something is recorded, for the elapsed times. */
  let now = $state(Date.now());
  $effect(() => {
    if (recorded.length === 0) return;
    now = Date.now();
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  const elapsed = (since: number) => {
    const s = Math.max(0, Math.floor((now - since) / 1000));
    const p = (n: number) => String(n).padStart(2, '0');
    return s >= 3600 ? `${Math.floor(s / 3600)}:${p(Math.floor(s / 60) % 60)}:${p(s % 60)}` : `${Math.floor(s / 60)}:${p(s % 60)}`;
  };

  const key = (mac: string, other: string) => (isMac ? mac : other);

  function toggleSecrets() {
    redaction.on = !redaction.on;
    toast.show(redaction.on ? 'Secrets hidden — on screen and when copied' : 'Secrets shown');
  }

  function setStartup(on: boolean) {
    ConfigService.SetRedactSecrets(on).catch((e) => toast.show(`Couldn't save the setting: ${e}`));
  }

  function show(t: Tab, p: Pane) {
    tabs.activate(t.id);
    tabs.focusPane(t.id, p.id);
  }

  const KIND: Record<SavedFile['kind'], string> = { recording: 'Recording', text: 'Output', markdown: 'Transcript' };
  const fileName = (path: string) => path.slice(path.lastIndexOf('/') + 1);
  const reveal = (path: string) => Files.Reveal(path).catch(() => toast.show(`${prettyPath(path)} isn't there any more`));
  const copyPath = (path: string) => Clipboard.SetText(path).then(() => toast.show('Copied path'));
</script>

<div class="share">
  <header>
    <span class="title">Share &amp; Record</span>
  </header>

  <div class="body">
    <!-- Hide Secrets -->
    <section>
      <h3><Icon name="shield" size={13} stroke={2} /> Hide Secrets</h3>
      <button class="toggle" class:on={redaction.on} role="switch" aria-checked={redaction.on} onclick={toggleSecrets}>
        <span class="knob" aria-hidden="true"></span>
        <span class="label">
          <strong>{redaction.on ? 'Secrets are hidden' : 'Secrets are shown'}</strong>
          <span>{redaction.on ? 'Covered on screen, [REDACTED] when copied or saved' : 'Turn on before you share your screen'}</span>
        </span>
      </button>
      <label class="check">
        <input type="checkbox" checked={settings.config.redactSecrets} onchange={(e) => setStartup(e.currentTarget.checked)} />
        Turn on whenever wtf starts
      </label>
      <p class="hint">
        Access keys, tokens, JWTs, passwords and private keys in every terminal. Best effort — it can miss a secret it
        doesn't recognize. <button class="link" onclick={() => openManual('secrets')}>What it covers</button>
        <span class="key">{key('⌥⌘R', 'Ctrl+Shift+Alt+X')}</span>
      </p>
    </section>

    <!-- Recording -->
    <section>
      <h3><Icon name="play" size={12} stroke={2} /> Record this terminal</h3>
      {#if pane && tab}
        {@const rec = recordings.of(pane.id)}
        <div class="target" title={prettyPath(pane.cwd)}>{tabTitle(tab, prettyPath)}</div>
        {#if rec}
          <div class="recording">
            <span class="dot"></span>
            <span class="rec-text">
              <strong>Recording · {elapsed(rec.since)}</strong>
              <span title={rec.path}>{prettyPath(rec.path)}</span>
            </span>
          </div>
          <button class="primary stop" onclick={() => recordings.stop(pane)}><Icon name="stop" size={12} stroke={2.2} /> Stop Recording</button>
        {:else}
          <button class="primary" onclick={() => recordings.toggle(pane)}><span class="dot"></span> Start Recording…</button>
        {/if}
        <p class="hint">
          An asciinema (.cast) file of what the terminal shows from now on, with its timing — not what you type. Kept as
          is: Hide Secrets doesn't apply to it. <span class="key">{key('⌥⌘S', 'Ctrl+Shift+Alt+M')}</span>
        </p>

        <h3 class="sub"><Icon name="download" size={12} stroke={2} /> Save what's there</h3>
        <button class="secondary" onclick={() => saveOutputAsText(pane)}>Save Output as Text…</button>
        <button
          class="secondary"
          disabled={!pane.integrated}
          title={pane.integrated ? '' : 'Needs shell integration (bash or zsh)'}
          onclick={() => saveTranscript(pane)}
        >
          Save Commands as Markdown…
        </button>
        <p class="hint">Markdown: each command with its exit status, start time, duration and output — for a postmortem or a ticket.</p>
      {:else}
        <p class="hint">Open a terminal to record it.</p>
      {/if}
    </section>

    {#if recorded.some((r) => r.pane.id !== pane?.id)}
      <section>
        <h3>Also recording</h3>
        {#each recorded.filter((r) => r.pane.id !== pane?.id) as r (r.pane.id)}
          {@const rec = recordings.of(r.pane.id)!}
          <div class="row">
            <span class="dot"></span>
            <button class="row-main" title="Show this terminal" onclick={() => show(r.tab, r.pane)}>
              <span class="name">{tabTitle(r.tab, prettyPath)}</span>
              <span class="meta">{elapsed(rec.since)} · {fileName(rec.path)}</span>
            </button>
            <button class="tool" title="Stop recording" aria-label="Stop recording" onclick={() => recordings.stop(r.pane)}>
              <Icon name="stop" size={12} stroke={2.2} />
            </button>
          </div>
        {/each}
      </section>
    {/if}

    {#if recordings.saved.length}
      <section>
        <h3>Saved in this window</h3>
        {#each recordings.saved as f (f.path)}
          <div class="row">
            <Icon name={f.kind === 'recording' ? 'play' : 'file'} size={12} stroke={2} />
            <button class="row-main" title="{f.path}&#10;Show in {isMac ? 'Finder' : 'the file manager'}" onclick={() => reveal(f.path)}>
              <span class="name">{fileName(f.path)}</span>
              <span class="meta">{KIND[f.kind]} · {new Date(f.at).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}</span>
            </button>
            <button class="tool" title="Copy path" aria-label="Copy path" onclick={() => copyPath(f.path)}><Icon name="copy" size={12} /></button>
          </div>
        {/each}
      </section>
    {/if}
  </div>
</div>

<style>
  .share {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  header {
    display: flex;
    align-items: center;
    flex: none;
    height: 30px;
    padding: 0 6px 0 10px;
  }
  .title {
    color: var(--chrome-text);
    font-size: 12px;
    font-weight: 600;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 10px 14px;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 0;
    border-top: 1px solid var(--hairline);
  }
  section:first-child {
    border-top: 0;
    padding-top: 2px;
  }
  h3 {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 0 2px;
    color: var(--chrome-text);
    font-size: 12px;
    font-weight: 600;
  }
  h3.sub {
    margin-top: 8px;
  }

  .toggle {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border: 1px solid var(--hairline);
    border-radius: 8px;
    background: transparent;
    color: var(--chrome-text);
    font: inherit;
    text-align: left;
  }
  .toggle:hover {
    background: var(--tab-hover);
  }
  .toggle.on {
    border-color: color-mix(in srgb, var(--g-green) 50%, transparent);
    background: color-mix(in srgb, var(--g-green) 14%, transparent);
  }
  .knob {
    position: relative;
    flex: none;
    width: 30px;
    height: 18px;
    border-radius: 9px;
    background: var(--tab-hover);
    box-shadow: inset 0 0 0 1px var(--hairline);
    transition: background-color 140ms ease;
  }
  .knob::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--chrome-text);
    transition: transform 140ms ease;
  }
  .toggle.on .knob {
    background: var(--g-green);
  }
  .toggle.on .knob::after {
    transform: translateX(12px);
    background: #fff;
  }
  .label {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    font-size: 11px;
    color: var(--chrome-muted);
  }
  .label strong {
    color: var(--chrome-text);
    font-size: 12px;
    font-weight: 600;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--chrome-text);
    font-size: 12px;
  }
  .hint {
    margin: 0;
    color: var(--chrome-muted);
    font-size: 11px;
    line-height: 1.45;
  }
  .key {
    margin-left: 2px;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--tab-hover);
    white-space: nowrap;
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    cursor: pointer;
  }

  .target {
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 11.5px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .primary,
  .secondary {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 28px;
    padding: 0 10px;
    border: 0;
    border-radius: 7px;
    font: inherit;
    font-size: 12px;
    font-weight: 600;
  }
  .primary {
    background: var(--accent);
    color: #fff;
  }
  .primary.stop {
    background: var(--danger);
  }
  .secondary {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .secondary:disabled {
    opacity: 0.45;
  }
  .primary:hover,
  .secondary:hover:not(:disabled) {
    filter: brightness(1.08);
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--danger);
  }
  .primary .dot {
    background: #fff;
  }

  .recording {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 7px;
    background: color-mix(in srgb, var(--danger) 14%, transparent);
  }
  .recording .dot {
    animation: rec 1.6s ease-in-out infinite;
  }
  @keyframes rec {
    50% {
      opacity: 0.35;
    }
  }
  .rec-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 11px;
    color: var(--chrome-muted);
  }
  .rec-text strong {
    color: var(--chrome-text);
    font-size: 12px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .rec-text span {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--chrome-muted);
  }
  .row-main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    padding: 3px 4px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    font: inherit;
    text-align: left;
  }
  .row-main:hover {
    background: var(--tab-hover);
  }
  .name {
    overflow: hidden;
    color: var(--chrome-text);
    font-size: 12px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .meta {
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 11px;
    white-space: nowrap;
    text-overflow: ellipsis;
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
  .tool:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
</style>
