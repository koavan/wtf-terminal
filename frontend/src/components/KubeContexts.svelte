<script lang="ts">
  import { untrack } from 'svelte';
  import { Clipboard, Dialogs } from '@wailsio/runtime';
  import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
  import ToolHint from './ToolHint.svelte';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { authLabel, clusterLabel, contextKey, kubeConfigs, useContext, type KubeContext, type Op } from '../lib/kubeconfigs.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { shellQuote } from '../lib/shell';
  import { sidebar } from '../lib/sidebar.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // The contexts of the focused shell's kubeconfig files. Reading them is
  // local; switching affects only the focused shell; edits are planned,
  // confirmed and backed up; only Test Connection reaches a cluster.

  let filter = $state('');

  // The focused shell's KUBECONFIG and PATH decide what's listed (and
  // whether login helpers are found), so follow it.
  $effect(() => {
    void tabs.activePane?.env;
    untrack(() => kubeConfigs.load());
  });

  const inUse = $derived(tabs.activePane?.infra?.kubeContext ?? '');
  /** The shell uses a context: same name, from a file it reads. */
  const isInUse = (c: KubeContext) => c.active && c.name === inUse;
  const shown = $derived(
    kubeConfigs.contexts.filter((c) => {
      const q = filter.trim().toLowerCase();
      return !q || `${c.name} ${c.cluster} ${c.server} ${c.namespace} ${c.eksCluster}`.toLowerCase().includes(q);
    }),
  );
  /** The shell's files first (as kubectl merges them), then each other kubeconfig in ~/.kube. */
  const sections = $derived.by(() => {
    const out: { title: string; hint: string; contexts: KubeContext[] }[] = [];
    const active = shown.filter((c) => c.active);
    if (active.length) {
      out.push({
        title: `Used by this shell · ${kubeConfigs.files.map(prettyPath).join(', ')}`,
        hint: 'The files kubectl reads in the focused shell (its KUBECONFIG, or ~/.kube/config)',
        contexts: active,
      });
    }
    for (const f of kubeConfigs.otherFiles) {
      const contexts = shown.filter((c) => !c.active && c.file === f);
      if (contexts.length) {
        out.push({ title: prettyPath(f), hint: `${f}\nNot read by the focused shell; using one of its contexts points the shell at this file only`, contexts });
      }
    }
    return out;
  });

  const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

  async function use(c: KubeContext, newTab = false) {
    try {
      const where = await useContext(c, newTab);
      toast.show(where === 'here' ? `This shell now uses ${c.name}` : `Opened a tab using ${c.name}`);
    } catch (e) {
      toast.show(message(e), 6000);
    }
  }

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  /** Plans an edit, shows the plan, applies it on confirmation. */
  async function confirmEdit(op: Op, title: string, label: string, note = '') {
    let steps: string[];
    try {
      steps = await kubeConfigs.plan(op);
    } catch (e) {
      toast.show(message(e), 6000);
      return;
    }
    overlay.confirm({
      title,
      message: `wtf will make these changes${note}. A copy of the kubeconfig is kept first (kube-backups).`,
      items: steps,
      showAll: true,
      confirmLabel: label,
      onConfirm: () =>
        kubeConfigs
          .applyEdit(op)
          .then(() => toast.show('Kubeconfig updated'))
          .catch((e) => toast.show(message(e), 6000)),
    });
  }

  /** A one-field edit (rename, namespace): the prompt shows the exact command and is the confirmation. */
  function prompt(c: KubeContext, kind: 'rename' | 'namespace') {
    const rename = kind === 'rename';
    overlay.show({
      kind: 'commandPrompt',
      title: rename ? `Rename ${c.name}` : `Default Namespace of ${c.name}`,
      message: rename
        ? `In ${prettyPath(c.file)}. Shells using it by name keep the old name until they switch again.`
        : `Commands in this context use it unless they name another (-n). Now: ${c.namespace || 'default'}.`,
      fields: [{ label: rename ? 'New name' : 'Namespace', value: rename ? c.name : c.namespace, placeholder: rename ? '' : 'default' }],
      command: ([v]) => {
        const value = v.trim();
        if (!value) throw new Error(rename ? 'Enter a name' : 'Enter a namespace');
        if (rename && value === c.name) throw new Error('That’s its name now');
        if (!rename && value === c.namespace) throw new Error('That’s its namespace now');
        return rename
          ? `kubectl config rename-context ${shellQuote(c.name)} ${shellQuote(value)}`
          : `kubectl config set-context ${shellQuote(c.name)} --namespace=${shellQuote(value)}`;
      },
      confirmLabel: rename ? 'Rename' : 'Set Namespace',
      note: 'wtf runs this, after keeping a copy of the kubeconfig.',
      submit: ([v]) => kubeConfigs.applyEdit({ kind, name: c.name, value: v.trim(), file: c.file }),
    });
  }

  async function merge() {
    let path: string;
    try {
      path = await Dialogs.OpenFile({ Title: 'Merge a Kubeconfig', ButtonText: 'Merge…', CanChooseFiles: true, ShowHiddenFiles: true });
    } catch (e) {
      toast.show(message(e), 6000);
      return;
    }
    if (!path) return;
    confirmEdit({ kind: 'merge', name: '', value: path, file: '' }, `Merge ${path.slice(path.lastIndexOf('/') + 1)}?`, 'Merge', ' to your kubeconfig');
  }

  function addMenu(e: MouseEvent) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    overlay.openMenu(r.left, r.bottom + 4, [
      { label: 'Add EKS Cluster…', action: () => overlay.show({ kind: 'eksAdd' }) },
      { label: 'Merge a Kubeconfig File…', action: merge },
    ]);
  }

  function menu(e: MouseEvent, c: KubeContext) {
    e.preventDefault();
    const entries: MenuEntry[] = [
      { label: 'Use in This Shell', hint: 'double-click', action: () => use(c) },
      { label: 'Open New Tab with Context', action: () => use(c, true) },
      { label: 'Show Its Resources', action: () => use(c).then(() => (sidebar.kubeView = 'cluster')) },
      'separator',
      { label: 'Test Connection', action: () => kubeConfigs.test(c) },
    ];
    if (c.execCommand.endsWith('aws-iam-authenticator')) {
      entries.push({
        label: 'Log In with aws eks get-token…',
        action: () => confirmEdit({ kind: 'fixAuth', name: c.name, value: '', file: c.file }, `Switch ${c.name} to aws eks get-token?`, 'Switch', ', so it needs only the aws CLI'),
      });
    }
    entries.push(
      'separator',
      { label: 'Set Default Namespace…', action: () => prompt(c, 'namespace') },
      { label: 'Rename…', action: () => prompt(c, 'rename') },
      'separator',
      { label: 'Copy Name', action: () => copy(c.name, 'context name') },
    );
    if (c.server) entries.push({ label: 'Copy Server URL', action: () => copy(c.server, 'server URL') });
    entries.push(
      { label: isMac ? 'Reveal Kubeconfig in Finder' : 'Show Kubeconfig in Folder', action: () => Files.Reveal(c.file) },
      'separator',
      { label: 'Delete…', danger: true, action: () => confirmEdit({ kind: 'delete', name: c.name, value: '', file: c.file }, `Delete ${c.name}?`, 'Delete Context') },
    );
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  /** Facts that keep a context from working. */
  function issues(c: KubeContext): string[] {
    const out = [...c.problems];
    if (c.auth === 'exec' && !c.pluginFound) {
      out.push(`${c.execCommand} isn’t on this shell’s PATH`);
    }
    return out;
  }

  function detail(c: KubeContext): string {
    return [clusterLabel(c), c.namespace && `ns ${c.namespace}`, authLabel(c), c.awsProfile && `profile ${c.awsProfile}`].filter(Boolean).join(' · ');
  }
</script>

<div class="contexts">
  <header>
    <div class="filter">
      <Icon name="search" size={12} stroke={2} />
      <input bind:value={filter} placeholder="Filter contexts" spellcheck="false" autocomplete="off" />
    </div>
    <button class="tool" title="Add a cluster" aria-label="Add a cluster" onclick={addMenu}><Icon name="plus" size={14} /></button>
    <button class="tool" title="Reload" aria-label="Reload" onclick={() => kubeConfigs.load()}><Icon name="refresh" size={13} /></button>
  </header>

  <div class="list">
    {#each sections as s (s.title)}
      <div class="section" title={s.hint}>{s.title}</div>
      {#each s.contexts as c (contextKey(c))}
        {@const check = kubeConfigs.checks.get(contextKey(c))}
        <div
          class="row"
          role="listitem"
          title="{c.active ? '' : `From ${prettyPath(c.file)}\n`}Cluster: {c.cluster}{c.server ? ` (${c.server})` : ''}\nUser: {c.user}{c.execCommand ? `\nLogs in with: ${c.execCommand} ${c.execArgs.join(' ')}` : ''}{c.execEnv.length ? `\nWith: ${c.execEnv.join(', ')}` : ''}"
          oncontextmenu={(e) => menu(e, c)}
          ondblclick={() => use(c)}
        >
          <span class="icon" class:on={isInUse(c)}><Icon name="wheel" size={13} /></span>
          <span class="main">
            <span class="name">
              {c.name}
              {#if isInUse(c)}<span class="pill on" title="The focused shell uses this context">in use</span>{/if}
              {#if c.current}<span class="pill" title="The kubeconfig's current-context: what new shells use">default</span>{/if}
            </span>
            <span class="sub">{detail(c)}</span>
            {#each issues(c) as issue (issue)}<span class="issue">{issue}</span>{/each}
            {#if check?.busy}<span class="sub">Asking the cluster…</span>{/if}
            {#if check?.result}
              <span class="ok">Connected · {check.result.version}{check.result.username ? ` · as ${check.result.username}` : ''}</span>
            {:else if check?.error}
              <span class="issue">{check.error}<ToolHint error={check.error} /></span>
            {/if}
          </span>
          <button
            class="use"
            title="Use in this shell"
            aria-label="Use {c.name}"
            onclick={(e) => {
              e.stopPropagation();
              use(c);
            }}
            ondblclick={(e) => e.stopPropagation()}
          >
            <Icon name="play" size={11} stroke={2} />
          </button>
        </div>
      {/each}
    {:else}
      {#if kubeConfigs.loaded}
        <div class="note">
          {#if filter}
            No contexts match “{filter}”.
          {:else}
            <strong>No Kubernetes contexts</strong>
            Contexts from your kubeconfig (<code>$KUBECONFIG</code> or <code>~/.kube/config</code>) show here. Press + to add an EKS cluster or merge a kubeconfig file.
          {/if}
        </div>
      {/if}
    {/each}
    {#each kubeConfigs.problems as p (p)}<div class="problem">{p}</div>{/each}
  </div>
</div>

<style>
  .contexts {
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
    padding: 0 6px 4px 8px;
  }
  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 0;
    height: 26px;
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
  .tool {
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
  .tool:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }
  .section {
    padding: 10px 8px 4px;
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 34px;
    padding: 3px 6px 3px 8px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .icon {
    display: grid;
    place-items: center;
    flex: none;
    align-self: flex-start;
    margin-top: 4px;
    color: var(--chrome-muted);
  }
  .icon.on {
    color: var(--accent);
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
    white-space: nowrap;
    font: 500 12px 'JetBrains Mono Variable', monospace;
  }
  .sub {
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .issue,
  .ok {
    font-size: 11px;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
  .issue {
    color: var(--danger);
  }
  .ok {
    color: var(--mark-ok);
  }
  .pill {
    padding: 0 5px;
    border-radius: 4px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
    font: 600 10px system-ui, sans-serif;
    line-height: 15px;
  }
  .pill.on {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .use {
    display: grid;
    place-items: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--accent);
    opacity: 0;
  }
  .row:hover .use {
    opacity: 1;
  }
  .use:hover {
    background: var(--accent-soft);
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
  .problem {
    padding: 8px;
    color: var(--danger);
    font-size: 11px;
  }
  code {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11px;
  }
</style>
