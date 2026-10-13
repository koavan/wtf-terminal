<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Browser } from '@wailsio/runtime';
  import { Service as Tools, type Method, type Status } from '../../bindings/github.com/dayanraj/wtf/internal/tools';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { openManual } from '../lib/help';
  import { overlay } from '../lib/overlay.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';
  import { shellPATH } from '../lib/tools';

  // The command-line programs side-pane features run: where each is and its
  // version, or how to install it. Installing shows exactly what will run,
  // then runs it in a new tab, where sudo can ask for a password.

  let { focus, welcome }: { focus?: string; welcome?: string[] } = $props();

  let list = $state<Status[] | null>(null);
  let failed = $state('');
  let checking = $state(false);
  /** The tool being installed, and the chosen way. */
  let installing = $state<Status | null>(null);
  let methodId = $state('');
  let busy = $state(false);

  const method = $derived<Method | undefined>(installing?.methods?.find((m) => m.id === methodId));

  async function check() {
    checking = true;
    failed = '';
    try {
      list = (await Tools.Check(shellPATH())) ?? [];
    } catch (e) {
      failed = String(e);
    } finally {
      checking = false;
    }
  }

  onMount(async () => {
    await check();
    const id = untrack(() => focus);
    const t = list?.find((s) => s.id === id);
    if (t && t.methods?.length) choose(t);
  });

  function choose(t: Status) {
    installing = t;
    methodId = t.methods?.[0]?.id ?? '';
    failed = '';
  }

  async function run() {
    if (!installing || !method || busy) return;
    busy = true;
    try {
      const command = await Tools.Install(installing.id, method.id, shellPATH());
      const name = installing.name;
      overlay.close();
      tabs.open({ command });
      toast.show(`Installing ${name} — check again from Command-Line Tools when it's done`, 4000);
    } catch (e) {
      failed = String(e);
    } finally {
      busy = false;
    }
  }

  function manual() {
    overlay.close();
    openManual('tools');
  }

  const open = (url: string) => Browser.OpenURL(url);
</script>

