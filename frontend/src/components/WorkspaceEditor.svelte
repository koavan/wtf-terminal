<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { fieldsIn, snippets, type Preview, type Workspace, type WorkspaceDef } from '../lib/snippets.svelte';
  import { GROUP_COLORS } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // Adds or edits a workspace with a form: its tabs (names, groups, panes,
  // folders, startup commands), placeholders, and where it opens. Review
  // shows the exact block and file; only Confirm writes, and an edit
  // replaces that workspace's block alone.

  let { workspace, draft: given, duplicate = false }: { workspace: Workspace | null; draft?: WorkspaceDef; duplicate?: boolean } = $props();

  const original = untrack(() => workspace);
  const editing = !!original && !untrack(() => duplicate);
  const start = untrack(() => given);

  interface PaneDraft {
    cwd: string;
    command: string;
  }
  interface TabDraft {
    title: string;
    group: string;
    color: string;
    layout: 'row' | 'column';
    panes: PaneDraft[];
  }
  interface ParamDraft {
    description: string;
    default: string;
    choices: string;
    raw: boolean;
  }

  const source = original ?? start;
  let name = $state(original ? (editing ? original.name : `${original.name} (copy)`) : (start?.name ?? ''));
  let description = $state(source?.description ?? '');
  let newWindow = $state(source?.window === 'new');
  let tabList = $state<TabDraft[]>(
    (source?.tabs ?? [{ title: '', group: '', color: '', cwd: '', command: '', layout: '', panes: [] }]).map((t) => ({
      title: t.title,
      group: t.group,
      color: t.color,
      layout: t.layout === 'column' ? 'column' : 'row',
      panes: (t.panes ?? []).length ? (t.panes ?? []).map((p) => ({ ...p })) : [{ cwd: t.cwd, command: t.command }],
    })),
  );
  let params = $state<Record<string, ParamDraft>>(
    Object.fromEntries(
      (source?.params ?? []).map((p) => [p.name, { description: p.description, default: p.default, choices: (p.choices ?? []).join(', '), raw: p.raw }]),
    ),
  );
  let file = $state(editing ? original!.source : '');
  let preview = $state<Preview | null>(null);
  let error = $state('');
  let busy = $state(false);
  let nameInput = $state<HTMLInputElement>();

  onMount(async () => {
    if (!snippets.loaded || !editing) await snippets.load();
    if (!file) file = (snippets.targets.find((t) => t.kind === 'user') ?? snippets.targets[0])?.path ?? '';
    nameInput?.focus();
  });

  const placeholders = $derived(
    fieldsIn(
      tabList.flatMap((t) => [t.title, t.group, ...t.panes.flatMap((p) => [p.cwd, p.command])]),
      [],
    ),
  );
  $effect(() => {
    for (const p of placeholders) if (!params[p.name]) params[p.name] = { description: '', default: '', choices: '', raw: false };
  });

  const edit = $derived({
    kind: 'workspace',
    file,
    index: editing ? original!.index : -1,
    version: snippets.versions[file] ?? '',
    snippet: { name: '', description: '', tags: [], command: '', steps: [], params: [] },
    workspace: {
      name,
      description,
      window: newWindow ? 'new' : '',
      tabs: tabList.map((t) => ({ title: t.title, group: t.group, color: t.color, cwd: '', command: '', layout: t.layout, panes: t.panes })),
      params: placeholders.map((p) => ({
        name: p.name,
        description: params[p.name]?.description ?? '',
        default: params[p.name]?.default ?? '',
        choices: (params[p.name]?.choices ?? '').split(',').map((c) => c.trim()).filter(Boolean),
        raw: params[p.name]?.raw ?? false,
      })),
    },
  });

  const targetLabel = (path: string) => {
    const t = snippets.targets.find((x) => x.path === path);
    const kind = t?.kind === 'project' ? 'this project’s, for git' : t?.kind === 'team' ? 'team file' : 'yours';
    return `${prettyPath(path)} — ${kind}${t && !t.exists ? ' (new file)' : ''}`;
  };

  async function review() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      preview = await snippets.plan(edit);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function save() {
    if (busy) return;
    busy = true;
    try {
      await snippets.save(edit);
      overlay.close();
      toast.show(editing ? `Saved “${name.trim()}”` : `Added “${name.trim()}” — open it from the Workspaces tab or Open Workspace…`);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      preview = null;
    } finally {
      busy = false;
    }
  }

  function moveTab(i: number, by: number) {
    const j = i + by;
    if (j < 0 || j >= tabList.length) return;
    [tabList[i], tabList[j]] = [tabList[j], tabList[i]];
  }

  const newTab = (): TabDraft => ({ title: '', group: tabList.at(-1)?.group ?? '', color: '', layout: 'row', panes: [{ cwd: '', command: '' }] });
</script>

<Dialog
  title={preview ? 'Review' : editing ? `Edit “${original!.name}”` : 'New Workspace'}
  subtitle={preview
    ? `${preview.newFile ? 'Creates' : editing ? 'Replaces this workspace in' : 'Adds to the end of'} ${prettyPath(preview.file)}. Nothing is written until you confirm.`
    : 'Tabs — with folders, split panes, groups and startup commands — opened in one go'}
  width={680}
  onsubmit={() => (preview ? save() : review())}
