<script lang="ts">
  import { untrack } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/snippets';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { fieldsOf, isRunbook, openSnippet, snippets, type Snippet } from '../lib/snippets.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // Snippets and runbooks, by file: the project's (from the focused shell's
  // folder up), yours, and team files. Reading is local; every change goes
  // through the editor's review, and deleting asks first.

  let filter = $state('');

  // The project's snippets depend on the focused shell's folder.
  $effect(() => {
    void tabs.activePane?.cwd;
    untrack(() => snippets.load());
  });

  const shown = $derived(
    snippets.list.filter((s) => {
      const q = filter.trim().toLowerCase();
      return !q || `${s.name} ${s.description} ${s.command} ${s.steps.map((st) => st.command).join(' ')} ${s.tags.join(' ')}`.toLowerCase().includes(q);
    }),
  );

  const sections = $derived.by(() =>
    snippets.files
      .map((f) => {
        const kind = snippets.targets.find((t) => t.path === f)?.kind;
        const title = kind === 'project' ? `This project · ${prettyPath(f)}` : kind === 'user' ? 'Yours' : prettyPath(f);
        return { file: f, title, list: shown.filter((s) => s.source === f) };
      })
      .filter((s) => s.list.length),
  );

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function remove(s: Snippet) {
    overlay.confirm({
      title: `Delete “${s.name}”?`,
      message: `Removes it from ${prettyPath(s.source)}. The file’s other snippets and comments stay.`,
      items: [],
      confirmLabel: 'Delete Snippet',
      onConfirm: () =>
        snippets
          .remove(s)
          .then(() => toast.show(`Deleted “${s.name}”`))
          .catch((e) => toast.show(e instanceof Error ? e.message : String(e), 6000)),
    });
  }

  function menu(e: MouseEvent, s: Snippet) {
    e.preventDefault();
    const entries: MenuEntry[] = [{ label: isRunbook(s) || fieldsOf(s).length ? 'Run…' : 'Insert at Prompt', hint: 'double-click', action: () => openSnippet(s) }];
    if (!isRunbook(s)) entries.push({ label: 'Copy Command', action: () => copy(s.command, 'command') });
    entries.push(
      'separator',
      { label: 'Edit…', action: () => overlay.show({ kind: 'snippetEdit', snippet: s }) },
      { label: 'Duplicate…', action: () => overlay.show({ kind: 'snippetEdit', snippet: s, duplicate: true }) },
      'separator',
      { label: 'Delete…', danger: true, action: () => remove(s) },
    );
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  function detail(s: Snippet): string {
    if (s.description) return s.description;
    // A runbook without a description: its steps, by title (the pill counts them).
    return isRunbook(s) ? s.steps.map((st, i) => st.title || `step ${i + 1}`).join(' → ') : s.command;
  }
</script>

<div class="snippets">
  <header>
    <div class="filter">
      <Icon name="search" size={12} stroke={2} />
      <input bind:value={filter} placeholder="Filter snippets" spellcheck="false" autocomplete="off" />
    </div>
    <button class="tool" title="New snippet" aria-label="New snippet" onclick={() => overlay.show({ kind: 'snippetEdit', snippet: null })}>
      <Icon name="plus" size={14} />
    </button>
    <button class="tool" title="Open your snippets file in a text editor" aria-label="Open snippets file" onclick={() => Service.OpenUserFile().catch((e) => toast.show(`Couldn't open it: ${e}`))}>
      <Icon name="file" size={13} />
    </button>
    <button class="tool" title="Reload" aria-label="Reload" onclick={() => snippets.load()}><Icon name="refresh" size={13} /></button>
  </header>

  <div class="list">
    {#each sections as sec (sec.file)}
      <div class="section" title={sec.file}>{sec.title}</div>
      {#each sec.list as s (s.id)}
        <div class="row" role="listitem" title={isRunbook(s) ? s.steps.map((st, i) => `${i + 1}. ${st.command}`).join('\n') : s.command} oncontextmenu={(e) => menu(e, s)} ondblclick={() => openSnippet(s)}>
          <span class="icon"><Icon name={isRunbook(s) ? 'layers' : 'terminal'} size={13} /></span>
          <span class="main">
            <span class="name">{s.name}</span>
            <span class="sub" class:mono={!s.description && !isRunbook(s)}>{detail(s)}</span>
          </span>
          {#if isRunbook(s)}<span class="pill">{s.steps.length} steps</span>{/if}
          <button
            class="run"
            title={isRunbook(s) || fieldsOf(s).length ? 'Fill in and run' : 'Put it at the prompt'}
            aria-label="Run {s.name}"
            onclick={(e) => {
              e.stopPropagation();
              openSnippet(s);
            }}
            ondblclick={(e) => e.stopPropagation()}
          >
            <Icon name="play" size={11} stroke={2} />
          </button>
        </div>
      {/each}
    {:else}
      {#if snippets.loaded}
        <div class="note">
          {#if filter}
            No snippets match “{filter}”.
          {:else}
            <strong>No snippets yet</strong>
            Save commands you run often, with blanks to fill in — or runbooks of steps. Press + to make one, or right-click a command in the terminal → Save as Snippet….
            <button class="link" onclick={() => Service.OpenUserFile().then(() => snippets.load())}>Start from examples</button>
          {/if}
        </div>
      {/if}
    {/each}
    {#each snippets.problems as p (p)}<div class="problem">{p}</div>{/each}
  </div>
</div>

<style>
  .snippets {
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
    overflow: hidden;
    font-size: 12.5px;
    font-weight: 550;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub.mono {
    font-family: 'JetBrains Mono Variable', monospace;
  }
  .pill {
    flex: none;
    padding: 0 5px;
    border-radius: 4px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
    font-size: 10px;
    font-weight: 600;
    line-height: 16px;
  }
  .run {
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
  .row:hover .run {
    opacity: 1;
  }
  .run:hover {
    background: var(--accent-soft);
  }
  .note {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 14px 8px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .link {
    align-self: flex-start;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font-size: 12px;
  }
  .problem {
    padding: 8px;
    color: var(--danger);
    font-size: 11px;
  }
</style>
