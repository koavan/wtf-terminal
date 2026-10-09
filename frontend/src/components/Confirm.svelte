<script lang="ts">
  import { onMount } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { overlay, type ConfirmRequest } from '../lib/overlay.svelte';

  let { request }: { request: ConfirmRequest } = $props();
  let confirmButton = $state<HTMLButtonElement>();

  onMount(() => confirmButton?.focus());

  function confirm() {
    // `request` is read live from overlay state, which closing clears.
    const { onConfirm } = request;
    overlay.close();
    onConfirm();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      overlay.close();
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

<svelte:window onkeydowncapture={onKeydown} />

<div class="backdrop" role="presentation" onpointerdown={() => overlay.close()}></div>

<div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" in:appear>
  <h2 id="confirm-title">{request.title}</h2>
  <p>{request.message}</p>
  {#if request.items.length}
    <ul>
      {#each request.showAll ? request.items : request.items.slice(0, 6) as item, i (i)}
        <li>{item}</li>
      {/each}
      {#if !request.showAll && request.items.length > 6}<li class="more">and {request.items.length - 6} more</li>{/if}
    </ul>
  {/if}
  <div class="buttons">
    <button class="cancel" onclick={() => overlay.close()}>Cancel</button>
    <button class="danger" bind:this={confirmButton} onclick={confirm}>{request.confirmLabel}</button>
  </div>
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
    top: 42%;
    left: 50%;
    z-index: 111;
    transform: translate(-50%, -50%);
    width: min(400px, calc(100vw - 32px));
    padding: 20px 20px 16px;
    border-radius: 14px;
    background: var(--popover);
    -webkit-backdrop-filter: blur(30px) saturate(180%);
    backdrop-filter: blur(30px) saturate(180%);
    box-shadow: var(--popover-shadow);
    color: var(--text);
  }

  h2 {
    margin: 0 0 6px;
    font-size: 15px;
    font-weight: 650;
  }
  p {
    margin: 0;
    color: var(--text-muted);
    font-size: 13px;
    line-height: 1.45;
  }

  ul {
    margin: 12px 0 0;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--identity);
    list-style: none;
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11.5px;
    max-height: 50vh;
    overflow: auto;
    overflow-wrap: anywhere;
  }
  li {
    padding: 2px 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  li.more {
    color: var(--text-muted);
    font-family: inherit;
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 18px;
  }
  button {
    height: 30px;
    padding: 0 14px;
    border: 0;
    border-radius: 8px;
    font-size: 13px;
    font-weight: 550;
  }
  .cancel {
    background: var(--identity);
    color: var(--text);
  }
  .cancel:hover {
    background: var(--tab-hover-strong);
  }
  .danger {
    background: var(--danger);
    color: #fff;
  }
  .danger:hover {
    filter: brightness(1.08);
  }
  .danger:focus-visible {
    outline: 2px solid var(--danger);
    outline-offset: 2px;
  }
</style>
