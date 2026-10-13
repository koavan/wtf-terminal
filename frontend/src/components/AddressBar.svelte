<script lang="ts">
  import Icon, { iconForProcess } from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { badges } from '../lib/infra';
  import { isMac } from '../lib/platform';
  import { recordings } from '../lib/recording.svelte';
  import { redaction } from '../lib/redaction.svelte';
  import { settings } from '../lib/settings.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import UsageReadout from './UsageReadout.svelte';
  import { formatDuration } from '../lib/toast.svelte';

  const tab = $derived(tabs.active);
  const pane = $derived(tabs.activePane);
  const path = $derived(pane ? prettyPath(pane.cwd) : '');
  const group = $derived(tabs.group(tab?.groupId ?? null));
  /** Ticks every 15 s while the shell has credentials that expire, for their clocks. */
  let clockNow = $state(Date.now());
  const expiring = $derived(!!pane?.infra?.expiries?.length);
  $effect(() => {
    if (!expiring) return;
    clockNow = Date.now();
    const timer = setInterval(() => (clockNow = Date.now()), 15_000);
    return () => clearInterval(timer);
  });
  const contexts = $derived(badges(pane?.infra, clockNow));

  const recording = $derived(pane ? recordings.of(pane.id) : undefined);
  /** Ticks once a second while the pane is recorded, for the elapsed time. */
  let now = $state(Date.now());
  $effect(() => {
    if (!recording) return;
    now = Date.now();
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });
  const elapsed = (ms: number) => {
    const s = Math.max(0, Math.floor(ms / 1000));
    const p = (n: number) => String(n).padStart(2, '0');
    return s >= 3600 ? `${Math.floor(s / 3600)}:${p(Math.floor(s / 60) % 60)}:${p(s % 60)}` : `${Math.floor(s / 60)}:${p(s % 60)}`;
  };

  const KIND_LABEL = { kube: 'k8s', aws: 'aws', gcp: 'gcp', tf: 'tf', git: 'git', ssh: 'ssh' } as const;

  /** "~/src/wtf" → ["~", "src", "wtf"], "/etc/ssh" → ["/", "etc", "ssh"] */
  const crumbs = $derived.by(() => {
    if (!path) return [];
    const parts = path.split('/').filter(Boolean);
    return path.startsWith('/') ? ['/', ...parts] : parts;
  });
</script>

