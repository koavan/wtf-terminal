<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { resolveDefault, shellFacts, type Workspace } from '../lib/snippets.svelte';
  import { toast } from '../lib/toast.svelte';
  import { openWorkspace, planWorkspace, workspaceFields } from '../lib/workspaces.svelte';

  // Opening a workspace: fill in its placeholders, see every tab and the
  // command each will run, then open it here or in a new window.

  let { workspace }: { workspace: Workspace } = $props();
  const w = untrack(() => workspace);
  const fields = workspaceFields(w);
  const facts = shellFacts();
  let values = $state<Record<string, string>>(
    Object.fromEntries(fields.map((f) => [f.name, resolveDefault(f.default, facts) || (f.choices[0] ?? '')])),
  );
  let first = $state<HTMLElement>();

  onMount(() => {
    if (first instanceof HTMLInputElement) first.select();
    else first?.focus();
  });

  const result = $derived.by(() => {
    try {
      return { plan: planWorkspace(w, values), error: '' };
    } catch (e) {
      return { plan: null, error: e instanceof Error ? e.message : String(e) };
    }
  });

  async function open(newWindow: boolean) {
    if (!result.plan) return;
    overlay.close();
    try {
      await openWorkspace(w, values, newWindow);
    } catch (e) {
      toast.show(`Couldn't open ${w.name}: ${e instanceof Error ? e.message : e}`, 6000);
    }
  }
</script>

<Dialog
  title="Open “{w.name}”"
  subtitle={[w.description, `From ${prettyPath(w.source)}`].filter(Boolean).join(' · ')}
  width={560}
  onsubmit={() => open(w.window === 'new')}
>
  {#each fields as f, i (f.name)}
    <label class="field">
      <span>{f.name}{f.description ? ` — ${f.description}` : ''}</span>
      {#if f.choices.length}
        {#if i === 0}
          <select bind:this={first} bind:value={values[f.name]}>{#each f.choices as c (c)}<option value={c}>{c}</option>{/each}</select>
        {:else}
          <select bind:value={values[f.name]}>{#each f.choices as c (c)}<option value={c}>{c}</option>{/each}</select>
        {/if}
      {:else if i === 0}
        <input bind:this={first} bind:value={values[f.name]} placeholder={f.default} spellcheck="false" autocomplete="off" />
      {:else}
        <input bind:value={values[f.name]} placeholder={f.default} spellcheck="false" autocomplete="off" />
      {/if}
    </label>
  {/each}

  {#if result.plan}
    <ol class="tabs">
      {#each result.plan.tabs as t, i (i)}
        <li>
          <div class="tab-head">
            <span class="title">{t.title || `Tab ${i + 1}`}</span>
            {#if t.group}<span class="pill">{t.group}</span>{/if}
            {#if t.panes.length > 1}<span class="pill">{t.panes.length} panes, {t.layout === 'row' ? 'side by side' : 'stacked'}</span>{/if}
          </div>
          {#each t.panes as p, j (j)}
            <div class="pane">
              <span class="cwd">{p.cwd ? prettyPath(p.cwd) : 'current folder'}</span>
              {#if p.command}<code>{p.command}</code>{/if}
            </div>
          {/each}
        </li>
      {/each}
    </ol>
    <p class="hint">Opens {result.plan.tabs.length} {result.plan.tabs.length === 1 ? 'tab' : 'tabs'}; each startup command is typed into its new shell and run.</p>
  {:else}
    <p class="hint">{result.error}</p>
  {/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class={w.window === 'new' ? 'primary' : 'secondary'} onclick={() => open(true)} disabled={!result.plan}>Open in New Window</button>
    {#if w.window !== 'new'}<button class="primary" onclick={() => open(false)} disabled={!result.plan}>Open Here</button>{/if}
  {/snippet}
</Dialog>

<style>
  .tabs {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 50vh;
    overflow: auto;
    margin: 4px 0 8px;
    padding: 0;
    list-style: none;
  }
  .tab-head {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 2px;
  }
  .title {
    font-size: 12.5px;
    font-weight: 600;
  }
  .pill {
    padding: 0 6px;
    border-radius: 4px;
    background: var(--identity);
    color: var(--text-muted);
    font-size: 10.5px;
    font-weight: 600;
    line-height: 17px;
  }
  .pane {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 2px 0 2px 10px;
    font-size: 12px;
  }
  .cwd {
    flex: none;
    color: var(--text-muted);
  }
  .pane code {
    overflow-wrap: anywhere;
  }
</style>
