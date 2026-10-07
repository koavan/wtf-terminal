<script lang="ts">
  import { onMount } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import type { ISearchOptions, SearchAddon } from '@xterm/addon-search';
  import Icon from './Icon.svelte';
  import { isMac } from '../lib/platform';
  import { search } from '../lib/search.svelte';
  import { settings } from '../lib/settings.svelte';

  let { addon }: { addon: SearchAddon } = $props();

  let input = $state<HTMLInputElement>();
  let query = $state('');
  let caseSensitive = $state(false);
  let results = $state<{ index: number; count: number } | null>(null);

  // Decoration colors must be #RRGGBB.
  const decorations = $derived(
    settings.scheme === 'dark'
      ? {
          matchBackground: '#4a4220',
          matchOverviewRuler: '#8a7a30',
          activeMatchBackground: '#c9a227',
          activeMatchBorder: '#ffd866',
          activeMatchColorOverviewRuler: '#ffd866',
        }
      : {
          matchBackground: '#fff1a8',
          matchOverviewRuler: '#d9b200',
          activeMatchBackground: '#ffc933',
          activeMatchBorder: '#d99a00',
          activeMatchColorOverviewRuler: '#d99a00',
        },
  );

  function options(incremental = false): ISearchOptions {
    return { caseSensitive, incremental, decorations };
  }

  const next = () => query && addon.findNext(query, options());
  const prev = () => query && addon.findPrevious(query, options());

  onMount(() => {
    search.controller = { next, prev, focus: () => input?.select() };
    input?.focus();
    const sub = addon.onDidChangeResults((e) => {
      results = { index: e.resultIndex, count: e.resultCount };
    });
    return () => {
      sub.dispose();
      addon.clearDecorations();
    };
  });

  // Search as you type.
  $effect(() => {
    if (query) {
      addon.findNext(query, options(true));
    } else {
      addon.clearDecorations();
      results = null;
    }
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      search.close();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (e.shiftKey) prev();
      else next();
    }
  }

  function slide(_node: HTMLElement) {
    return {
      duration: 140,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: translateY(${(1 - t) * -6}px);`,
    };
  }

  const status = $derived(
    !results || !query ? '' : results.count === 0 ? 'No results' : results.index < 0 ? `${results.count}+` : `${results.index + 1} of ${results.count}`,
  );
</script>

<div class="search" role="search" in:slide>
  <input
    bind:this={input}
    bind:value={query}
    placeholder="Find"
    spellcheck="false"
    autocomplete="off"
    onkeydown={onKeydown}
  />
  <span class="status" class:none={results?.count === 0}>{status}</span>
  <button
    class="toggle"
    class:on={caseSensitive}
    title="Match case"
    aria-pressed={caseSensitive}
    onclick={() => (caseSensitive = !caseSensitive)}>Aa</button
  >
  <span class="divider"></span>
  <button title="Previous ({isMac ? '⌘⇧G' : 'Shift+Enter'})" aria-label="Previous match" onclick={prev}>
    <Icon name="chevronUp" size={14} stroke={2} />
  </button>
  <button title="Next ({isMac ? '⌘G' : 'Enter'})" aria-label="Next match" onclick={next}>
    <Icon name="chevronDown" size={14} stroke={2} />
  </button>
  <button title="Close (Esc)" aria-label="Close search" onclick={() => search.close()}>
    <Icon name="x" size={13} stroke={2} />
  </button>
</div>

<style>
  .search {
    position: absolute;
    top: 8px;
    right: 14px;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 2px;
    height: 34px;
    padding: 0 4px 0 4px;
    border-radius: 10px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(24px) saturate(180%);
    backdrop-filter: blur(24px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
    font-size: 12.5px;
  }

  input {
    width: 180px;
    height: 26px;
    padding: 0 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    font: inherit;
    -webkit-user-select: text;
    user-select: text;
  }
  input:focus {
    outline: none;
  }
  input::placeholder {
    color: var(--text-muted);
  }

  .status {
    min-width: 58px;
    padding-right: 4px;
    text-align: right;
    color: var(--text-muted);
    font-size: 11.5px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .status.none {
    color: var(--danger);
  }

  button {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
  }
  button:hover {
    background: var(--tab-hover-strong);
    color: var(--text);
  }
  .toggle {
    width: 28px;
    font-size: 11.5px;
    font-weight: 650;
  }
  .toggle.on {
    background: var(--accent-soft);
    color: var(--accent);
  }

  .divider {
    width: 1px;
    height: 16px;
    margin: 0 3px;
    background: var(--hairline-strong);
  }
</style>
