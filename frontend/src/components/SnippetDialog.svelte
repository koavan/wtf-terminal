<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import Dialog from './Dialog.svelte';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import {
    fieldsOf, fill, isRunbook, resolveDefault, runSnippetCommand, shellFacts, type RunHow, type Snippet,
  } from '../lib/snippets.svelte';

  // Fills in a snippet's placeholders and runs it — or, for a runbook, each
  // step when you're ready. The preview is exactly what will run.

  let { snippet }: { snippet: Snippet } = $props();
  const s = untrack(() => snippet);
  const fields = fieldsOf(s);
  const facts = shellFacts();
  // A field with choices starts on its default, else its first choice.
  let values = $state<Record<string, string>>(
    Object.fromEntries(fields.map((f) => [f.name, resolveDefault(f.default, facts) || (f.choices[0] ?? '')])),
  );
  const done = new SvelteSet<number>();
  let first = $state<HTMLElement>();

  onMount(() => {
    if (first instanceof HTMLInputElement) first.select();
    else first?.focus();
  });

  /** Each command filled in, or why it can't be yet. */
  const filled = $derived(
    (isRunbook(s) ? s.steps : [{ title: '', command: s.command }]).map((st) => {
      try {
        return { title: st.title, command: fill(st.command, values, fields), error: '' };
      } catch (e) {
        return { title: st.title, command: '', error: e instanceof Error ? e.message : String(e) };
      }
    }),
  );

  function run(i: number, how: RunHow) {
    const c = filled[i];
    if (!c?.command) return;
    if (!runSnippetCommand(c.command, how)) return;
    if (isRunbook(s)) done.add(i);
    else overlay.close();
  }

  function onsubmit() {
    if (!isRunbook(s)) run(0, 'insert');
  }

  function onKeydown(e: KeyboardEvent) {
    // ⌘↩ runs a single command right away (↩ inserts it).
    if (!isRunbook(s) && e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      e.stopPropagation();
      run(0, 'run');
    }
  }
</script>

<svelte:window onkeydowncapture={onKeydown} />

<Dialog
  title={s.name}
  subtitle={[s.description, `From ${prettyPath(s.source)}`].filter(Boolean).join(' · ')}
  width={isRunbook(s) ? 600 : 520}
  {onsubmit}
>
  {#each fields as f, i (f.name)}
    <label class="field">
      <span>{f.name}{f.description ? ` — ${f.description}` : ''}</span>
      {#if f.choices.length}
        {#if i === 0}
          <select bind:this={first} bind:value={values[f.name]}>
            {#each f.choices as c (c)}<option value={c}>{c}</option>{/each}
          </select>
        {:else}
          <select bind:value={values[f.name]}>
            {#each f.choices as c (c)}<option value={c}>{c}</option>{/each}
          </select>
        {/if}
      {:else if i === 0}
        <input bind:this={first} bind:value={values[f.name]} placeholder={f.default} spellcheck="false" autocomplete="off" />
      {:else}
        <input bind:value={values[f.name]} placeholder={f.default} spellcheck="false" autocomplete="off" />
      {/if}
    </label>
  {/each}

  {#if isRunbook(s)}
    <ol class="steps">
      {#each filled as st, i (i)}
        <li class:done={done.has(i)}>
          <div class="step-head">
            <span class="num">{#if done.has(i)}<Icon name="check" size={12} stroke={2.5} />{:else}{i + 1}{/if}</span>
            <span class="title">{st.title || `Step ${i + 1}`}</span>
            <button class="secondary small" onclick={() => run(i, 'insert')} disabled={!st.command} title="Put it at the prompt">Insert</button>
            <button class="primary small" onclick={() => run(i, 'run')} disabled={!st.command}>Run</button>
          </div>
          {#if st.command}<div class="command">{st.command}</div>{:else}<div class="hint pending">{st.error}</div>{/if}
        </li>
      {/each}
    </ol>
    <p class="hint">Steps run in the focused terminal, one at a time: watch each one finish before the next.</p>
  {:else}
    {#if filled[0].command}
      <div class="command">{filled[0].command}</div>
    {:else}
      <p class="hint pending">{filled[0].error}</p>
    {/if}
  {/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    {#if isRunbook(s)}
      <button class="secondary" onclick={() => overlay.close()}>Close</button>
    {:else}
      <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
      <button class="secondary" onclick={() => run(0, 'tab')} disabled={!filled[0].command}>Run in New Tab</button>
      <button class="secondary" onclick={() => run(0, 'run')} disabled={!filled[0].command} title={isMac ? '⌘↩' : 'Ctrl+Enter'}>Run</button>
      <button class="primary" onclick={() => run(0, 'insert')} disabled={!filled[0].command} title="Put it at the prompt to check, then press Enter (↩)">Insert</button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .steps {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-height: 50vh;
    overflow: auto;
    margin: 4px 0 8px;
    padding: 0;
    list-style: none;
  }
  .step-head {
    display: flex;
    align-items: center;
    gap: 8px;
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
  li.done .num {
    background: color-mix(in srgb, var(--mark-ok) 25%, transparent);
    color: var(--mark-ok);
  }
  .title {
    flex: 1;
    min-width: 0;
    font-size: 12.5px;
    font-weight: 600;
  }
  li.done .title {
    color: var(--text-muted);
  }
  .small {
    height: 24px !important;
    padding: 0 10px !important;
    font-size: 11.5px !important;
  }
  .pending {
    margin: 4px 0 0;
  }
</style>
