<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import Icon, { iconForProcess, type IconName } from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { commands, shortcutLabel } from '../lib/commands';
  import { fuzzyMatch } from '../lib/fuzzy';
  import { Service as ConfigService } from '../../bindings/github.com/dayanraj/wtf/internal/config';
  import { overlay, type PaletteMode } from '../lib/overlay.svelte';
  import { settings, type Theme } from '../lib/settings.svelte';
  import { focusedPane, paneTitle, tabs, type GroupColor } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  let { mode = 'commands' }: { mode?: PaletteMode } = $props();

  interface Entry {
    key: string;
    label: string;
    detail?: string;
    hint?: string;
    icon?: IconName;
    color?: GroupColor;
    /** In theme mode: the theme to preview while this row is selected. */
    theme?: Theme;
    section: 'Tabs' | 'Groups' | 'Commands' | 'Themes';
    run: () => void;
  }

  let themes = $state.raw<Theme[]>([]);
  let committed = false;

  function commitTheme(spec: string) {
    committed = true;
    // The preview stays up until the new config arrives, so nothing flickers.
    ConfigService.SetTheme(spec).catch((e) => {
      settings.preview = null;
      toast.show(`Couldn't save theme: ${e}`);
    });
  }

  const themeEntries = $derived.by((): Entry[] => {
    const auto = themes.find((t) => t.name === (settings.scheme === 'dark' ? 'wtf Dark' : 'wtf Light'));
    return [
      { key: 'auto', label: 'Auto', detail: 'wtf Dark or wtf Light, following the system', theme: auto, section: 'Themes', run: () => commitTheme('auto') },
      ...themes.map((t): Entry => ({
        key: `theme:${t.name}`,
        label: t.name,
        detail: (t.dark ? 'Dark' : 'Light') + (t.builtin ? '' : ' · from themes folder'),
        theme: t,
        section: 'Themes',
        run: () => commitTheme(t.name),
      })),
    ];
  });

  let input = $state<HTMLInputElement>();
  let list = $state<HTMLDivElement>();
  let query = $state('');
  let selected = $state(0);

  const entries = $derived.by((): Entry[] => {
    if (mode === 'themes') return themeEntries;
    const out: Entry[] = [];
    for (const tab of tabs.list) {
      const pane = focusedPane(tab);
      const group = tabs.group(tab.groupId);
      out.push({
        key: `tab${tab.id}`,
        label: paneTitle(pane, prettyPath),
        detail: [group?.name, prettyPath(pane.cwd)].filter(Boolean).join(' · '),
        icon: iconForProcess(pane.process, pane.isShell),
        color: group?.color,
        section: 'Tabs',
        run: () => tabs.activate(tab.id),
      });
    }
    for (const g of tabs.groups) {
      out.push({
        key: `group${g.id}`,
        label: `${g.collapsed ? 'Expand' : 'Collapse'} ${g.name || 'unnamed group'}`,
        detail: `${tabs.tabsIn(g.id).length} tabs`,
        color: g.color,
        section: 'Groups',
        run: () => tabs.toggleCollapsed(g.id),
      });
    }
    for (const c of commands) {
      if (c.id === 'palette' || (c.available && !c.available())) continue;
      out.push({ key: c.id, label: c.label, hint: shortcutLabel(c), icon: 'command', section: 'Commands', run: c.run });
    }
    return out;
  });

  /** With no query: commands first. Otherwise: best matches first. */
  const results = $derived.by(() => {
    if (!query.trim() && mode === 'themes') return entries.map((e) => ({ e, indices: [] as number[] }));
    if (!query.trim()) {
      const order = { Commands: 0, Tabs: 1, Groups: 2, Themes: 3 };
      return [...entries].sort((a, b) => order[a.section] - order[b.section]).map((e) => ({ e, indices: [] as number[] }));
    }
    return entries
      .map((e) => ({ e, m: fuzzyMatch(query, e.label) }))
      .filter((r) => r.m)
      .sort((a, b) => b.m!.score - a.m!.score)
      .map((r) => ({ e: r.e, indices: r.m!.indices }));
  });

  $effect(() => {
    query;
    selected = 0;
  });

  onMount(() => {
    input?.focus();
    if (mode === 'themes') {
      ConfigService.Themes().then(async (all) => {
        themes = all;
        await tick();
        // Start on the theme in use.
        const spec = settings.config.theme.trim().toLowerCase();
        const current = spec === 'auto' || spec === '' ? 'auto' : `theme:${settings.theme.name}`;
        selected = Math.max(0, results.findIndex((r) => r.e.key === current));
        move(0); // scroll it into view
      });
    }
    return () => {
      if (!committed) settings.preview = null;
    };
  });

  // Browsing themes previews them on the whole window.
  $effect(() => {
    if (mode !== 'themes') return;
    settings.preview = results[selected]?.e.theme ?? null;
  });

  async function move(delta: number) {
    const n = results.length;
    if (!n) return;
    selected = (selected + delta + n) % n;
    await tick();
    list?.querySelector('.row.selected')?.scrollIntoView({ block: 'nearest' });
  }

  function choose(i: number) {
    const r = results[i];
    if (!r) return;
    overlay.close();
    r.e.run();
  }

  function onKeydown(e: KeyboardEvent) {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        move(1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        move(-1);
        break;
      case 'Enter':
        e.preventDefault();
        choose(selected);
        break;
      case 'Escape':
        e.preventDefault();
        e.stopPropagation();
        overlay.close();
        break;
    }
  }

  /** Splits a label into plain and highlighted runs. */
  function segments(text: string, indices: number[]) {
    const hit = new Set(indices);
    const out: { text: string; hit: boolean }[] = [];
    for (let i = 0; i < text.length; i++) {
      const h = hit.has(i);
      const last = out.at(-1);
      if (last && last.hit === h) last.text += text[i];
      else out.push({ text: text[i], hit: h });
    }
    return out;
  }

  function drop(_node: HTMLElement) {
    return {
      duration: 140,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: translateX(-50%) scale(${0.98 + 0.02 * t}) translateY(${(1 - t) * -6}px);`,
    };
  }
</script>

<div class="backdrop" role="presentation" onpointerdown={() => overlay.close()}></div>

<div class="palette" role="dialog" aria-label="Command palette" in:drop>
  <div class="field">
    <Icon name="search" size={15} stroke={2} />
    <input
      bind:this={input}
      bind:value={query}
      placeholder={mode === 'themes' ? 'Search themes…' : 'Search commands and tabs…'}
      spellcheck="false"
      autocomplete="off"
      role="combobox"
      aria-expanded="true"
      aria-controls="palette-list"
      onkeydown={onKeydown}
    />
  </div>

  <div class="list" id="palette-list" role="listbox" bind:this={list}>
    {#each results as r, i (r.e.key)}
      {#if !query.trim() && r.e.section !== results[i - 1]?.e.section}
        <div class="section">{r.e.section}</div>
      {/if}
      <button
        class="row"
        class:selected={i === selected}
        role="option"
        aria-selected={i === selected}
        onpointermove={() => (selected = i)}
        onclick={() => choose(i)}
      >
        <span class="glyph" style:--group-color={r.e.color ? `var(--g-${r.e.color})` : null}>
          {#if r.e.section === 'Groups'}
            <span class="dot"></span>
          {:else if r.e.theme}
            {@const t = r.e.theme}
            <span class="swatch" style:background={t.background} style:color={t.palette[4]}>
              <span style:background={t.palette[1]}></span><span style:background={t.palette[2]}></span><span
                style:background={t.palette[4]}
              ></span>
            </span>
          {:else if r.e.icon}
            <Icon name={r.e.icon} size={13} stroke={1.9} />
          {/if}
        </span>
        <span class="label">
          {#each segments(r.e.label, r.indices) as seg}
            {#if seg.hit}<mark>{seg.text}</mark>{:else}{seg.text}{/if}
          {/each}
        </span>
        {#if r.e.detail}<span class="detail">{r.e.detail}</span>{/if}
        {#if r.e.hint}<kbd>{r.e.hint}</kbd>{/if}
      </button>
    {:else}
      <div class="empty">No matches</div>
    {/each}
  </div>
</div>

<style>
  .backdrop {
    --wails-draggable: no-drag;
    position: fixed;
    inset: 0;
    z-index: 100;
    background: var(--scrim);
  }

  .palette {
    --wails-draggable: no-drag;
    position: fixed;
    top: 72px;
    left: 50%;
    z-index: 101;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    width: min(600px, calc(100vw - 32px));
    max-height: min(460px, calc(100vh - 110px));
    border-radius: 14px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(30px) saturate(180%);
    backdrop-filter: blur(30px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
    overflow: hidden;
  }

  .field {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
    height: 50px;
    padding: 0 16px;
    border-bottom: 1px solid var(--hairline-strong);
    color: var(--text-muted);
  }
  input {
    flex: 1;
    height: 100%;
    border: 0;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 15px;
    -webkit-user-select: text;
    user-select: text;
  }
  input:focus {
    outline: none;
  }
  input::placeholder {
    color: var(--text-muted);
  }

  .list {
    overflow-y: auto;
    padding: 6px;
    scroll-padding: 6px;
  }

  .section {
    padding: 8px 10px 4px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    height: 34px;
    padding: 0 10px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: inherit;
    text-align: left;
    font-size: 13px;
  }
  .row.selected {
    background: var(--accent);
    color: var(--on-accent);
  }

  .glyph {
    display: grid;
    place-items: center;
    flex: none;
    width: 16px;
    color: var(--group-color, currentColor);
    opacity: 0.85;
  }
  .row.selected .glyph {
    color: inherit;
    opacity: 1;
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--group-color);
  }

  .swatch {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 1.5px;
    width: 18px;
    height: 14px;
    border-radius: 4px;
    box-shadow: inset 0 0 0 0.5px rgba(127, 127, 127, 0.45);
  }
  .swatch span {
    width: 3px;
    height: 3px;
    border-radius: 50%;
  }

  .label {
    flex: none;
    max-width: 60%;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  mark {
    background: none;
    color: var(--accent);
    font-weight: 650;
  }
  .row.selected mark {
    color: inherit;
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  .detail {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--text-muted);
    font-size: 12px;
  }
  .row.selected .detail {
    color: inherit;
    opacity: 0.75;
  }

  kbd {
    flex: none;
    margin-left: auto;
    padding: 1px 6px;
    border-radius: 5px;
    background: var(--identity);
    color: var(--text-muted);
    font: inherit;
    font-size: 11.5px;
  }
  .row.selected kbd {
    background: rgba(255, 255, 255, 0.2);
    color: inherit;
  }

  .empty {
    padding: 18px;
    text-align: center;
    color: var(--text-muted);
  }
</style>
