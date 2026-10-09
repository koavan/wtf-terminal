<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { fieldsOf, snippets, type Preview, type Snippet } from '../lib/snippets.svelte';
  import { toast } from '../lib/toast.svelte';

  // Adds or edits a snippet with a form instead of TOML. Saving shows the
  // exact block that will be written, and where; only Confirm writes. An
  // edit replaces that snippet's block alone, so the rest of the file
  // (comments, other snippets) stays as it was.

  let { snippet, command = '', duplicate = false }: { snippet: Snippet | null; command?: string; duplicate?: boolean } = $props();

  const original = untrack(() => snippet);
  const editing = !!original && !untrack(() => duplicate);

  interface ParamDraft {
    description: string;
    default: string;
    choices: string;
    raw: boolean;
  }

  let name = $state(original ? (editing ? original.name : `${original.name} (copy)`) : '');
  let description = $state(original?.description ?? '');
  let mode = $state<'command' | 'runbook'>(original && original.steps.length ? 'runbook' : 'command');
  let cmd = $state(original?.command ?? untrack(() => command));
  let steps = $state(original?.steps.length ? original.steps.map((s) => ({ ...s })) : [{ title: '', command: '' }]);
  let params = $state<Record<string, ParamDraft>>(
    Object.fromEntries(
      (original?.params ?? []).map((p) => [p.name, { description: p.description, default: p.default, choices: (p.choices ?? []).join(', '), raw: p.raw }]),
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

  /** Placeholders in the command(s), in order: each gets its settings below. */
  const placeholders = $derived(
    fieldsOf({
      id: '', name: '', description: '', tags: [], source: '', index: 0, project: false, params: [],
      command: mode === 'command' ? cmd : '',
      steps: mode === 'runbook' ? steps.filter((s) => s.command.trim()) : [],
    }),
  );
  $effect(() => {
    for (const p of placeholders) if (!params[p.name]) params[p.name] = { description: '', default: '', choices: '', raw: false };
  });

  const edit = $derived({
    kind: 'snippet',
    workspace: { name: '', description: '', window: '', tabs: [], params: [] },
    file,
    index: editing ? original!.index : -1,
    version: snippets.versions[file] ?? '',
    snippet: {
      name,
      description,
      tags: original?.tags ?? [],
      command: mode === 'command' ? cmd : '',
      steps: mode === 'runbook' ? steps : [],
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
      toast.show(editing ? `Saved “${name.trim()}”` : `Added “${name.trim()}”`);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      preview = null;
    } finally {
      busy = false;
    }
  }

  function moveStep(i: number, by: number) {
    const j = i + by;
    if (j < 0 || j >= steps.length) return;
    [steps[i], steps[j]] = [steps[j], steps[i]];
  }
</script>

<Dialog
  title={preview ? 'Review' : editing ? `Edit “${original!.name}”` : 'New Snippet'}
  subtitle={preview ? `${preview.newFile ? 'Creates' : editing ? 'Replaces this snippet in' : 'Adds to the end of'} ${prettyPath(preview.file)}. Nothing is written until you confirm.` : 'A command with blanks to fill in, or a runbook of steps'}
  width={600}
  onsubmit={() => (preview ? save() : review())}
>
  {#if preview}
    <pre class="command toml">{preview.text}</pre>
    {#if editing}<p class="hint">The rest of the file — other snippets, your comments — stays as it is.</p>{/if}
  {:else}
    <label class="field">
      <span>Name</span>
      <input bind:this={nameInput} bind:value={name} placeholder="Restart a deployment" autocomplete="off" />
    </label>
    <label class="field">
      <span>Description</span>
      <input bind:value={description} placeholder="optional — shown in the list" autocomplete="off" />
    </label>

    <div class="kinds" role="radiogroup" aria-label="Kind">
      <button role="radio" aria-checked={mode === 'command'} class:on={mode === 'command'} onclick={() => (mode = 'command')}>Command</button>
      <button role="radio" aria-checked={mode === 'runbook'} class:on={mode === 'runbook'} onclick={() => (mode = 'runbook')}>Runbook (steps)</button>
    </div>

    {#if mode === 'command'}
      <label class="field">
        <span>Command</span>
        <input bind:value={cmd} placeholder={'kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}'} spellcheck="false" autocomplete="off" />
      </label>
    {:else}
      <div class="steps">
        {#each steps as st, i (i)}
          <div class="step">
            <span class="num">{i + 1}</span>
            <div class="step-fields">
              <input bind:value={st.title} placeholder="What this step does (optional)" autocomplete="off" />
              <input class="mono-input" bind:value={st.command} placeholder={'kubectl cordon {{node}}'} spellcheck="false" autocomplete="off" />
            </div>
            <div class="step-tools">
              <button class="icon" aria-label="Move step {i + 1} up" disabled={i === 0} onclick={() => moveStep(i, -1)}><Icon name="chevronUp" size={12} /></button>
              <button class="icon" aria-label="Move step {i + 1} down" disabled={i === steps.length - 1} onclick={() => moveStep(i, 1)}><Icon name="chevronDown" size={12} /></button>
              <button class="icon" aria-label="Remove step {i + 1}" disabled={steps.length === 1} onclick={() => steps.splice(i, 1)}><Icon name="x" size={12} /></button>
            </div>
          </div>
        {/each}
        <button class="secondary small" onclick={() => steps.push({ title: '', command: '' })}><Icon name="plus" size={12} /> Add Step</button>
      </div>
    {/if}
    <small class="syntax">
      <code>{'{{name}}'}</code> asks for a value; <code>{'{{name=default}}'}</code> fills one in. Defaults can follow the shell:
      <code>$kube_namespace</code>, <code>$kube_context</code>, <code>$aws_profile</code>, <code>$aws_region</code>, <code>$cwd</code>.
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
              <input bind:value={d.default} placeholder={p.default && p.default !== d.default ? `default (${p.default} in the command)` : 'default'} spellcheck="false" autocomplete="off" />
              <input bind:value={d.choices} placeholder="choices, comma-separated" spellcheck="false" autocomplete="off" />
              <label class="raw" title="Insert as typed instead of shell-quoting (for several flags at once)"><input type="checkbox" bind:checked={d.raw} /> raw</label>
            </div>
          {/if}
        {/each}
      </div>
    {/if}

    {#if !editing}
      <label class="field">
        <span>Save to</span>
        <select bind:value={file}>
          {#each snippets.targets as t (t.path)}<option value={t.path}>{targetLabel(t.path)}</option>{/each}
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
  .kinds {
    display: flex;
    gap: 2px;
    margin: 0 0 10px;
    padding: 2px;
    border-radius: 8px;
    background: var(--identity);
  }
  .kinds button {
    flex: 1;
    height: 26px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 550;
  }
  .kinds button.on {
    background: var(--popover);
    color: var(--text);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.15);
  }
  .steps {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 6px;
  }
  .step {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  .num {
    display: grid;
    place-items: center;
    flex: none;
    width: 20px;
    height: 20px;
    margin-top: 5px;
    border-radius: 50%;
    background: var(--identity);
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 700;
  }
  .step-fields {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
    min-width: 0;
  }
  .step-tools {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 22px;
    height: 20px;
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
    grid-template-columns: 70px 1fr 1fr 1fr auto;
    align-items: center;
    gap: 6px;
  }
  .pname {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .raw {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--text-muted);
    font-size: 11.5px;
  }
  .toml {
    max-height: 50vh;
    overflow: auto;
    white-space: pre;
  }
</style>
