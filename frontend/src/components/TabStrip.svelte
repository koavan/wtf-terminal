<script lang="ts">
  import { tick } from 'svelte';
  import { flip } from 'svelte/animate';
  import { cubicOut } from 'svelte/easing';
  import Icon, { iconForProcess } from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { closeTab, closeTabs } from '../lib/closing';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { focusedPane, paneTitle, tabs, type Group, type Tab } from '../lib/tabs.svelte';
  import { moveTabToNewWindow, moveTabToWindow, otherWindows } from '../lib/window';

  type Item =
    | { kind: 'chip'; key: string; group: Group; count: number }
    | { kind: 'tab'; key: string; tab: Tab; group: Group | undefined; groupEnd: boolean };

  /** The strip as rendered: a chip in front of each group, collapsed tabs left out. */
  const items = $derived.by(() => {
    const out: Item[] = [];
    const list = tabs.list;
    list.forEach((tab, i) => {
      const group = tabs.group(tab.groupId);
      if (group && list[i - 1]?.groupId !== group.id) {
        out.push({ kind: 'chip', key: `g${group.id}`, group, count: tabs.tabsIn(group.id).length });
      }
      if (!group?.collapsed) {
        const groupEnd = !!group && list[i + 1]?.groupId !== group.id;
        out.push({ kind: 'tab', key: `t${tab.id}`, tab, group, groupEnd });
      }
    });
    return out;
  });

  interface Drag {
    id: number;
    el: HTMLElement;
    startX: number;
    /** Pointer offset from the tab's left edge. */
    grabX: number;
    offset: number;
    moved: boolean;
    startY: number;
    /** Pulled far enough out of the strip that letting go opens a new window. */
    tearing: boolean;
    offsetY: number;
  }

  /** How far below the strip a tab must be pulled to tear off. */
  const TEAR_DISTANCE = 56;

  let stripEl: HTMLDivElement;
  let drag = $state<Drag | null>(null);

  const hint = (mac: string, other: string) => (isMac ? mac : other);
  const newTabHint = `New Tab (${hint('⌘T', 'Ctrl+Shift+T')})`;

  function onPointerDown(e: PointerEvent, tab: Tab) {
    if (e.button !== 0 || (e.target as HTMLElement).closest('.close')) return;
    tabs.activate(tab.id);
    const el = e.currentTarget as HTMLElement;
    drag = {
      id: tab.id,
      el,
      startX: e.clientX,
      grabX: e.clientX - el.getBoundingClientRect().left,
      offset: 0,
      moved: false,
      startY: e.clientY,
      tearing: false,
      offsetY: 0,
    };
    el.setPointerCapture(e.pointerId);
  }

  // Positions use offsetLeft, which ignores transforms, so the reorder
  // animations of the other tabs don't disturb the hit-testing.
  async function onPointerMove(e: PointerEvent) {
    const d = drag;
    if (!d) return;
    if (!d.moved && Math.abs(e.clientX - d.startX) < 4 && Math.abs(e.clientY - d.startY) < 4) return;
    d.moved = true;

    // Pulled out of the strip (down, or outside the window): tear off.
    const strip = stripEl.getBoundingClientRect();
    const outside = e.clientX < 0 || e.clientX > window.innerWidth || e.clientY < 0;
    d.tearing = tabs.list.length > 1 && (outside || e.clientY > strip.bottom + TEAR_DISTANCE);
    d.offsetY = d.tearing ? e.clientY - d.startY : 0;
    if (d.tearing) {
      d.offset = e.clientX - d.startX;
      return;
    }

    const maxLeft = stripEl.scrollWidth - d.el.offsetWidth;
    const left = Math.max(0, Math.min(e.clientX - stripEl.getBoundingClientRect().left - d.grabX, maxLeft));
    const center = left + d.el.offsetWidth / 2;

    let before: number | null = null;
    for (const el of stripEl.querySelectorAll<HTMLElement>('.tab')) {
      if (el === d.el) continue;
      if (el.offsetLeft + el.offsetWidth / 2 > center) {
        before = Number(el.dataset.id);
        break;
      }
    }
    tabs.moveBefore(d.id, before);

    await tick();
    if (drag === d) d.offset = left - d.el.offsetLeft;
  }

  function endDrag(e: PointerEvent) {
    const d = drag;
    drag = null;
    if (d?.tearing && e.type === 'pointerup') {
      // Place the new window so the tab lands under the pointer.
      moveTabToNewWindow(d.id, e.screenX - d.grabX - 90, e.screenY - 20);
    }
  }

  function onAuxClick(e: MouseEvent, tab: Tab) {
    if (e.button === 1) closeTab(tab.id); // middle-click closes, like a browser
  }

  function groupLabel(g: Group) {
    return g.name || 'Unnamed group';
  }

  async function tabMenu(e: MouseEvent, tab: Tab) {
    e.preventDefault();
    const { clientX, clientY } = e;
    const windows = await otherWindows();
    const index = tabs.list.indexOf(tab);
    const others = tabs.groups.filter((g) => g.id !== tab.groupId);
    const entries: MenuEntry[] = [
      { label: 'New tab to the right', action: () => tabs.openNextTo(tab.id) },
      'separator',
      {
        label: 'Add tab to new group',
        hint: hint('⌥⌘G', 'Ctrl+Shift+G'),
        action: () => {
          const g = tabs.createGroup(tab.id);
          if (g) overlay.openGroupEditor(g.id);
        },
      },
    ];
    if (others.length) {
      entries.push({ heading: 'Add tab to group' });
      for (const g of others) {
        entries.push({ label: groupLabel(g), color: g.color, action: () => tabs.addToGroup(tab.id, g.id) });
      }
    }
    if (tab.groupId !== null) {
      entries.push({ label: 'Remove from group', action: () => tabs.removeFromGroup(tab.id) });
    }
    entries.push(
      'separator',
      { label: 'Duplicate', action: () => tabs.openNextTo(tab.id, true) },
    );
    if (tabs.list.length > 1) {
      entries.push('separator', { label: 'Move to new window', action: () => moveTabToNewWindow(tab.id) });
    }
    if (windows.length) {
      if (tabs.list.length === 1) entries.push('separator');
      for (const w of windows) {
        entries.push({ label: `Move to “${w.title || w.name}”`, action: () => moveTabToWindow(tab.id, w.name) });
      }
    }
    entries.push('separator', { label: 'Close tab', hint: hint('⌥⌘W', ''), action: () => closeTab(tab.id) });
    if (tabs.list.length > 1) {
      entries.push({
        label: 'Close other tabs',
        action: () => closeTabs(tabs.list.filter((t) => t.id !== tab.id).map((t) => t.id)),
      });
    }
    if (index < tabs.list.length - 1) {
      entries.push({
        label: 'Close tabs to the right',
        action: () => closeTabs(tabs.list.slice(index + 1).map((t) => t.id)),
      });
    }
    overlay.openMenu(clientX, clientY, entries);
  }

  function chipMenu(e: MouseEvent, group: Group) {
    e.preventDefault();
    overlay.openGroupEditor(group.id);
  }

  /** Tabs open and close by growing/shrinking in place. */
  function grow(node: HTMLElement) {
    const width = node.getBoundingClientRect().width;
    return {
      duration: 180,
      easing: cubicOut,
      css: (t: number) => `max-width: ${t * width}px; min-width: 0; opacity: ${t};`,
    };
  }
