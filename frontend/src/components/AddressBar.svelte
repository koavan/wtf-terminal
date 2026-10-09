<script lang="ts">
  import Icon, { iconForProcess } from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { badges } from '../lib/infra';
  import { tabs } from '../lib/tabs.svelte';
  import { formatDuration } from '../lib/toast.svelte';

  const tab = $derived(tabs.active);
  const pane = $derived(tabs.activePane);
  const path = $derived(pane ? prettyPath(pane.cwd) : '');
  const group = $derived(tabs.group(tab?.groupId ?? null));
  const contexts = $derived(badges(pane?.infra));

  const KIND_LABEL = { kube: 'k8s', aws: 'aws', gcp: 'gcp', tf: 'tf' } as const;

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
            </span>
          {/each}
        </span>
      {/if}

      <!-- What the shell is doing, when shell integration reports it. -->
      {#if pane.running && pane.command}
        <span class="status running" title={pane.command}>
          <span class="pulse"></span>
          <span class="cmd">{pane.command}</span>
        </span>
      {:else if pane.lastCommand}
        {@const last = pane.lastCommand}
        <span
          class="status"
          class:fail={last.exitCode !== 0}
          title="{last.text} — {last.exitCode === 0 ? 'succeeded' : `exit code ${last.exitCode}`}"
        >
          <Icon name={last.exitCode === 0 ? 'check' : 'x'} size={11} stroke={2.5} />
          {#if last.exitCode !== 0}<span>exit {last.exitCode}</span>{/if}
          <span class="took">{formatDuration(last.durationMs)}</span>
        </span>
      {/if}
    {/if}
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