<div class="address">
  <div class="omnibox" title={pane?.cwd}>
    {#if tab && pane}
      {#if group}
        <span class="group" style:--group-color="var(--g-{group.color})">
          <span class="dot"></span>{group.name || 'Group'}
        </span>
      {/if}
      <span class="identity" class:running={!pane.isShell}>
        <Icon name={iconForProcess(pane.process, pane.isShell)} size={12} stroke={2} />
        <span>{pane.process || 'shell'}</span>
      </span>
      <span class="path">
        {#each crumbs as crumb, i (i)}
          {#if i > 0 && crumbs[i - 1] !== '/'}
            <span class="sep"><Icon name="chevron" size={11} stroke={2} /></span>
          {/if}
          <span class="crumb" class:last={i === crumbs.length - 1}>{crumb}</span>
        {/each}
      </span>

      {#if contexts.length}
        <span class="contexts">
          {#each contexts as b (b.kind)}
            <span class="ctx" title={b.title}>
              <span class="kind">{KIND_LABEL[b.kind]}</span>
              <span class="value">{b.label}</span>
              {#if b.clock}
                <span class="clock {b.clock.state}">
                  <Icon name="clock" size={10} stroke={2.4} />{b.clock.label}
                </span>
              {/if}
            </span>
          {/each}
        </span>
      {/if}

      <!-- What the shell is doing, when shell integration reports it. -->
      {#if pane.running && pane.command}
        {@const command = redaction.text(pane.command)}
        <span class="status running" title={command}>
          <span class="pulse"></span>
          <span class="cmd">{command}</span>
        </span>
      {:else if pane.lastCommand}
        {@const last = pane.lastCommand}
        <span
          class="status"
          class:fail={last.exitCode !== 0}
          title="{redaction.text(last.text)} — {last.exitCode === 0 ? 'succeeded' : `exit code ${last.exitCode}`}"
        >
          <Icon name={last.exitCode === 0 ? 'check' : 'x'} size={11} stroke={2.5} />
          {#if last.exitCode !== 0}<span>exit {last.exitCode}</span>{/if}
          <span class="took">{formatDuration(last.durationMs)}</span>
        </span>
      {/if}
    {/if}
    <span class="end">
    {#if pane && recording}
      <button
        class="rec"
        title="Recording to {prettyPath(recording.path)}. Click to stop ({isMac ? '⌥⌘S' : 'Ctrl+Shift+Alt+M'})"
        onclick={() => recordings.stop(pane)}
      >
        <span class="dot"></span>
        <span>REC {elapsed(now - recording.since)}</span>
      </button>
    {/if}
    {#if redaction.on}
      <!-- Always in view while on, so it's never on (or off) by surprise. -->
      <button
        class="secrets"
        title="Secrets in the output are covered, and replaced with [REDACTED] when copied. Click to show them ({isMac ? '⌥⌘R' : 'Ctrl+Shift+Alt+X'})"
        onclick={() => (redaction.on = false)}
      >
        <Icon name="shield" size={12} stroke={2.2} />
        <span>Secrets hidden</span>
      </button>
    {/if}
    {#if settings.config?.showUsage}<UsageReadout />{/if}
    </span>
  </div>
</div>

<style>
  .address {
    display: flex;
    align-items: center;
    height: var(--address-height);
    padding: 0 8px;
    border-bottom: 1px solid var(--hairline);
  }

  .omnibox {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-width: 0;
    height: 28px;
    padding: 0 10px 0 4px;
    border-radius: 8px;
    background: var(--omnibox);
    font-size: 12.5px;
    color: var(--text-muted);
  }

  .end {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    margin-left: auto;
  }
  .end:empty {
    display: none;
  }

  .secrets {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: none;
    height: 20px;
    padding: 0 8px 0 7px;
    border: 0;
    border-radius: 6px;
    background: color-mix(in srgb, var(--g-green) 22%, transparent);
    color: var(--text);
    font: inherit;
    font-size: 11.5px;
    font-weight: 600;
    white-space: nowrap;
  }
  .rec {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 20px;
    padding: 0 8px 0 7px;
    border: 0;
    border-radius: 6px;
    background: color-mix(in srgb, var(--danger) 18%, transparent);
    color: var(--text);
    font: inherit;
    font-size: 11.5px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .rec .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--danger);
    animation: rec 1.6s ease-in-out infinite;
  }
  @keyframes rec {
    50% {
      opacity: 0.35;
    }
  }
  /* Both labels: the second one follows the first instead of pushing right. */
  .rec + .secrets {
    margin-left: 0;
  }
  .secrets :global(svg) {
    color: var(--g-green);
  }

  .identity {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: none;
    height: 20px;
    padding: 0 8px 0 7px;
    border-radius: 6px;
    background: var(--identity);
    color: var(--text);
    font-size: 11.5px;
    font-weight: 600;
  }
  .identity.running {
    background: var(--accent-soft);
    color: var(--accent);
  }

  .group {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    max-width: 160px;
    height: 20px;
    margin-left: 4px;
    margin-right: -4px;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--text);
    font-size: 11.5px;
    font-weight: 600;
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--group-color);
  }

  .path {
    display: flex;
    align-items: center;
    gap: 3px;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    mask-image: linear-gradient(to right, #000 calc(100% - 24px), transparent);
  }
  .contexts {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 0 1 auto;
    min-width: 0;
    margin-left: auto;
    overflow: hidden;
  }
  /* The command status sits right after the badges, not pushed apart. */
  .contexts + .status {
    margin-left: 0;
  }
  .ctx {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: 0 1 auto;
    min-width: 0;
    height: 20px;
    padding: 0 8px 0 6px;
    border-radius: 6px;
    background: var(--identity);
    color: var(--text);
    font-size: 11.5px;
    font-weight: 500;
    white-space: nowrap;
  }
  .ctx .kind {
    flex: none;
    color: var(--text-muted);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
  .ctx .value {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .clock {
    display: flex;
    align-items: center;
    gap: 3px;
    flex: none;
    margin-right: -4px;
    padding: 0 5px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--text-muted) 14%, transparent);
    color: var(--text-muted);
    font-size: 10.5px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .clock.soon {
    background: color-mix(in srgb, var(--g-yellow) 26%, transparent);
    color: var(--text);
  }
  .clock.expired {
    background: color-mix(in srgb, var(--danger) 18%, transparent);
    color: var(--danger);
  }

  .status {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: none;
    max-width: 40%;
    height: 20px;
    margin-left: auto;
    padding: 0 8px;
    border-radius: 6px;
    background: var(--identity);
    color: var(--mark-ok);
    font-size: 11.5px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .status.fail {
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
  }
  .status.running {
    color: var(--accent);
    background: var(--accent-soft);
  }
  .took {
    color: var(--text-muted);
    font-weight: 500;
  }
  .cmd {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-family: 'JetBrains Mono Variable', monospace;
    font-weight: 500;
  }
  .pulse {
    flex: none;
    width: 6px;
    height: 6px;
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

  .sep {
    display: grid;
    place-items: center;
    opacity: 0.45;
  }
  .crumb.last {
    color: var(--text);
    font-weight: 500;
  }
</style>