</script>

<div class="tabs" role="tablist" bind:this={stripEl}>
  {#each items as item (item.key)}
    <div
      class="slot"
      class:tab-slot={item.kind === 'tab'}
      animate:flip={{ duration: item.kind === 'tab' && drag?.id === item.tab.id ? 0 : 200, easing: cubicOut }}
      in:grow
      out:grow
    >
      {#if item.kind === 'chip'}
        {@const g = item.group}
        <button
          class="chip"
          class:collapsed={g.collapsed}
          class:unnamed={!g.name}
          data-group-chip={g.id}
          style:--group-color="var(--g-{g.color})"
          title="{groupLabel(g)} — {item.count} {item.count === 1 ? 'tab' : 'tabs'}"
          onclick={() => tabs.toggleCollapsed(g.id)}
          oncontextmenu={(e) => chipMenu(e, g)}
        >
          {#if g.name}<span class="chip-name">{g.name}</span>{/if}
          {#if g.collapsed}<span class="chip-count">{item.count}</span>{/if}
        </button>
      {:else}
        {@const tab = item.tab}
        {@const pane = focusedPane(tab)}
        {@const dragging = drag?.id === tab.id && drag.moved}
        <div
          class="tab"
          class:active={tab.id === tabs.activeId}
          class:dragging
          class:grouped={!!item.group}
          class:group-end={item.groupEnd}
          style:--group-color={item.group ? `var(--g-${item.group.color})` : null}
          class:tearing={dragging && drag!.tearing}
          style:transform={dragging ? `translate(${drag!.offset}px, ${drag!.offsetY}px)` : null}
          data-id={tab.id}
          role="tab"
          tabindex="-1"
          aria-selected={tab.id === tabs.activeId}
          title={pane.oscTitle || prettyPath(pane.cwd)}
          onpointerdown={(e) => onPointerDown(e, tab)}
          onpointermove={onPointerMove}
          onpointerup={endDrag}
          onpointercancel={endDrag}
          onauxclick={(e) => onAuxClick(e, tab)}
          oncontextmenu={(e) => tabMenu(e, tab)}
        >
          <span class="icon" class:activity={tab.activity} class:ok={tab.finished === 'ok'} class:fail={tab.finished === 'fail'}>
            <Icon name={iconForProcess(pane.process, pane.isShell)} size={13} />
          </span>
          <span class="title">{paneTitle(pane, prettyPath)}</span>
          <button
            class="close"
            aria-label="Close tab"
            onclick={(e) => {
              e.stopPropagation();
              closeTab(tab.id);
            }}
          >
            <Icon name="x" size={11} stroke={2.25} />
          </button>
        </div>
      {/if}
    </div>
  {/each}

  <button class="new-tab" title={newTabHint} aria-label="New tab" onclick={() => tabs.open()}>
    <Icon name="plus" size={15} stroke={2} />
  </button>
</div>

<style>
  .tabs {
    position: relative; /* offsetParent for drag hit-testing */
    display: flex;
    align-items: center;
    gap: var(--tab-gap);
    min-width: 0;
    flex: 1;
    height: 100%;
  }

  /* One slot per chip or tab: the unit that is laid out and animated. */
  .slot {
    display: flex;
    flex: none;
  }
  .slot.tab-slot {
    flex: 0 1 var(--tab-width);
    min-width: 44px;
  }

  /* ── Tabs ─────────────────────────────────────────────────────────── */

  .tab {
    --wails-draggable: no-drag;
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-width: 0;
    height: var(--tab-height);
    padding: 0 6px 0 11px;
    border-radius: var(--radius-tab);
    color: var(--chrome-muted);
    font-size: 12.5px;
    font-weight: 500;
    letter-spacing: -0.005em;
    cursor: default;
    transition:
      background-color 140ms ease,
      color 140ms ease,
      box-shadow 140ms ease,
      transform 180ms cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .tab:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tab.active {
    background: var(--tab-active);
    color: var(--chrome-text);
    box-shadow: var(--tab-active-shadow);
  }
  .tab.dragging {
    transition: none;
    z-index: 1;
    box-shadow: var(--tab-drag-shadow);
  }
  /* Lifted out of the strip: letting go opens it in a new window. */
  .tab.tearing {
    z-index: 50;
    opacity: 0.92;
    background: var(--popover-solid);
    color: var(--chrome-text);
    box-shadow: var(--popover-shadow);
    scale: 1.03;
  }

  /* Group members share a colored line that runs under the chip and tabs. */
  .tab.grouped::after {
    content: '';
    position: absolute;
    left: 0;
    right: calc(-1 * var(--tab-gap));
    bottom: var(--group-line-offset);
    height: 2px;
    background: var(--group-color);
    pointer-events: none;
  }
  .tab.grouped.group-end::after {
    right: 6px;
    border-radius: 0 2px 2px 0;
  }
  .tab.grouped.active {
    background: color-mix(in srgb, var(--group-color) 14%, var(--tab-active));
  }
  .tab.grouped.dragging::after {
    display: none;
  }

  .icon {
    position: relative;
    display: grid;
    place-items: center;
    flex: none;
    opacity: 0.8;
  }
  .tab.active .icon {
    color: var(--accent);
    opacity: 1;
  }
  .tab.grouped.active .icon {
    color: var(--group-color);
  }
  /* Background tab produced output. */
  .icon.activity::after {
    content: '';
    position: absolute;
    top: -3px;
    right: -4px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 2px var(--chrome-solid);
  }

  /* A command finished in the background: the dot says how it went. */
  .icon.ok::after,
  .icon.fail::after {
    content: '';
    position: absolute;
    top: -3px;
    right: -4px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    box-shadow: 0 0 0 2px var(--chrome-solid);
  }
  .icon.ok::after {
    background: var(--mark-ok);
  }
  .icon.fail::after {
    background: var(--danger);
  }

  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    /* Fade long titles out instead of an ellipsis, like Chrome. */
    mask-image: linear-gradient(to right, #000 calc(100% - 18px), transparent);
  }

  .close {
    --wails-draggable: no-drag;
    display: grid;
    place-items: center;
    flex: none;
    width: 18px;
    height: 18px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: inherit;
    opacity: 0;
    transition: opacity 120ms ease, background-color 120ms ease;
  }
  .tab:hover .close,
  .tab.active .close {
    opacity: 0.7;
  }
  .close:hover {
    opacity: 1 !important;
    background: var(--tab-hover-strong);
  }

  /* ── Group chips ──────────────────────────────────────────────────── */

  .chip {
    --wails-draggable: no-drag;
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    max-width: 160px;
    height: 22px;
    padding: 0 9px;
    border: 0;
    border-radius: 7px;
    background: var(--group-color);
    color: var(--group-ink);
    font-size: 11.5px;
    font-weight: 650;
    letter-spacing: 0.005em;
    transition: filter 120ms ease, transform 120ms ease;
  }
  .chip:hover {
    filter: brightness(1.08);
  }
  .chip:active {
    transform: scale(0.96);
  }
  .chip.unnamed:not(.collapsed) {
    width: 14px;
    height: 14px;
    padding: 0;
    margin: 0 4px;
    border-radius: 50%;
  }
  /* The group line starts under the chip. */
  .chip:not(.collapsed)::after {
    content: '';
    position: absolute;
    left: 50%;
    right: calc(-1 * var(--tab-gap));
    bottom: calc(var(--group-line-offset) - (var(--tab-height) - 22px) / 2);
    height: 2px;
    border-radius: 2px 0 0 2px;
    background: var(--group-color);
  }
  .chip.unnamed:not(.collapsed)::after {
    bottom: calc(var(--group-line-offset) - (var(--tab-height) - 14px) / 2);
    right: calc(-1 * var(--tab-gap) - 4px);
  }
  .chip-name {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .chip-count {
    display: grid;
    place-items: center;
    min-width: 15px;
    height: 15px;
    padding: 0 4px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--group-ink) 18%, transparent);
    font-size: 10.5px;
    font-variant-numeric: tabular-nums;
  }

  /* ── New tab ──────────────────────────────────────────────────────── */

  .new-tab {
    --wails-draggable: no-drag;
    display: grid;
    place-items: center;
    flex: none;
    width: 28px;
    height: 28px;
    margin-left: 2px;
    padding: 0;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--chrome-muted);
    transition: background-color 120ms ease, color 120ms ease;
  }
  .new-tab:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .new-tab:active {
    background: var(--tab-hover-strong);
  }
</style>
