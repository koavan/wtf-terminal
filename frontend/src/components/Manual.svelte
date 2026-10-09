<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { Browser } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import { commands, shortcutLabel } from '../lib/commands';
  import { plainText, renderManual, type ManualContext } from '../lib/manual';
  import { isMac } from '../lib/platform';
  import { overlay } from '../lib/overlay.svelte';
  import { TOPICS, type Topic } from '../manual';

  // The in-app manual: topics on the left (with search), the page on the
  // right. Pages are Markdown in src/manual; shortcuts come from the
  // command registry, so they're right for this computer.

  let { topic }: { topic: string } = $props();

  let current = $state(untrack(() => (TOPICS.some((t) => t.id === topic) ? topic : TOPICS[0].id)));
  let query = $state('');
  let content = $state<HTMLElement>();
  let searchInput = $state<HTMLInputElement>();
  /** Pages visited in this window, for Back. */
  let trail = $state<string[]>([]);

  const byId = new Map(commands.map((c) => [c.id, c]));
  const ctx: ManualContext = {
    isMac,
    label: (id) => byId.get(id)?.label ?? null,
    shortcut: (id) => {
      const c = byId.get(id);
      return c ? (shortcutLabel(c) ?? null) : null;
    },
  };
  const texts = new Map(TOPICS.map((t) => [t.id, `${t.title} ${plainText(t.source)}`.toLowerCase()]));

  const page = $derived(TOPICS.find((t) => t.id === current) ?? TOPICS[0]);
  const html = $derived(renderManual(page.source, ctx).html);

  /** Topics whose title or text has every word of the query. */
  const found = $derived.by(() => {
    const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return TOPICS;
    return TOPICS.filter((t) => words.every((w) => texts.get(t.id)!.includes(w)));
  });
  const groups = $derived.by(() => {
    const out: { group: string; topics: Topic[] }[] = [];
    for (const t of found) {
      const g = out.find((x) => x.group === t.group);
      if (g) g.topics.push(t);
      else out.push({ group: t.group, topics: [t] });
    }
    return out;
  });

  /** Every command, for the shortcuts page. */
  const shortcutRows = commands
    .map((c) => ({ label: c.label, key: shortcutLabel(c) ?? '' }))
    .sort((a, b) => a.label.localeCompare(b.label));

  async function open(id: string, remember = true) {
    if (id === current) return;
    if (remember) trail = [...trail, current];
    current = id;
    await tick();
    content?.scrollTo(0, 0);
  }

  function back() {
    const prev = trail.at(-1);
    if (!prev) return;
    trail = trail.slice(0, -1);
    open(prev, false);
  }

  /** Links inside a page: another page, or a web page in the browser. */
  function onClick(e: MouseEvent) {
    const a = (e.target as HTMLElement).closest('a');
    if (!a) return;
    e.preventDefault();
    if (a.dataset.topic) open(a.dataset.topic);
    else if (a.dataset.href) Browser.OpenURL(a.dataset.href);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      if (query) query = '';
      else overlay.close();
    } else if ((e.metaKey || e.ctrlKey) && e.key === 'f') {
      e.preventDefault();
      searchInput?.focus();
    } else if (e.key === 'Enter' && document.activeElement === searchInput && found.length) {
      e.preventDefault();
      open(found[0].id);
    }
  }

  onMount(() => searchInput?.focus());

  function appear(_node: HTMLElement) {
    return {
      duration: 160,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: translate(-50%, -50%) scale(${0.97 + 0.03 * t});`,
    };
  }
</script>

<svelte:window onkeydowncapture={onKeydown} />

<div class="backdrop" role="presentation" onpointerdown={() => overlay.close()}></div>

<div class="manual" role="dialog" aria-modal="true" aria-label="wtf manual" in:appear>
  <nav>
    <div class="search">
      <Icon name="search" size={12} stroke={2} />
      <input bind:this={searchInput} bind:value={query} placeholder="Search the manual" spellcheck="false" autocomplete="off" />
    </div>
    <div class="topics">
      {#each groups as g (g.group)}
        <div class="group">{g.group}</div>
        {#each g.topics as t (t.id)}
          <button class="topic" class:on={t.id === current} onclick={() => open(t.id)}>{t.title}</button>
        {/each}
      {:else}
        <div class="none">Nothing matches “{query}”.</div>
      {/each}
    </div>
  </nav>

  <article bind:this={content}>
    <div class="bar">
      <button class="tool" onclick={back} disabled={!trail.length} aria-label="Back" title="Back"><Icon name="arrowLeft" size={13} /></button>
      <span class="crumb">{page.group}</span>
      <span class="spacer"></span>
      <button class="tool" onclick={() => overlay.close()} aria-label="Close manual" title="Close (Esc)"><Icon name="x" size={13} /></button>
    </div>
    <!-- Clicks on the page's own links (anchors) navigate. -->
    <div class="page" role="presentation" onclick={onClick}>
      <!-- The renderer escapes all text; the only markup is its own (lib/manual.ts). -->
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      {@html html}
      {#if page.id === 'shortcuts'}
        <table>
          <thead><tr><th>Command</th><th>Shortcut</th></tr></thead>
          <tbody>
            {#each shortcutRows as r (r.label)}
              <tr><td>{r.label}</td><td>{#if r.key}<kbd>{r.key}</kbd>{:else}<span class="muted">palette only</span>{/if}</td></tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </article>
</div>

<style>
  .backdrop {
    --wails-draggable: no-drag;
    position: fixed;
    inset: 0;
    z-index: 110;
    background: var(--scrim);
  }
  .manual {
    --wails-draggable: no-drag;
    position: fixed;
    top: 50%;
    left: 50%;
    z-index: 111;
    display: flex;
    width: min(980px, calc(100vw - 48px));
    height: min(760px, calc(100vh - 64px));
    overflow: hidden;
    transform: translate(-50%, -50%);
    border-radius: 14px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(30px) saturate(180%);
    backdrop-filter: blur(30px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
  }

  nav {
    display: flex;
    flex-direction: column;
    flex: none;
    width: 230px;
    border-right: 1px solid var(--hairline);
  }
  .search {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 12px 10px 6px;
    height: 28px;
    padding: 0 9px;
    border-radius: 8px;
    background: var(--identity);
    color: var(--text-muted);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 12.5px;
  }
  .search input:focus {
    outline: none;
  }
  .topics {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 6px 12px;
  }
  .group {
    padding: 12px 8px 4px;
    color: var(--text-muted);
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .topic {
    display: block;
    width: 100%;
    padding: 5px 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    font-size: 12.5px;
    text-align: left;
  }
  .topic:hover {
    background: var(--tab-hover);
  }
  .topic.on {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 600;
  }
  .none {
    padding: 12px 8px;
    color: var(--text-muted);
    font-size: 12px;
  }

  article {
    flex: 1;
    min-width: 0;
    overflow: auto;
  }
  .bar {
    position: sticky;
    top: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px 6px 14px;
    background: var(--popover);
  }
  .crumb {
    color: var(--text-muted);
    font-size: 11.5px;
  }
  .spacer {
    flex: 1;
  }
  .tool {
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
  .tool:hover:not(:disabled) {
    background: var(--tab-hover);
    color: var(--text);
  }
  .tool:disabled {
    opacity: 0.35;
  }

  .page {
    max-width: 680px;
    padding: 0 32px 40px;
    font-size: 13.5px;
    line-height: 1.6;
    -webkit-user-select: text;
    user-select: text;
  }
  .page :global(h1) {
    margin: 4px 0 12px;
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -0.01em;
  }
  .page :global(h2) {
    margin: 26px 0 8px;
    font-size: 15.5px;
    font-weight: 650;
  }
  .page :global(h3) {
    margin: 18px 0 6px;
    font-size: 13.5px;
    font-weight: 650;
  }
  .page :global(p) {
    margin: 0 0 10px;
  }
  .page :global(ul),
  .page :global(ol) {
    margin: 0 0 12px;
    padding-left: 22px;
  }
  .page :global(li) {
    margin: 3px 0;
  }
  .page :global(code) {
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--identity);
    font: 12px 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
  }
  .page :global(pre) {
    margin: 0 0 12px;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--identity);
    overflow: auto;
  }
  .page :global(pre code) {
    padding: 0;
    background: none;
  }
  .page :global(kbd) {
    display: inline-block;
    padding: 0 6px;
    border: 1px solid var(--hairline);
    border-radius: 5px;
    background: var(--identity);
    font: 600 11.5px system-ui, sans-serif;
    line-height: 19px;
    white-space: nowrap;
  }
  .page :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  .page :global(a:hover) {
    text-decoration: underline;
  }
  .page :global(aside) {
    margin: 4px 0 14px;
    padding: 10px 14px;
    border-left: 3px solid var(--accent);
    border-radius: 0 8px 8px 0;
    background: var(--accent-soft);
  }
  .page :global(table) {
    width: 100%;
    margin: 4px 0 14px;
    border-collapse: collapse;
    font-size: 12.5px;
  }
  .page :global(th),
  .page :global(td) {
    padding: 6px 10px;
    border-bottom: 1px solid var(--hairline);
    text-align: left;
    vertical-align: top;
  }
  .page :global(th) {
    color: var(--text-muted);
    font-size: 11.5px;
    font-weight: 650;
  }
  .muted {
    color: var(--text-muted);
    font-size: 11.5px;
  }
</style>
