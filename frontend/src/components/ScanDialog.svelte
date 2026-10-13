<script lang="ts">
  import { onMount } from 'svelte';
  import { Service, type Scan, type Scanner } from '../../bindings/github.com/dayanraj/wtf/internal/findings';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { sidebar } from '../lib/sidebar.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { runInPane } from '../lib/terminals';
  import { shellPATH, showTools } from '../lib/tools';
  import { toast } from '../lib/toast.svelte';

  // Run Scan: picks a scanner, shows the exact command, and types it into
  // the focused shell (a new tab if it's busy). Its SARIF goes to wtf's
  // cache; the Findings tab reads it when the command finishes.

  const pane = tabs.activePane;
  const folder = pane?.cwd ?? '';
  let scanners = $state<Scanner[] | null>(null);
  let chosen = $state(lastChoice);
  let scan = $state<Scan | null>(null);
  let error = $state('');

  onMount(async () => {
    try {
      const list = (await Service.Scanners(shellPATH())) ?? [];
      if (!list.some((s) => s.id === chosen)) chosen = list.find((s) => s.found)?.id ?? list[0]?.id ?? '';
      scanners = list;
    } catch (e) {
      error = String(e);
    }
  });

  const current = $derived(scanners?.find((s) => s.id === chosen) ?? null);

  // The command for the chosen scanner, worked out by the backend (it knows the repository's root).
  $effect(() => {
    const id = chosen;
    scan = null;
    if (!id || !folder) return;
    Service.ScanCommand(folder, id).then(
      (s) => {
        if (chosen === id) scan = s;
      },
      (e) => (error = String(e)),
    );
  });

  function run() {
    if (!scan || !current?.found) return;
    lastChoice = chosen;
    const command = scan.command;
    overlay.close();
    if (runInPane(pane, command) === 'tab') toast.show('Scanning in a new tab: that shell was busy');
    sidebar.tab = 'findings';
    sidebar.open = true;
  }

  function install() {
    if (current) showTools(current.tool);
  }
</script>

<script lang="ts" module>
  let lastChoice = 'trivy';
</script>

<Dialog title="Run Security Scan" subtitle={scan ? `Scans ${prettyPath(scan.root)}. Results show in the Findings tab when it finishes.` : 'Scans the focused shell’s repository.'} width={520} onsubmit={run}>
  {#if !folder}
    <div class="error" role="alert">Focus a terminal first: the scan runs in its folder.</div>
  {:else if !scanners}
    <p class="hint">Looking for scanners…</p>
  {:else}
    <div class="scanners" role="radiogroup" aria-label="Scanner">
      {#each scanners as s (s.id)}
        <label class="scanner" class:on={chosen === s.id}>
          <input type="radio" name="scanner" value={s.id} bind:group={chosen} />
          <span class="text">
            <span class="name">{s.name}{#if !s.found}<span class="absent"> · not installed</span>{/if}</span>
            <span class="about">{s.about}</span>
          </span>
        </label>
      {/each}
    </div>

    {#if current?.note}<p class="hint">{current.note}</p>{/if}
    {#if scan}
      <div class="command">{scan.command}</div>
      <p class="hint">Runs in your shell (a new tab if it’s busy). The SARIF goes to {prettyPath(scan.output)}, not into the repository.</p>
    {/if}
  {/if}
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    {#if current && !current.found}
      <button class="secondary" onclick={install}>Install {current.name}…</button>
    {/if}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={run} disabled={!scan || !current?.found}>Run Scan</button>
  {/snippet}
</Dialog>

<style>
  .scanners {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 10px;
  }
  .scanner {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 8px;
    cursor: default;
  }
  .scanner:hover {
    background: var(--tab-hover);
  }
  .scanner.on {
    background: var(--accent-soft);
  }
  .scanner input {
    margin: 2px 0 0;
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .name {
    font-size: 12.5px;
    font-weight: 600;
  }
  .absent {
    color: var(--text-muted, var(--chrome-muted));
    font-weight: 500;
  }
  .about {
    color: var(--text-muted, var(--chrome-muted));
    font-size: 11.5px;
  }
</style>