>
  {#if preview}
    <pre class="command toml">{preview.text}</pre>
    {#if editing}<p class="hint">The rest of the file — snippets, other workspaces, your comments — stays as it is.</p>{/if}
  {:else}
    <div class="row">
      <label class="field grow">
        <span>Name</span>
        <input bind:this={nameInput} bind:value={name} placeholder="payments on call" autocomplete="off" />
      </label>
      <label class="field grow">
        <span>Description</span>
        <input bind:value={description} placeholder="optional" autocomplete="off" />
      </label>
    </div>

    <div class="tabs">
      {#each tabList as t, i (i)}
        <div class="card">
          <div class="card-head">
            <span class="num">{i + 1}</span>
            <input class="title" bind:value={t.title} placeholder="Tab name (optional)" autocomplete="off" />
            <input class="group" bind:value={t.group} placeholder="Group (optional)" autocomplete="off" />
            <select class="color" bind:value={t.color} aria-label="Group color of tab {i + 1}" disabled={!t.group.trim()}>
              <option value="">color</option>
              {#each GROUP_COLORS as c (c)}<option value={c}>{c}</option>{/each}
            </select>
            <button class="icon" aria-label="Move tab {i + 1} up" disabled={i === 0} onclick={() => moveTab(i, -1)}><Icon name="chevronUp" size={12} /></button>
            <button class="icon" aria-label="Move tab {i + 1} down" disabled={i === tabList.length - 1} onclick={() => moveTab(i, 1)}><Icon name="chevronDown" size={12} /></button>
            <button class="icon" aria-label="Remove tab {i + 1}" disabled={tabList.length === 1} onclick={() => tabList.splice(i, 1)}><Icon name="x" size={12} /></button>
          </div>
          {#each t.panes as p, j (j)}
            <div class="pane">
              <input class="cwd" bind:value={p.cwd} placeholder="folder (e.g. ~/src/app)" spellcheck="false" autocomplete="off" />
              <input class="cmd" bind:value={p.command} placeholder="startup command (optional)" spellcheck="false" autocomplete="off" />
              {#if t.panes.length > 1}
                <button class="icon" aria-label="Remove pane {j + 1} of tab {i + 1}" onclick={() => t.panes.splice(j, 1)}><Icon name="x" size={12} /></button>
              {/if}
            </div>
          {/each}
          <div class="card-foot">
            <button class="secondary small" onclick={() => t.panes.push({ cwd: t.panes.at(-1)?.cwd ?? '', command: '' })}><Icon name="plus" size={12} /> Split</button>
            {#if t.panes.length > 1}
              <select class="layout" bind:value={t.layout} aria-label="Layout of tab {i + 1}">
                <option value="row">side by side</option>
                <option value="column">stacked</option>
              </select>
            {/if}
          </div>
        </div>
      {/each}
      <button class="secondary small" onclick={() => tabList.push(newTab())}><Icon name="plus" size={12} /> Add Tab</button>
    </div>

    <small class="syntax">
      Names, groups, folders and commands can use placeholders, asked for when it opens: <code>{'{{service}}'}</code>, or
      <code>{'{{ns=$kube_namespace}}'}</code>.
    </small>

    {#if placeholders.length}
      <div class="params">
        <div class="params-head">Placeholders</div>
        {#each placeholders as p (p.name)}
          {@const d = params[p.name]}
          {#if d}
            <div class="param">
              <code class="pname">{p.name}</code>
              <input bind:value={d.description} placeholder="what to enter" autocomplete="off" />
              <input bind:value={d.default} placeholder="default" spellcheck="false" autocomplete="off" />
              <input bind:value={d.choices} placeholder="choices, comma-separated" spellcheck="false" autocomplete="off" />
            </div>
          {/if}
        {/each}
      </div>
    {/if}

    <label class="check"><input type="checkbox" bind:checked={newWindow} /> Open in a new window (otherwise as new tabs in the current one)</label>

    {#if !editing}
      <label class="field">
        <span>Save to</span>
        <select bind:value={file}>
          {#each snippets.targets as tg (tg.path)}<option value={tg.path}>{targetLabel(tg.path)}</option>{/each}
        </select>
      </label>
    {/if}
  {/if}

  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    {#if preview}
      <button class="secondary" onclick={() => (preview = null)} disabled={busy}>Back</button>
      <button class="primary" onclick={save} disabled={busy}>Confirm &amp; Save</button>
    {:else}
      <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
      <button class="primary" onclick={review} disabled={busy || !name.trim() || !file}>Review…</button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .tabs {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 46vh;
    overflow: auto;
    margin-bottom: 8px;
    padding-right: 2px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 8px;
    border: 1px solid var(--hairline);
    border-radius: 9px;
  }
  .card-head,
  .pane,
  .card-foot {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .num {
    display: grid;
    place-items: center;
    flex: none;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--identity);
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 700;
  }
  .title {
    flex: 2;
    min-width: 0;
  }
  .group {
    flex: 1.4;
    min-width: 0;
  }
  .color {
    flex: none;
    width: 86px !important;
  }
  .pane {
    padding-left: 26px;
  }
  .cwd {
    flex: 1;
    min-width: 0;
  }
  .cmd {
    flex: 2;
    min-width: 0;
  }
  .card-foot {
    padding-left: 26px;
  }
  .layout {
    width: 120px !important;
    height: 24px !important;
  }
  .icon {
    display: grid;
    place-items: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--text-muted);
  }
  .icon:hover:not(:disabled) {
    background: var(--tab-hover);
    color: var(--text);
  }
  .icon:disabled {
    opacity: 0.3;
  }
  .small {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    align-self: flex-start;
    height: 24px !important;
    padding: 0 10px !important;
    font-size: 11.5px !important;
  }
  .syntax {
    display: block;
    margin: 0 0 10px;
  }
  .params {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 10px;
  }
  .params-head {
    color: var(--text-muted);
    font-size: 11.5px;
    font-weight: 600;
  }
  .param {
    display: grid;
    grid-template-columns: 80px 1fr 1fr 1fr;
    align-items: center;
    gap: 6px;
  }
  .pname {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .toml {
    max-height: 50vh;
    overflow: auto;
    white-space: pre;
  }
</style>
