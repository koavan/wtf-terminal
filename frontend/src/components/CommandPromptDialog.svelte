<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import { overlay, type CommandPrompt } from '../lib/overlay.svelte';
  import { tabs } from '../lib/tabs.svelte';

  // Fills in a command, shows it exactly, and runs it in a new tab when
  // confirmed. The preview is the confirmation.

  let { request }: { request: CommandPrompt } = $props();
  const r = untrack(() => request);
  let values = $state(r.fields.map((f) => f.value));
  let first = $state<HTMLInputElement>();

  onMount(() => first?.select());

  const result = $derived.by(() => {
    try {
      return { command: r.command(values), error: '' };
    } catch (e) {
      return { command: '', error: e instanceof Error ? e.message : String(e) };
    }
  });

  let busy = $state(false);
  let failed = $state('');

  async function run() {
    if (!result.command || busy) return;
    if (r.submit) {
      busy = true;
      failed = '';
      try {
        await r.submit(values);
        overlay.close();
      } catch (e) {
        failed = e instanceof Error ? e.message : String(e);
      } finally {
        busy = false;
      }
      return;
    }
    const command = result.command;
    overlay.close();
    tabs.open({ command });
  }
</script>

<Dialog title={r.title} subtitle={r.message} onsubmit={run}>
  <div class="row">
    {#each r.fields as f, i (f.label)}
      <label class="field grow">
        <span>{f.label}</span>
        {#if i === 0}
          <input bind:this={first} bind:value={values[i]} placeholder={f.placeholder} spellcheck="false" autocomplete="off" />
        {:else}
          <input bind:value={values[i]} placeholder={f.placeholder} spellcheck="false" autocomplete="off" />
        {/if}
      </label>
    {/each}
  </div>
  {#if result.command}
    <div class="command">{result.command}</div>
    <p class="hint">{r.note ?? (r.submit ? 'wtf runs this.' : 'Runs in a new tab.')}</p>
  {:else if result.error}
    <div class="error" role="alert">{result.error}</div>
  {/if}
  {#if failed}<div class="error" role="alert">{failed}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={run} disabled={!result.command || busy}>{r.confirmLabel}</button>
  {/snippet}
</Dialog>
