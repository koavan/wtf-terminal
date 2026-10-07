<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import Icon, { iconForProcess } from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { closeTab } from '../lib/closing';
  import { fuzzyMatch } from '../lib/fuzzy';
  import { leaves } from '../lib/layout';
  import { overlay } from '../lib/overlay.svelte';
  import { focusedPane, paneTitle, tabs, type Group, type Tab } from '../lib/tabs.svelte';
  import { terminalFor } from '../lib/terminals';

  const PREVIEW_ROWS = 12;

  let query = $state('');
  let selectedId = $state<number | null>(tabs.activeId);
  let input = $state<HTMLInputElement>();
  let grid = $state<HTMLDivElement>();

  // Previews are taken once, when the overview opens.
  const previews = new Map<number, string[]>();
  for (const tab of tabs.list) previews.set(tab.id, terminalFor(focusedPane(tab).id)?.snapshot(PREVIEW_ROWS) ?? []);

  interface Section {
    group: Group | undefined;
    tabs: Tab[];
  }

  /** Tabs in strip order, split into sections at group boundaries. */
  const sections = $derived.by((): Section[] => {
    const q = query.trim();
    const matches = (t: Tab) => {
      if (!q) return true;
      const p = focusedPane(t);
      const group = tabs.group(t.groupId)?.name ?? '';
      return !!fuzzyMatch(q, `${paneTitle(p, prettyPath)} ${prettyPath(p.cwd)} ${group}`);
    };
    const out: Section[] = [];
    for (const tab of tabs.list) {
      if (!matches(tab)) continue;
      const group = tabs.group(tab.groupId);
      const last = out.at(-1);
      if (last && last.group?.id === group?.id) last.tabs.push(tab);
      else out.push({ group, tabs: [tab] });
    }
    return out;
  });

  const ordered = $derived(sections.flatMap((s) => s.tabs));

  // Keep a selection while filtering.
  $effect(() => {
    if (!ordered.some((t) => t.id === selectedId)) selectedId = ordered[0]?.id ?? null;
  });

  onMount(() => {
    input?.focus();
    scrollToSelected();
  });

  async function scrollToSelected() {
    await tick();
    grid?.querySelector('.card.selected')?.scrollIntoView({ block: 'nearest' });
  }

  function open(id: number) {
    overlay.close();
    tabs.activate(id);
  }

  /** Moves the selection; up/down move by the number of columns on screen. */
  function move(dx: number, dy: number) {
    const cards = [...(grid?.querySelectorAll<HTMLElement>('.card') ?? [])];
    const i = cards.findIndex((c) => Number(c.dataset.id) === selectedId);
    if (i < 0) return;
    let target = i + dx;
    if (dy) {
      // The card in the next/previous row that lines up best.
      const here = cards[i].getBoundingClientRect();
      const rows = cards.filter((c) => {
        const r = c.getBoundingClientRect();
        return dy > 0 ? r.top > here.top + 1 : r.top < here.top - 1;
      });
      const rowTop = dy > 0 ? Math.min(...rows.map((c) => c.getBoundingClientRect().top)) : Math.max(...rows.map((c) => c.getBoundingClientRect().top));
      const row = rows.filter((c) => Math.abs(c.getBoundingClientRect().top - rowTop) < 1);
      const best = row.sort(
        (a, b) => Math.abs(a.getBoundingClientRect().left - here.left) - Math.abs(b.getBoundingClientRect().left - here.left),
      )[0];
      if (!best) return;
      target = cards.indexOf(best);
    }
    const card = cards[Math.max(0, Math.min(target, cards.length - 1))];
    selectedId = Number(card.dataset.id);
    scrollToSelected();
  }

  function onKeydown(e: KeyboardEvent) {
    switch (e.key) {
      case 'Escape':
        e.preventDefault();
        e.stopPropagation();
        overlay.close();
        break;
      case 'Enter':
        e.preventDefault();
        if (selectedId !== null) open(selectedId);
        break;
      case 'ArrowRight':
        e.preventDefault();
        move(1, 0);
        break;
      case 'ArrowLeft':
        e.preventDefault();
        move(-1, 0);
        break;
      case 'ArrowDown':
        e.preventDefault();
        move(0, 1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        move(0, -1);
        break;
      case 'Backspace':
        // With an empty filter, ⌫ closes the selected tab.
        if (!query && selectedId !== null) {
          e.preventDefault();
          closeTab(selectedId);
        }
        break;
    }
  }

  function zoomIn(_node: HTMLElement) {
    return {
      duration: 180,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: scale(${1.02 - 0.02 * t});`,
    };
  }
</script>

<div class="overview" role="dialog" aria-label="All tabs" in:zoomIn>
  <header>
    <div class="field">
      <Icon name="search" size={14} stroke={2} />
      <input
        bind:this={input}
        bind:value={query}
        placeholder="Search {tabs.list.length} tabs"
        spellcheck="false"
        autocomplete="off"
        onkeydown={onKeydown}
      />
    </div>
    <button class="done" onclick={() => overlay.close()}>Done</button>
  </header>

  <div class="scroller" bind:this={grid}>
    {#each sections as section, si (si)}
      {#if section.group}
        <div class="section-title" style:--group-color="var(--g-{section.group.color})">
          <span class="dot"></span>{section.group.name || 'Unnamed group'}
          {#if section.group.collapsed}<span class="muted">· collapsed</span>{/if}
        </div>
      {:else if si > 0}
        <div class="section-title plain"></div>
      {/if}
      <div class="grid">
        {#each section.tabs as tab (tab.id)}
          {@const pane = focusedPane(tab)}
          {@const paneCount = leaves(tab.root).length}
          <div
            class="card"
            class:selected={tab.id === selectedId}
            class:active={tab.id === tabs.activeId}
            style:--group-color={section.group ? `var(--g-${section.group.color})` : null}
            data-id={tab.id}
            role="button"
            tabindex="-1"
            onclick={() => open(tab.id)}
            onkeydown={() => {}}
            onpointerenter={() => (selectedId = tab.id)}
          >
            <div class="card-head">
              <span class="icon"><Icon name={iconForProcess(pane.process, pane.isShell)} size={12} /></span>
              <span class="title">{paneTitle(pane, prettyPath)}</span>
              {#if paneCount > 1}<span class="badge">{paneCount} panes</span>{/if}
              <button
                class="close"
                aria-label="Close tab"
                onclick={(e) => {
                  e.stopPropagation();
                  closeTab(tab.id);
                }}><Icon name="x" size={11} stroke={2.25} /></button
              >
            </div>
            <pre class="preview">{(previews.get(tab.id) ?? []).join('\n')}</pre>
            <div class="card-foot">{prettyPath(pane.cwd)}</div>
          </div>
        {/each}
      </div>
    {:else}
      <div class="empty">No tabs match “{query}”</div>
    {/each}
  </div>
</div>

<style>
  .overview {
    --wails-draggable: no-drag;
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    flex-direction: column;
    background: var(--chrome);
    -webkit-backdrop-filter: blur(40px) saturate(160%);
    backdrop-filter: blur(40px) saturate(160%);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    flex: none;
    height: var(--strip-height);
    padding: 0 16px;
  }
  /* Clear the traffic lights, which float above the overview. */
  :global(.app.mac:not(.fullscreen)) ~ .overview header {
    padding-left: var(--traffic-lights-width);
  }

  .field {
    display: flex;
    align-items: center;
    gap: 8px;
    width: min(420px, 100%);
    height: 30px;
    padding: 0 10px;
    border-radius: 8px;
    background: var(--omnibox);
    box-shadow: inset 0 0 0 1px var(--hairline-strong);
    color: var(--text-muted);
  }
  input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    -webkit-user-select: text;
    user-select: text;
  }
  input:focus {
    outline: none;
  }
  .done {
    height: 28px;
    padding: 0 12px;
    border: 0;
    border-radius: 7px;
    background: var(--identity);
    color: var(--text);
    font-size: 12.5px;
    font-weight: 600;
  }
  .done:hover {
    background: var(--tab-hover-strong);
  }

  .scroller {
    flex: 1;
    overflow-y: auto;
    padding: 6px 24px 28px;
    scroll-padding: 24px;
  }

  .section-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 18px 4px 10px;
    color: var(--text);
    font-size: 12.5px;
    font-weight: 650;
  }
  .section-title.plain {
    height: 1px;
    margin: 22px 4px 14px;
    background: var(--hairline-strong);
  }
  .muted {
    color: var(--text-muted);
    font-weight: 500;
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--group-color);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 16px;
  }

  .card {
    display: flex;
    flex-direction: column;
    min-width: 0;
    border-radius: 12px;
    background: var(--surface);
    box-shadow: var(--card-shadow);
    overflow: hidden;
    cursor: default;
    transition: transform 140ms ease, box-shadow 140ms ease;
  }
  .card.selected {
    transform: translateY(-2px);
    box-shadow: 0 0 0 2px var(--accent), var(--card-shadow);
  }
  .card.active:not(.selected) {
    box-shadow: 0 0 0 1.5px var(--hairline-strong), var(--card-shadow);
  }

  .card-head {
    display: flex;
    align-items: center;
    gap: 7px;
    height: 32px;
    padding: 0 6px 0 10px;
    border-bottom: 1px solid var(--hairline);
    color: var(--text);
    font-size: 12px;
    font-weight: 600;
  }
  .card-head .icon {
    display: grid;
    color: var(--group-color, var(--accent));
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .badge {
    flex: none;
    padding: 1px 6px;
    border-radius: 5px;
    background: var(--identity);
    color: var(--text-muted);
    font-size: 10.5px;
    font-weight: 600;
  }
  .close {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--text-muted);
    opacity: 0;
  }
  .card:hover .close,
  .card.selected .close {
    opacity: 1;
  }
  .close:hover {
    background: var(--tab-hover-strong);
    color: var(--text);
  }

  .preview {
    height: calc(12 * 1.35em + 16px);
    margin: 0;
    padding: 8px 10px;
    overflow: hidden;
    color: var(--text);
    opacity: 0.85;
    font: 9.5px/1.35 'JetBrains Mono Variable', monospace;
    white-space: pre;
    -webkit-mask-image: linear-gradient(to right, #000 85%, transparent);
    mask-image: linear-gradient(to right, #000 85%, transparent);
  }

  .card-foot {
    padding: 6px 10px 8px;
    overflow: hidden;
    color: var(--text-muted);
    font-size: 11px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .empty {
    padding: 60px 0;
    text-align: center;
    color: var(--text-muted);
  }
</style>
