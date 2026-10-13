<script lang="ts">
  import { Service as ConfigService } from '../../bindings/github.com/dayanraj/wtf/internal/config';
  import Dialog from './Dialog.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { settings } from '../lib/settings.svelte';
  import { toast } from '../lib/toast.svelte';
  import { formatBytes, formatCPU, resourceUsage, SAMPLE_MS, wtfTotal, type Group } from '../lib/usage.svelte';

  // Resource Usage: wtf's CPU and memory, process by process, updated every
  // 2 seconds while the dialog is open (see lib/usage).

  $effect(() => resourceUsage.watch());

  const u = $derived(resourceUsage.current);
  const total = $derived(u ? wtfTotal(u) : null);
  const memoryNote = $derived(
    u?.memoryKind === 'rss'
      ? 'Memory is resident memory (RSS); pages shared between processes count in each.'
      : 'Memory is the footprint Activity Monitor shows.',
  );

  const sections = $derived(
    u
      ? ([
          { title: 'wtf', note: 'The app, and what it runs itself (tunnels, git and kubectl checks)', group: u.app },
          { title: 'Windows', note: isMac ? 'WebKit’s processes that draw wtf’s windows' : 'WebKitGTK’s processes that draw wtf’s windows', group: u.window },
          { title: 'Your programs', note: 'Shells and what runs in them — not counted in wtf’s total', group: u.programs },
        ] as { title: string; note: string; group: Group }[])
      : [],
  );

  function setShown(on: boolean) {
    ConfigService.SetShowUsage(on).catch((e) => toast.show(`Couldn't save the setting: ${e}`));
  }
</script>

<Dialog title="Resource Usage" subtitle="CPU is a percentage of one core ({u?.cores ?? '?'} cores = {(u?.cores ?? 0) * 100}%), over the last {SAMPLE_MS / 1000} seconds. {memoryNote}" width={560}>
  {#if resourceUsage.error}
    <div class="error" role="alert">{resourceUsage.error}</div>
  {:else if !u || !total}
    <p class="hint">Measuring…</p>
  {:else}
    <div class="total">
      <span class="label">wtf in all</span>
      <span class="num">{formatCPU(total.cpu)}</span>
      <span class="num">{formatBytes(total.memory)}</span>
    </div>
    {#if u.note}<p class="hint">{u.note}</p>{/if}

    {#each sections as s (s.title)}
      <div class="section">
        <div class="head" title={s.note}>
          <span class="label">{s.title}</span>
          <span class="num">{formatCPU(u.interval > 0 ? s.group.cpu : -1)}</span>
          <span class="num">{formatBytes(s.group.memory)}</span>
        </div>
        <div class="note">{s.note}</div>
        {#each s.group.procs as p (p.pid)}
          <div class="proc">
            <span class="name" title="{p.name} (pid {p.pid})">{p.name}<span class="pid">{p.pid}</span></span>
            <span class="num">{formatCPU(p.cpu)}</span>
            <span class="num">{formatBytes(p.memory)}</span>
          </div>
        {:else}
          <div class="proc empty">None</div>
        {/each}
      </div>
    {/each}
  {/if}

  {#snippet buttons()}
    <label class="check">
      <input type="checkbox" checked={settings.config?.showUsage ?? false} onchange={(e) => setShown(e.currentTarget.checked)} />
      Show in the address bar
    </label>
    <span class="spacer"></span>
    <button class="primary" onclick={() => overlay.close()}>Done</button>
  {/snippet}
</Dialog>

<style>
  .total,
  .head,
  .proc {
    display: grid;
    grid-template-columns: 1fr 64px 76px;
    align-items: baseline;
    gap: 8px;
  }
  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .total {
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--identity);
    font-size: 13px;
    font-weight: 700;
  }
  .section {
    margin-top: 12px;
  }
  .head {
    padding: 0 10px;
    font-size: 12.5px;
    font-weight: 600;
  }
  .note {
    padding: 0 10px 4px;
    color: var(--text-muted);
    font-size: 11px;
  }
  .proc {
    padding: 2px 10px 2px 18px;
    font-size: 12px;
  }
  .proc.empty {
    display: block;
    color: var(--text-muted);
  }
  .name {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11.5px;
  }
  .pid {
    margin-left: 6px;
    color: var(--text-muted);
  }
  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }
</style>