{#if installing && method}
  <Dialog title="Install {installing.name}" subtitle={installing.about} width={600} onsubmit={run}>
    {#if (installing.methods?.length ?? 0) > 1}
      <div class="methods" role="radiogroup" aria-label="How to install">
        {#each installing.methods ?? [] as m (m.id)}
          <label class="check">
            <input type="radio" name="method" value={m.id} bind:group={methodId} />
            {m.label}{#if m === installing.methods?.[0]}<span class="muted">(recommended)</span>{/if}
          </label>
        {/each}
      </div>
    {/if}

    {#if method.script}
      <p class="hint">wtf writes this script to its cache folder and runs it with <code>sh</code> in a new tab:</p>
      <div class="command script">{method.script}</div>
    {:else}
      <p class="hint">Runs in a new tab:</p>
      <div class="command">{method.command}</div>
    {/if}

    <dl>
      <dt>Installs to</dt>
      <dd>{method.dest}</dd>
      <dt>Checked</dt>
      <dd>{method.verify}</dd>
      {#if method.sudo}
        <dt>Password</dt>
        <dd>Uses sudo; it asks for your password in the tab.</dd>
      {/if}
    </dl>
    {#if method.note}<p class="note">{method.note}</p>{/if}
    {#if failed}<div class="error" role="alert">{failed}</div>{/if}

    {#snippet buttons()}
      <button class="secondary" onclick={() => (installing = null)}>Back</button>
      <span class="spacer"></span>
      <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
      <button class="primary" onclick={run} disabled={busy}>Run in New Tab</button>
    {/snippet}
  </Dialog>
{:else}
  <Dialog title="Command-Line Tools" subtitle="Programs side-pane features run. wtf looks on the focused shell's PATH first, then where installers put them." width={600}>
    {#if welcome?.length}
      <p class="welcome">
        Welcome to wtf. {welcome.join(', ')}
        {welcome.length === 1 ? "isn't" : "aren't"} installed yet. The terminal works without them; the features below that use them say
        so when you open them. Install any now, or later from the palette (Command-Line Tools) or About wtf.
      </p>
    {/if}

    {#if list === null}
      <p class="hint">{failed || 'Checking…'}</p>
    {:else}
      <ul>
        {#each list as t (t.id)}
          <li class:missing={!t.found}>
            <span class="dot" class:ok={t.found && !t.missing?.length} aria-hidden="true"></span>
            <div class="info">
              <div class="head">
                <strong>{t.name}</strong>
                {#if t.found}
                  <span class="version">{t.version}</span>
                {:else}
                  <span class="absent">not installed</span>
                {/if}
              </div>
              {#if t.found}<div class="path" title={t.path}>{prettyPath(t.path)}</div>{/if}
              {#if t.found && t.missing?.length}<div class="absent">Not found: {t.missing.join(', ')}</div>{/if}
              <div class="used">Used by: {t.usedBy?.join(' · ')}</div>
              {#if !t.found && t.builtIn && !t.methods?.length}
                <div class="used">Part of {t.builtIn}; it may have been removed.</div>
              {:else if !t.found && !t.methods?.length}
                <div class="used">Install it with your system's package manager.</div>
              {/if}
            </div>
            <div class="actions">
              {#if t.methods?.length}
                <button class="primary small" onclick={() => choose(t)}>Install…</button>
              {/if}
              {#if t.homepage}
                <button class="link" onclick={() => open(t.homepage)} title={t.homepage}>Website</button>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
      {#if failed}<div class="error" role="alert">{failed}</div>{/if}
    {/if}

    {#snippet buttons()}
      <button class="secondary" onclick={manual}>Manual</button>
      <span class="spacer"></span>
      <button class="secondary" onclick={check} disabled={checking}>{checking ? 'Checking…' : 'Check Again'}</button>
      <button class="primary" onclick={() => overlay.close()}>Done</button>
    {/snippet}
  </Dialog>
{/if}

<style>
  .welcome {
    margin: 0 0 10px;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--accent-soft);
    font-size: 12.5px;
    line-height: 1.45;
  }
  ul {
    display: flex;
    flex-direction: column;
    max-height: min(420px, 60vh);
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }
  li {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 9px 2px;
    border-bottom: 1px solid var(--hairline);
  }
  li:last-child {
    border-bottom: 0;
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    margin-top: 5px;
    border-radius: 50%;
    background: var(--text-muted);
  }
  .dot.ok {
    background: var(--mark-ok);
  }
  .info {
    flex: 1;
    min-width: 0;
    font-size: 12px;
  }
  .head {
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .head strong {
    font-size: 13px;
  }
  .version,
  .path {
    color: var(--text-muted);
    font: 11.5px 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .absent {
    color: var(--text-muted);
    font-style: italic;
  }
  .used {
    margin-top: 2px;
    color: var(--text-muted);
  }
  .actions {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: 4px;
    align-items: flex-end;
  }
  button.small {
    height: 24px;
    padding: 0 10px;
    font-size: 12px;
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font-size: 11.5px;
  }
  .link:hover {
    text-decoration: underline;
  }
  .methods {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 8px;
  }
  .methods .check {
    margin: 0;
  }
  .muted {
    color: var(--text-muted);
  }
  .script {
    max-height: 240px;
    overflow-y: auto;
  }
  dl {
    display: grid;
    grid-template-columns: 84px 1fr;
    gap: 4px 10px;
    margin: 12px 0 0;
    font-size: 12px;
  }
  dt {
    color: var(--text-muted);
    font-weight: 600;
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .note {
    margin: 10px 0 0;
    font-size: 12px;
    line-height: 1.45;
    -webkit-user-select: text;
    user-select: text;
  }
</style>
