<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { Service, type SecretEntry } from '../../bindings/github.com/dayanraj/wtf/internal/kube';
  import Dialog from './Dialog.svelte';
  import Icon from './Icon.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { copySecret } from '../lib/clipboard';
  import { toast } from '../lib/toast.svelte';

  // A secret's decoded values. Fetched when the dialog opens, held only in
  // this component (gone when it closes), masked until revealed key by key,
  // and masked again whenever the window loses focus — for screen sharing.

  let { request }: { request: { name: string; context: string; namespace: string; env: Record<string, string> } } = $props();
  const r = untrack(() => request);

  let entries = $state.raw<SecretEntry[] | null>(null);
  let error = $state('');
  const shown = new SvelteSet<string>();

  onMount(() => {
    Service.SecretValues({ env: r.env, context: r.context, namespace: r.namespace }, r.name)
      .then((e) => (entries = e ?? []))
      .catch((e) => (error = e instanceof Error ? e.message : String(e)));
    const hide = () => shown.clear();
    window.addEventListener('blur', hide);
    return () => {
      window.removeEventListener('blur', hide);
      entries = null; // drop the values with the dialog
    };
  });

  const allShown = $derived(!!entries?.length && entries.every((e) => shown.has(e.key)));

  function toggle(key: string) {
    if (shown.has(key)) shown.delete(key);
    else shown.add(key);
  }

  function toggleAll() {
    if (allShown) shown.clear();
    else for (const e of entries ?? []) shown.add(e.key);
  }

  function copy(e: SecretEntry) {
    copySecret(e.value, e.binary ? `${e.key} (base64)` : e.key);
  }

  const size = (n: number) => (n < 1024 ? `${n} B` : `${(n / 1024).toFixed(1)} KB`);
</script>

<Dialog title="Secret {r.name}" subtitle="Context {r.context}, namespace {r.namespace}. Values hide again when wtf loses focus." width={560}>
  {#if error}
    <div class="error" role="alert">{error}</div>
  {:else if !entries}
    <p class="hint">Reading the secret…</p>
  {:else if entries.length === 0}
    <p class="hint">This secret has no data.</p>
  {:else}
    <ul class="entries">
      {#each entries as e (e.key)}
        {@const open = shown.has(e.key)}
        <li>
          <div class="head">
            <span class="key">{e.key}</span>
            <span class="meta">{e.binary ? `binary · ${size(e.size)}` : size(e.size)}</span>
            <button class="icon" title={open ? 'Hide' : 'Reveal'} aria-label="{open ? 'Hide' : 'Reveal'} {e.key}" onclick={() => toggle(e.key)}>
              <Icon name={open ? 'eyeOff' : 'eye'} size={13} />
            </button>
            <button class="icon" title={e.binary ? 'Copy (base64)' : 'Copy'} aria-label="Copy {e.key}" onclick={() => copy(e)}>
              <Icon name="copy" size={13} />
            </button>
          </div>
          {#if open}
            <pre class="value">{e.value}</pre>
          {:else}
            <div class="masked" aria-label="hidden">••••••••</div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#snippet buttons()}
    {#if entries?.length}
      <button class="secondary" onclick={toggleAll}>{allShown ? 'Hide All' : 'Reveal All'}</button>
    {/if}
    <span class="spacer"></span>
    <button class="primary" onclick={() => overlay.close()}>Done</button>
  {/snippet}
</Dialog>

<style>
  .entries {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 60vh;
    overflow: auto;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .key {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    font: 600 12px 'JetBrains Mono Variable', monospace;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    color: var(--text-muted);
    font-size: 11px;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
  }
  .icon:hover {
    background: var(--tab-hover);
    color: var(--text);
  }
  .value,
  .masked {
    margin: 3px 0 0;
    padding: 6px 9px;
    border-radius: 7px;
    background: var(--identity);
    font: 12px/1.45 'JetBrains Mono Variable', monospace;
  }
  .value {
    max-height: 30vh;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .masked {
    color: var(--text-muted);
    letter-spacing: 2px;
  }
</style>
