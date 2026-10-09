<script lang="ts">
  import type { Snippet } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { overlay } from '../lib/overlay.svelte';

  // The frame of the SSH admin dialogs: backdrop, card, title, buttons row.
  // Form controls inside get a shared look (.field, input, select, button
  // classes .primary/.secondary/.danger), so each dialog only lays out fields.

  let {
    title,
    label = '',
    subtitle = '',
    width = 440,
    children,
    buttons,
    onsubmit,
  }: {
    /** The heading; empty for a dialog that draws its own (then give a label). */
    title: string;
    label?: string;
    subtitle?: string;
    width?: number;
    children: Snippet;
    buttons: Snippet;
    /** ⌘↩ (and ↩ in a text field) runs this. */
    onsubmit?: () => void;
  } = $props();

  const id = `dlg-${Math.random().toString(36).slice(2)}`;

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      overlay.close();
    } else if (e.key === 'Enter' && onsubmit && (e.metaKey || e.ctrlKey || (e.target as HTMLElement).tagName === 'INPUT')) {
      e.preventDefault();
      onsubmit();
    }
  }

  function appear(_node: HTMLElement) {
    return {
      duration: 160,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: translate(-50%, -50%) scale(${0.96 + 0.04 * t});`,
    };
  }
</script>

<svelte:window onkeydowncapture={(e) => e.key === 'Escape' && onKeydown(e)} />

<div class="backdrop" role="presentation" onpointerdown={() => overlay.close()}></div>

<div
  class="dialog"
  style:width="min({width}px, calc(100vw - 32px))"
  role="dialog"
  aria-modal="true"
  aria-labelledby={title ? id : undefined}
  aria-label={title ? undefined : label}
  tabindex="-1"
  in:appear
  onkeydown={onKeydown}
>
  {#if title}<h2 {id}>{title}</h2>{/if}
  {#if subtitle}<p class="subtitle">{subtitle}</p>{/if}
  {@render children()}
  <div class="buttons">{@render buttons()}</div>
</div>

<style>
  .backdrop {
    --wails-draggable: no-drag;
    position: fixed;
    inset: 0;
    z-index: 110;
    background: var(--scrim);
  }

  .dialog {
    --wails-draggable: no-drag;
    position: fixed;
    top: 46%;
    left: 50%;
    z-index: 111;
    transform: translate(-50%, -50%);
    max-height: calc(100vh - 48px);
    overflow: auto;
    box-sizing: border-box;
    padding: 20px 20px 16px;
    border-radius: 14px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(30px) saturate(180%);
    backdrop-filter: blur(30px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
  }
  .dialog:focus {
    outline: none;
  }

  h2 {
    margin: 0;
    font-size: 15px;
    font-weight: 650;
  }
  .subtitle {
    margin: 2px 0 14px;
    color: var(--text-muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  .dialog :global(.field) {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
  }
  .dialog :global(.field > span) {
    color: var(--text-muted);
    font-size: 11.5px;
    font-weight: 600;
  }
  .dialog :global(.row) {
    display: flex;
    gap: 10px;
  }
  .dialog :global(.grow) {
    flex: 1;
    min-width: 0;
  }
  .dialog :global(small),
  .dialog :global(.hint) {
    color: var(--text-muted);
    font-size: 11px;
    line-height: 1.4;
  }
  .dialog :global(code),
  .dialog :global(.mono) {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11.5px;
    overflow-wrap: anywhere;
  }

  .dialog :global(input:not([type='checkbox']):not([type='radio'])),
  .dialog :global(select),
  .dialog :global(textarea) {
    width: 100%;
    box-sizing: border-box;
    height: 30px;
    padding: 0 9px;
    border: 1px solid var(--hairline);
    border-radius: 7px;
    background: var(--identity);
    color: var(--text);
    font: 12.5px 'JetBrains Mono Variable', monospace;
    -webkit-user-select: text;
    user-select: text;
  }
  .dialog :global(select) {
    font-family: inherit;
  }
  .dialog :global(input:focus),
  .dialog :global(select:focus) {
    outline: none;
    border-color: var(--accent);
  }
  .dialog :global(input::placeholder) {
    color: var(--text-muted);
    opacity: 0.7;
  }
  .dialog :global(.check) {
    display: flex;
    align-items: center;
    gap: 7px;
    margin: 4px 0 10px;
    font-size: 12.5px;
  }

  .dialog :global(.command) {
    margin: 4px 0 0;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--identity);
    font: 11.5px/1.45 'JetBrains Mono Variable', monospace;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .dialog :global(.error) {
    margin: 8px 0 0;
    padding: 8px 10px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
    font-size: 12px;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 16px;
  }
  .buttons :global(.spacer) {
    flex: 1;
  }
  .dialog :global(button.primary),
  .dialog :global(button.secondary),
  .dialog :global(button.danger) {
    height: 30px;
    padding: 0 14px;
    border: 0;
    border-radius: 8px;
    font-size: 13px;
    font-weight: 550;
    white-space: nowrap;
  }
  .dialog :global(button:disabled) {
    opacity: 0.5;
  }
  .dialog :global(button.secondary) {
    background: var(--identity);
    color: var(--text);
  }
  .dialog :global(button.secondary:hover:not(:disabled)) {
    background: var(--tab-hover-strong);
  }
  .dialog :global(button.primary) {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 650;
  }
  .dialog :global(button.primary:hover:not(:disabled)) {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
  }
  .dialog :global(button.danger) {
    padding: 0 10px;
    background: transparent;
    color: var(--danger);
  }
  .dialog :global(button.danger:hover:not(:disabled)) {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }
</style>
