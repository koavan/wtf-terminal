<script lang="ts">
  import KeyGenForm from './KeyGenForm.svelte';
  import PasswordGenerator from './PasswordGenerator.svelte';
  import { sidebar, type GenerateSection } from '../lib/sidebar.svelte';

  // The Generate tab: SSH key pairs and passwords, made in forms rather
  // than at a prompt.

  const SECTIONS: { id: GenerateSection; label: string }[] = [
    { id: 'key', label: 'SSH Key' },
    { id: 'password', label: 'Password' },
  ];
</script>

<div class="generate">
  <div class="sections" role="tablist" aria-label="Generate">
    {#each SECTIONS as s (s.id)}
      <button
        role="tab"
        aria-selected={sidebar.generateSection === s.id}
        class:on={sidebar.generateSection === s.id}
        onclick={() => (sidebar.generateSection = s.id)}
      >
        {s.label}
      </button>
    {/each}
  </div>

  <div class="body">
    {#if sidebar.generateSection === 'password'}
      <PasswordGenerator />
    {:else}
      <KeyGenForm />
    {/if}
  </div>
</div>

<style>
  .generate {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .sections {
    display: flex;
    gap: 2px;
    flex: none;
    margin: 0 8px 6px;
    padding: 2px;
    border-radius: 8px;
    background: var(--tab-hover);
  }
  .sections button {
    flex: 1;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
    font-size: 11.5px;
    font-weight: 550;
  }
  .sections button:hover:not(.on) {
    color: var(--chrome-text);
  }
  .sections button.on {
    background: var(--surface);
    color: var(--chrome-text);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.12);
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px 14px 16px;
    color: var(--chrome-text);
  }

  /* Form controls for both generators. */
  .body :global(.field) {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
  }
  .body :global(.field > span) {
    color: var(--chrome-muted);
    font-size: 11px;
    font-weight: 600;
  }
  .body :global(small),
  .body :global(.hint) {
    color: var(--chrome-muted);
    font-size: 11px;
    line-height: 1.45;
  }
  .body :global(code) {
    font: 11px 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
  }
  .body :global(input:not([type='checkbox']):not([type='range'])),
  .body :global(select) {
    box-sizing: border-box;
    height: 28px;
    padding: 0 8px;
    border: 1px solid var(--hairline);
    border-radius: 7px;
    background: var(--identity);
    color: var(--text);
    font: 12px 'JetBrains Mono Variable', monospace;
    -webkit-user-select: text;
    user-select: text;
  }
  .body :global(select) {
    font-family: inherit;
  }
  /* Full width only straight inside a field; rows of controls size their own. */
  .body :global(.field > input),
  .body :global(.field > select) {
    width: 100%;
  }
  .body :global(input:focus),
  .body :global(select:focus) {
    outline: none;
    border-color: var(--accent);
  }
  .body :global(input::placeholder) {
    color: var(--chrome-muted);
    opacity: 0.7;
  }
  .body :global(.check) {
    display: flex;
    align-items: center;
    gap: 7px;
    margin: 2px 0 8px;
    font-size: 12px;
  }
  .body :global(button.primary),
  .body :global(button.secondary) {
    height: 28px;
    padding: 0 12px;
    border: 0;
    border-radius: 7px;
    font-size: 12px;
    font-weight: 550;
    white-space: nowrap;
  }
  .body :global(button.primary) {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 650;
  }
  .body :global(button.primary:hover:not(:disabled)) {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
  }
  .body :global(button.secondary) {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .body :global(button:disabled) {
    opacity: 0.5;
  }
  .body :global(.link) {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font-size: 11.5px;
  }
  .body :global(.error) {
    margin: 8px 0;
    padding: 8px 10px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
    font-size: 11.5px;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .body :global(.box) {
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--identity);
    color: var(--text);
    font: 11.5px/1.45 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
    -webkit-user-select: text;
    user-select: text;
  }
  .body :global(.actions) {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 8px 0;
  }
</style>
