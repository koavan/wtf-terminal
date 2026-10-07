<script lang="ts">
  import type { Snippet } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { overlay } from '../lib/overlay.svelte';

  /** Positions its content at (x, y), nudged to stay inside the window. */
  let { x, y, label, children }: { x: number; y: number; label: string; children: Snippet } = $props();

  const MARGIN = 8;
  let el: HTMLDivElement;
  let left = $state(0);
  let top = $state(0);

  $effect(() => {
    const { width, height } = el.getBoundingClientRect();
    left = Math.max(MARGIN, Math.min(x, window.innerWidth - width - MARGIN));
    top = Math.max(MARGIN, Math.min(y, window.innerHeight - height - MARGIN));
  });

  function pop(_node: HTMLElement) {
    return {
      duration: 120,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: scale(${0.97 + 0.03 * t}) translateY(${(1 - t) * -3}px);`,
    };
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      overlay.close();
    }
  }
</script>

<svelte:window onkeydowncapture={onKeydown} onblur={() => overlay.close()} />

<!-- Swallows the click that dismisses the popover. -->
<div
  class="backdrop"
  role="presentation"
  onpointerdown={() => overlay.close()}
  oncontextmenu={(e) => {
    e.preventDefault();
    overlay.close();
  }}
></div>

<div
  class="popover"
  role="dialog"
  aria-label={label}
  bind:this={el}
  style:left="{left}px"
  style:top="{top}px"
  in:pop
>
  {@render children()}
</div>

<style>
  .backdrop {
    --wails-draggable: no-drag;
    position: fixed;
    inset: 0;
    z-index: 100;
  }

  .popover {
    --wails-draggable: no-drag;
    position: fixed;
    z-index: 101;
    min-width: 200px;
    padding: 5px;
    border-radius: 11px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(24px) saturate(180%);
    backdrop-filter: blur(24px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
    font-size: 13px;
    transform-origin: top left;
  }
</style>
