<script lang="ts">
  import TerminalPane from './TerminalPane.svelte';
  import { clampRatio, place, type Divider, type Rect } from '../lib/layout';
  import { isMac } from '../lib/platform';
  import type { Tab } from '../lib/tabs.svelte';

  let { tab, visible }: { tab: Tab; visible: boolean } = $props();

  const FULL: Rect = { x: 0, y: 0, w: 1, h: 1 };

  const layout = $derived(place(tab.root));
  const split = $derived(layout.panes.length > 1);
  // Render in a stable order (by id) so splitting never moves an existing
  // terminal's DOM node; position comes from the layout instead.
  const ordered = $derived([...layout.panes].sort((a, b) => a.pane.id - b.pane.id));

  let el: HTMLDivElement;
  let resizing = $state<Divider | null>(null);

  function startResize(e: PointerEvent, d: Divider) {
    if (e.button !== 0) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    resizing = d;
  }

  function onResizeMove(e: PointerEvent) {
    if (!resizing) return;
    const box = el.getBoundingClientRect();
    const { split, rect } = resizing;
    split.ratio =
      split.dir === 'row'
        ? clampRatio(((e.clientX - box.left) / box.width - rect.x) / rect.w)
        : clampRatio(((e.clientY - box.top) / box.height - rect.y) / rect.h);
  }

  function pct(n: number) {
    return `${n * 100}%`;
  }
</script>

<div class="tab-panes" class:visible class:resizing={!!resizing} bind:this={el}>
  {#each ordered as p (p.pane.id)}
    {@const hidden = tab.zoomedPaneId !== null && tab.zoomedPaneId !== p.pane.id}
    {@const r = tab.zoomedPaneId === p.pane.id ? FULL : p.rect}
    <div class="slot" class:hidden style:left={pct(r.x)} style:top={pct(r.y)} style:width={pct(r.w)} style:height={pct(r.h)}>
      <TerminalPane
        pane={p.pane}
        tabId={tab.id}
        visible={visible && !hidden}
        focused={visible && !hidden && tab.focusedPaneId === p.pane.id}
        dimmed={split && tab.zoomedPaneId === null && tab.focusedPaneId !== p.pane.id}
      />
    </div>
  {/each}

  {#if tab.zoomedPaneId === null}
    {#each layout.dividers as d (d.split)}
      {@const s = d.split}
      {@const at = s.dir === 'row' ? d.rect.x + d.rect.w * s.ratio : d.rect.y + d.rect.h * s.ratio}
      <div
        class="divider {s.dir}"
        class:active={resizing?.split === s}
        role="separator"
        aria-orientation={s.dir === 'row' ? 'vertical' : 'horizontal'}
        style:left={s.dir === 'row' ? pct(at) : pct(d.rect.x)}
        style:top={s.dir === 'row' ? pct(d.rect.y) : pct(at)}
        style:width={s.dir === 'row' ? null : pct(d.rect.w)}
        style:height={s.dir === 'row' ? pct(d.rect.h) : null}
        onpointerdown={(e) => startResize(e, d)}
        onpointermove={onResizeMove}
        onpointerup={() => (resizing = null)}
        onpointercancel={() => (resizing = null)}
        ondblclick={() => (s.ratio = 0.5)}
      ></div>
    {/each}
  {:else}
    <div class="zoom-badge">Zoomed · {isMac ? '⌘⇧↩' : 'Ctrl+Shift+Enter'} to restore</div>
  {/if}
</div>

<style>
  .tab-panes {
    position: absolute;
    inset: 0;
    visibility: hidden;
  }
  .tab-panes.visible {
    visibility: visible;
  }
  .tab-panes.resizing {
    cursor: inherit;
  }

  .slot {
    position: absolute;
  }
  .slot.hidden {
    visibility: hidden;
  }

  /* A wide invisible grab area around a hairline. */
  .divider {
    position: absolute;
    z-index: 3;
  }
  .divider.row {
    width: 9px;
    transform: translateX(-50%);
    cursor: col-resize;
  }
  .divider.column {
    height: 9px;
    transform: translateY(-50%);
    cursor: row-resize;
  }
  .divider::after {
    content: '';
    position: absolute;
    background: var(--hairline-strong);
    transition: background-color 120ms ease;
  }
  .divider.row::after {
    top: 0;
    bottom: 0;
    left: 4px;
    width: 1px;
  }
  .divider.column::after {
    left: 0;
    right: 0;
    top: 4px;
    height: 1px;
  }
  .divider:hover::after,
  .divider.active::after {
    background: var(--accent);
  }
  .divider.row:hover::after,
  .divider.row.active::after {
    left: 3.5px;
    width: 2px;
  }
  .divider.column:hover::after,
  .divider.column.active::after {
    top: 3.5px;
    height: 2px;
  }

  .zoom-badge {
    position: absolute;
    bottom: 10px;
    left: 50%;
    z-index: 3;
    transform: translateX(-50%);
    padding: 4px 10px;
    border-radius: 999px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(20px);
    backdrop-filter: blur(20px);
    box-shadow: var(--popover-shadow);
    color: var(--text-muted);
    font-size: 11.5px;
    pointer-events: none;
  }
</style>
