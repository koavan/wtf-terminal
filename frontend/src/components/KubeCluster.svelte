<script lang="ts">
  import { untrack } from 'svelte';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import ToolHint from './ToolHint.svelte';
  import Icon from './Icon.svelte';
  import PausedNote from './PausedNote.svelte';
  import RefreshControls from './RefreshControls.svelte';
  import { shortClusterName } from '../lib/infra';
  import {
    age, commands as kc, runInShell, firstPort, isTableKind, kube, LAYOUT, MIN_POLL_MS, podTone, rowSummary, rowTone, SECTION_LABEL,
    TABLE_KINDS, type Deployment, type KubeSection, type Pod, type Row, type TableKind, type Target,
  } from '../lib/kube.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { kubectlEnv } from '../lib/sshAdmin.svelte';
  import { poll, sidebarInterval } from '../lib/poller';
  import { settings } from '../lib/settings.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // Deployments, pods and events of the focused shell's context, each
  // fetched only while its section is expanded (see lib/kube). Actions run
  // real kubectl commands in new tabs; the ones that change the cluster show
  // the exact command and wait for a yes.

  let filter = $state('');

  // Opening the tab, or the focused shell pointing elsewhere, only resolves
  // the context (kubeconfig, no cluster request). The poller then refreshes
  // the expanded sections, if any.
  $effect(() => {
    const s = kube.shell();
    void [s.context, s.namespace, s.env];
    untrack(() => kube.resolve());
  });
  $effect(() => {
    const ms = sidebarInterval(settings.config);
    // The first call is the resolve above; the poller only repeats.
    let first = true;
    return untrack(() =>
      poll(() => {
        if (!first) kube.refreshOpen();
        first = false;
      }, ms && Math.max(ms, MIN_POLL_MS)),
    );
  });

  const t = $derived(kube.target);
  const match = (text: string) => {
    const q = filter.trim().toLowerCase();
    return !q || text.toLowerCase().includes(q);
  };
  const deployments = $derived((kube.deployments.items ?? []).filter((d) => match(d.name)));
  const pods = $derived((kube.pods.items ?? []).filter((p) => match(`${p.name} ${p.status} ${p.node}`)));
  const events = $derived((kube.events.items ?? []).filter((e) => match(`${e.object} ${e.reason} ${e.message}`)));
  const updatedAt = $derived(
    Math.max(0, ...[kube.deployments, kube.pods, kube.events].map((s) => s.updatedAt ?? 0)) || null,
  );

  /** How many rows a section shows, with the filter applied. */
  function countOf(section: KubeSection): number {
    if (section === 'deployments') return deployments.length;
    if (section === 'pods') return pods.length;
    if (section === 'events') return events.length;
    return (kube.tables[section].items ?? []).filter((r) => match(`${r.name} ${Object.values(r.cells).join(' ')}`)).length;
  }

  const run = (command: string) => tabs.open({ command });
  /** Runs in the focused shell itself when it's at a prompt (its own context and credentials), else a new tab. */
  const here = (build: (t: Target) => string) => {
    const ct = kube.commandTarget;
    if (ct) runInShell(tabs.activePane, ct, build);
  };

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  /** Where a command will run, as facts. */
  function where(ct: Target): string {
    const cluster = t?.cluster ? ` (cluster ${shortClusterName(t.cluster)})` : '';
    return `Context ${ct.context}${cluster}, namespace ${ct.namespace}.`;
  }

  /** Changes the cluster: shows the exact command, runs it in a tab on confirmation. */
  function confirmRun(title: string, label: string, command: string) {
    const ct = kube.commandTarget;
    if (!ct) return;
    overlay.confirm({
      title,
      message: `${where(ct)} This runs in a new tab:`,
      items: [command],
      showAll: true,
      confirmLabel: label,
      onConfirm: () => run(command),
    });
  }

  function podMenu(e: MouseEvent, p: Pod) {
    e.preventDefault();
    const ct = kube.commandTarget;
    if (!ct) return;
    const many = p.containers.length > 1;
    const entries: MenuEntry[] = [];
    if (many) {
      entries.push({ heading: 'Logs' });
      for (const c of p.containers) entries.push({ label: c.name, action: () => run(kc.logs(ct, p.name, c.name)) });
      entries.push({ heading: 'Shell' });
      for (const c of p.containers) entries.push({ label: c.name, action: () => here((t) => kc.exec(t, p.name, c.name)) });
    } else {
      entries.push(
        { label: 'Logs', hint: 'double-click', action: () => run(kc.logs(ct, p.name)) },
        { label: 'Shell (exec)', action: () => here((t) => kc.exec(t, p.name)) },
      );
    }
    for (const c of p.containers.filter((c) => c.restarts > 0)) {
      entries.push({
        label: many ? `Logs Before Last Restart: ${c.name}` : 'Logs Before Last Restart',
        action: () => run(kc.logs(ct, p.name, many ? c.name : undefined, true)),
      });
    }
    entries.push(
      'separator',
      { label: 'Describe', action: () => here((t) => kc.describe(t, 'pod', p.name)) },
      { label: 'Port Forward…', action: () => portForward(`pod/${p.name}`) },
      'separator',
      { label: 'Copy Name', action: () => copy(p.name, 'pod name') },
    );
    if (p.ip) entries.push({ label: `Copy IP (${p.ip})`, action: () => copy(p.ip, 'pod IP') });
    entries.push('separator', {
      label: 'Delete Pod…',
      danger: true,
      action: () => confirmRun(`Delete pod ${p.name}?`, 'Delete Pod', kc.deletePod(ct, p.name)),
    });
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  function deploymentMenu(e: MouseEvent, d: Deployment) {
    e.preventDefault();
    const ct = kube.commandTarget;
    if (!ct) return;
    overlay.openMenu(e.clientX, e.clientY, [
      { label: kube.deployment?.name === d.name ? 'Show All Pods' : 'Show Its Pods', hint: 'click', action: () => kube.selectDeployment(d) },
      { label: 'Logs (all pods)', action: () => run(kc.workloadLogs(ct, 'deployment', d.name)) },
      { label: 'Describe', action: () => here((t) => kc.describe(t, 'deployment', d.name)) },
      { label: 'View YAML', action: () => run(kc.yaml(ct, 'deployment', d.name)) },
      { label: 'Rollout Status', action: () => run(kc.rolloutStatus(ct, 'deployment', d.name)) },
      { label: 'Rollout History', action: () => run(kc.rolloutHistory(ct, 'deployment', d.name)) },
      { label: 'Port Forward…', action: () => portForward(`deployment/${d.name}`) },
      'separator',
      { label: 'Restart…', action: () => confirmRun(`Restart ${d.name}?`, 'Restart', kc.restart(ct, 'deployment', d.name)) },
      { label: 'Scale…', action: () => scale('deployment', d.name, d.desired) },
      'separator',
      { label: 'Copy Name', action: () => copy(d.name, 'deployment name') },
    ]);
  }

  function scale(kind: string, name: string, current: number) {
    const ct = kube.commandTarget;
    if (!ct) return;
    overlay.show({
      kind: 'commandPrompt',
      title: `Scale ${name}`,
      message: `${where(ct)} It has ${current} ${current === 1 ? 'replica' : 'replicas'} now.`,
      fields: [{ label: 'Replicas', value: String(current) }],
      command: ([n]) => {
        const replicas = Number(n);
        if (!/^\d+$/.test(n.trim()) || replicas > 1000) throw new Error('Enter a number of replicas, 0 or more');
        if (replicas === current) throw new Error(`It already has ${current}`);
        return kc.scale(ct, kind, name, replicas);
      },
      confirmLabel: 'Scale',
    });
  }

  /** The menu of a row of a table kind: what fits that kind, plus describe and copy. */
  function rowMenu(e: MouseEvent, kind: TableKind, r: Row) {
    e.preventDefault();
    const ct = kube.commandTarget;
    if (!ct) return;
    const k = TABLE_KINDS[kind].kind;
    const entries: MenuEntry[] = [];
    switch (kind) {
      case 'statefulsets':
      case 'daemonsets':
        entries.push(
          { label: 'Logs (all pods)', action: () => run(kc.workloadLogs(ct, k, r.name)) },
          { label: 'Rollout Status', action: () => run(kc.rolloutStatus(ct, k, r.name)) },
          'separator',
          { label: 'Restart…', action: () => confirmRun(`Restart ${r.name}?`, 'Restart', kc.restart(ct, k, r.name)) },
        );
        if (kind === 'statefulsets') {
          const ready = /\/(\d+)$/.exec(r.cells.READY ?? '');
          entries.push({ label: 'Scale…', action: () => scale(k, r.name, ready ? +ready[1] : 0) });
        }
        entries.push('separator');
        break;
      case 'jobs':
        entries.push({ label: 'Logs', action: () => run(kc.workloadLogs(ct, k, r.name)) }, 'separator');
        break;
      case 'cronjobs':
        entries.push(
          {
            label: 'Run Now…',
            action: () => confirmRun(`Run ${r.name} now?`, 'Create Job', kc.triggerCronJob(ct, r.name)),
          },
          'separator',
        );
        break;
      case 'services':
        entries.push({ label: 'Port Forward…', action: () => portForward(`service/${r.name}`, firstPort(r.cells['PORT(S)'])) }, 'separator');
        break;
      case 'ingresses': {
        const hosts = (r.cells.HOSTS ?? '').split(',').filter((h) => h && h !== '*');
        for (const h of hosts) entries.push({ label: `Open https://${h}`, action: () => Browser.OpenURL(`https://${h}`) });
        if (hosts.length) entries.push('separator');
        break;
      }
    }
    if (kind === 'secrets') {
      entries.push(
        {
          label: 'View Decoded Values…',
          hint: 'double-click',
          action: () => overlay.show({ kind: 'secretView', name: r.name, context: ct.context, namespace: ct.namespace, env: ct.env }),
        },
        { label: 'Decode in Terminal', action: () => run(kc.decodeSecret(ct, r.name)) },
        'separator',
      );
    }
    entries.push({ label: 'Describe', action: () => here((t) => kc.describe(t, k, r.name)) });
    // A secret's YAML holds its values base64-encoded; the viewer above decodes them.
    if (kind !== 'secrets') entries.push({ label: 'View YAML', action: () => run(kc.yaml(ct, k, r.name)) });
    entries.push('separator', { label: 'Copy Name', action: () => copy(r.name, `${k} name`) });
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  function rowTooltip(r: Row): string {
    return Object.entries(r.cells)
      .filter(([, v]) => v && v !== '<none>')
      .map(([c, v]) => `${c}: ${v}`)
      .join('\n');
  }

  /** A port forward to a resource: saved and run in the background, or one-off in a terminal (the editor offers both). */
  function portForward(resource: string, remoteDefault = '') {
    const ct = kube.commandTarget;
    if (!ct) return;
    overlay.show({
      kind: 'tunnel',
      tunnel: null,
      host: null,
      prefill: {
        kind: 'kubectl',
        name: resource.slice(resource.indexOf('/') + 1),
        context: ct.context,
        namespace: ct.namespace,
        resource,
        listenPort: remoteDefault,
        destPort: remoteDefault,
        env: kubectlEnv(ct.env),
      },
    });
  }


  function podDetail(p: Pod): string {
    const parts = [p.status];
    if (p.total) parts.push(`${p.ready}/${p.total} ready`);
    if (p.restarts) {
      const last = p.containers.find((c) => c.lastReason)?.lastReason;
      parts.push(`${p.restarts} ${p.restarts === 1 ? 'restart' : 'restarts'}${last ? ` (${last}${p.lastRestart ? ` ${age(p.lastRestart)} ago` : ''})` : ''}`);
    }
    parts.push(age(p.created));
    return parts.join(' · ');
  }

  function podTooltip(p: Pod): string {
    const lines = [p.name, `Node: ${p.node || '—'}  IP: ${p.ip || '—'}`];
    if (p.owner) lines.push(`Owner: ${p.owner}`);
    for (const c of p.containers) {
      lines.push(`${c.name}: ${c.image}${c.ready ? '' : ` — ${c.reason || 'not ready'}`}${c.restarts ? `, ${c.restarts} restarts` : ''}`);
    }
    return lines.join('\n');
  }

  function onNamespace(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const v = input.value.trim();
    if (v && v !== t?.namespace) kube.pickNamespace(v);
    else input.value = t?.namespace ?? '';
  }
</script>

{#snippet heading(section: KubeSection, count: number | null)}
  {@const open = kube.open.has(section)}
  {@const label = SECTION_LABEL[section]}
  {@const data = kube.section(section)}
  <button
    class="fold"
    onclick={() => kube.toggle(section)}
    aria-expanded={open}
    title={(open ? `Collapse ${label}` : `Show ${label}`) + (isTableKind(section) && TABLE_KINDS[section].note ? `\n${TABLE_KINDS[section].note}` : '')}
  >
    <Icon name={open ? 'chevronDown' : 'chevron'} size={12} /> {label}
    {#if open && count !== null}<span class="count">{count}</span>{/if}
    {#if open && data.loading && data.items === null}<span class="count">…</span>{/if}
  </button>
{/snippet}

{#snippet sectionError(section: KubeSection)}
  {#if kube.section(section).error}<div class="empty err">{kube.section(section).error}<ToolHint error={kube.section(section).error} /></div>{/if}
{/snippet}

{#snippet deploymentsBody()}
  {#each deployments as d (d.name)}
    <div
      class="row"
      class:on={kube.deployment?.name === d.name}
      role="button"
      tabindex="-1"
      title="{d.images.join('\n')}{d.problems.length ? '\n' + d.problems.join('\n') : ''}"
      onclick={() => kube.selectDeployment(d)}
      oncontextmenu={(e) => deploymentMenu(e, d)}
      onkeydown={() => {}}
    >
      <span class="dot {d.ready === d.desired ? 'ok' : d.ready === 0 && d.desired > 0 ? 'bad' : 'wait'}"></span>
      <span class="main">
        <span class="name">{d.name}</span>
        <span class="sub">{d.ready}/{d.desired} ready{d.upToDate !== d.desired ? ` · ${d.upToDate} up to date` : ''} · {age(d.created)}</span>
        {#each d.problems as pr (pr)}<span class="problem-line">{pr}</span>{/each}
      </span>
    </div>
  {:else}
    {#if kube.deployments.items}<div class="empty">No deployments{filter ? ' match' : ''}</div>{/if}
  {/each}
{/snippet}

{#snippet podsBody()}
  {#each pods as p (p.name)}
    <div
      class="row"
      role="listitem"
      title={podTooltip(p)}
      oncontextmenu={(e) => podMenu(e, p)}
      ondblclick={() => kube.commandTarget && run(kc.logs(kube.commandTarget, p.name, p.containers.length > 1 ? p.containers[0].name : undefined))}
    >
      <span class="dot {podTone(p)}"></span>
      <span class="main">
        <span class="name">{p.name}</span>
        <span class="sub" class:bad={podTone(p) === 'bad'}>{podDetail(p)}</span>
      </span>
    </div>
  {:else}
    {#if kube.pods.items}<div class="empty">No pods{filter || kube.deployment ? ' match' : ''}</div>{/if}
  {/each}
{/snippet}

{#snippet eventsBody()}
  {#each events as ev, i (i)}
    <div class="event" class:warn={ev.type === 'Warning'} title="{ev.type} · {ev.last}">
      <span class="ev-head">{ev.reason} <span class="obj">{ev.object}</span>{ev.count > 1 ? ` ×${ev.count}` : ''} · {age(ev.last)}</span>
      <span class="ev-msg">{ev.message}</span>
    </div>
  {:else}
    {#if kube.events.items}<div class="empty">No recent events</div>{/if}
  {/each}
{/snippet}

{#snippet tableBody(kind: TableKind)}
  {@const rows = (kube.tables[kind].items ?? []).filter((r) => match(`${r.name} ${Object.values(r.cells).join(' ')}`))}
  {#each rows as r (r.name)}
    {@const tone = rowTone(kind, r)}
    <div
      class="row"
      role="listitem"
      title={rowTooltip(r)}
      oncontextmenu={(e) => rowMenu(e, kind, r)}
      ondblclick={() => kind === 'secrets' && kube.commandTarget &&
        overlay.show({ kind: 'secretView', name: r.name, ...kube.commandTarget })}
    >
      <span class="dot {tone ?? 'none'}"></span>
      <span class="main">
        <span class="name">{r.name}</span>
        <span class="sub" class:bad={tone === 'bad'}>{rowSummary(kind, r)}</span>
      </span>
    </div>
  {:else}
    {#if kube.tables[kind].items}<div class="empty">No {SECTION_LABEL[kind].toLowerCase()}{filter ? ' match' : ''}</div>{/if}
  {/each}
{/snippet}

<div class="kube">
  <header>
    <div class="target" title={t ? `Context: ${t.context}\nCluster: ${t.cluster}\nFollows the focused shell's context` : ''}>
      <Icon name="wheel" size={13} />
      <span class="ctx">{t ? shortClusterName(t.cluster || t.context) : kube.shell().context || 'Kubernetes'}</span>
    </div>
    <RefreshControls onrefresh={() => kube.resolve()} />
  </header>

  {#if t}
    <div class="ns">
      <span>Namespace</span>
      <input
        value={t.namespace}
        list="kube-namespaces"
        onfocus={() => kube.loadNamespaces()}
        onchange={onNamespace}
        onkeydown={(e) => e.key === 'Enter' && (e.currentTarget as HTMLInputElement).blur()}
        aria-label="Namespace"
        title="Shows another namespace here; the shell keeps its own"
        spellcheck="false"
        autocomplete="off"
      />
      <datalist id="kube-namespaces">
        {#each kube.namespaceList ?? [] as n (n)}<option value={n}>{n === kube.shell().namespace ? 'the shell’s' : ''}</option>{/each}
      </datalist>
    </div>
  {/if}
  {#if kube.open.size}<PausedNote {updatedAt} />{/if}

  {#if kube.open.size}
    <div class="filter">
      <Icon name="search" size={12} stroke={2} />
      <input bind:value={filter} placeholder="Filter by name, status, node" spellcheck="false" autocomplete="off" />
    </div>
  {/if}

  <div class="list">
    {#if kube.targetError}
      <div class="note">
        <strong>Can’t find a Kubernetes context</strong>
        <span class="err">{kube.targetError}<ToolHint error={kube.targetError} /></span>
      </div>
    {:else if t}
      {#each LAYOUT as g (g.group)}
        <div class="group">{g.group}</div>
        {#each g.sections as section (section)}
          {@const data = kube.section(section)}
          <h3>
            {@render heading(section, data.items ? countOf(section) : null)}
            {#if section === 'pods' && kube.deployment}
              <button class="chip" onclick={() => kube.selectDeployment(null)} title="Show all pods">{kube.deployment.name} ×</button>
            {/if}
          </h3>
          {#if kube.open.has(section)}
            {@render sectionError(section)}
            {#if section === 'deployments'}
              {@render deploymentsBody()}
            {:else if section === 'pods'}
              {@render podsBody()}
            {:else if section === 'events'}
              {@render eventsBody()}
            {:else if isTableKind(section)}
              {@render tableBody(section)}
            {/if}
          {/if}
        {/each}
      {/each}

      {#if !kube.open.size}
        <p class="hint">Open a section to load it. Only open sections are read from the cluster, and refreshed.</p>
      {/if}
    {/if}
  </div>
</div>

<style>
  .kube {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  header {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
    padding: 0 6px 4px 10px;
  }
  .target {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 0;
    color: var(--accent);
  }
  .ctx {
    overflow: hidden;
    color: var(--chrome-text);
    font: 600 12px 'JetBrains Mono Variable', monospace;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  header :global(.tool) {
    display: grid;
    place-items: center;
    flex: none;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
  }
  header :global(.tool:hover) {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .ns {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: none;
    margin: 0 8px 6px 10px;
    color: var(--chrome-muted);
    font-size: 11.5px;
  }
  .ns input {
    flex: 1;
    min-width: 0;
    height: 24px;
    padding: 0 6px;
    border: 1px solid var(--hairline);
    border-radius: 6px;
    background: var(--tab-hover);
    color: var(--chrome-text);
    font: 12px 'JetBrains Mono Variable', monospace;
  }
  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 26px;
    margin: 0 8px 4px;
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
  }
  .filter input:focus {
    outline: none;
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }
  h3 {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 10px 8px 4px;
    color: var(--chrome-muted);
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .group {
    margin: 14px 10px 0;
    color: var(--chrome-muted);
    font-size: 10px;
    font-weight: 600;
    opacity: 0.75;
  }
  .group:first-child {
    margin-top: 2px;
  }
  .count {
    font-weight: 500;
  }
  .chip {
    margin-left: auto;
    padding: 0 6px;
    border: 0;
    border-radius: 5px;
    background: var(--accent-soft);
    color: var(--accent);
    font: 600 10.5px system-ui, sans-serif;
    letter-spacing: 0;
    text-transform: none;
    line-height: 17px;
  }
  .fold {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 9px;
    min-height: 32px;
    padding: 3px 6px 3px 10px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .row.on {
    background: var(--accent-soft);
  }
  .row:focus {
    outline: none;
  }
  .dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--chrome-muted);
  }
  .dot.ok {
    background: var(--mark-ok);
  }
  .dot.wait {
    background: var(--g-yellow);
  }
  .dot.bad {
    background: var(--danger);
  }
  .dot.none {
    visibility: hidden;
  }
  .dot.done {
    opacity: 0.5;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .name {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font: 500 12px 'JetBrains Mono Variable', monospace;
  }
  .sub {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--chrome-muted);
    font-size: 11px;
  }
  .sub.bad,
  .problem-line,
  .err {
    color: var(--danger);
  }
  .problem-line {
    font-size: 11px;
    line-height: 1.35;
  }
  .event {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 4px 8px 4px 10px;
    border-left: 2px solid transparent;
    font-size: 11px;
  }
  .event.warn {
    border-left-color: var(--danger);
  }
  .ev-head {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .event.warn .ev-head {
    color: var(--danger);
  }
  .obj {
    color: var(--chrome-muted);
    font: 400 11px 'JetBrains Mono Variable', monospace;
  }
  .ev-msg {
    color: var(--chrome-muted);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
  .empty {
    padding: 4px 10px;
    color: var(--chrome-muted);
    font-size: 11.5px;
  }
  .note {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 8px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .hint {
    margin: 10px 10px 0;
    color: var(--chrome-muted);
    font-size: 11.5px;
    line-height: 1.45;
  }
</style>
