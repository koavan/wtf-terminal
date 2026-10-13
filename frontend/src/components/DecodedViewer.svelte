<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import Icon from './Icon.svelte';
  import { decodeBase64, JWT_RE } from '../lib/inspect';
  import { overlay } from '../lib/overlay.svelte';
  import { copySecret } from '../lib/clipboard';
  import { toast } from '../lib/toast.svelte';

  // Base64 from the terminal (a selection, or a value in
  // `kubectl get secret -o yaml`), decoded. Encoded values are often
  // secrets, so the result is masked until revealed and masked again when
  // wtf loses focus; it lives only in this dialog. A certificate or a JWT
  // inside can be opened in its own inspector.

  let { text }: { text: string } = $props();
  let decoded = $state.raw(untrack(() => decodeBase64(text)));
  let shown = $state(false);

  onMount(() => {
    const hide = () => (shown = false);
    window.addEventListener('blur', hide);
    return () => {
      window.removeEventListener('blur', hide);
      decoded = null;
    };
  });

  const KIND = { certificate: 'PEM certificate', jwt: 'JWT', json: 'JSON', text: 'Text', binary: 'Binary data' } as const;
  const size = (n: number) => (n < 1024 ? `${n} bytes` : `${(n / 1024).toFixed(1)} KB`);

  function copy() {
    if (!decoded || decoded.kind === 'binary') return;
    copySecret(decoded.text, 'decoded text');
  }
</script>

<Dialog title="Decoded Base64" subtitle="Hidden until you reveal it, and hidden again when wtf loses focus." width={600}>
  {#if !decoded}
    <div class="error" role="alert">This isn't base64 wtf can decode.</div>
  {:else}
    <div class="head">
      <span class="kind">{KIND[decoded.kind]}</span>
      <span class="meta">{size(decoded.size)}{decoded.kind === 'binary' && decoded.size > 256 ? ' · first 256 shown' : ''}</span>
      <span class="spacer"></span>
      <button class="icon" title={shown ? 'Hide' : 'Reveal'} aria-label={shown ? 'Hide' : 'Reveal'} onclick={() => (shown = !shown)}>
        <Icon name={shown ? 'eyeOff' : 'eye'} size={13} />
      </button>
    </div>
    {#if shown}
      <pre class="value" class:dump={decoded.kind === 'binary'}>{decoded.text}</pre>
    {:else}
      <button class="masked" onclick={() => (shown = true)}>•••••••• &nbsp;Reveal</button>
    {/if}
  {/if}

  {#snippet buttons()}
    {#if decoded?.kind === 'certificate'}
      {@const pem = decoded.text}
      <button class="secondary" onclick={() => overlay.show({ kind: 'cert', source: { text: pem } })}>Inspect Certificate…</button>
    {:else if decoded?.kind === 'jwt'}
      {@const jwt = decoded.text.match(JWT_RE)?.[0] ?? ''}
      <button class="secondary" onclick={() => overlay.show({ kind: 'jwt', token: jwt })}>Inspect JWT…</button>
    {/if}
    {#if decoded && decoded.kind !== 'binary'}
      <button class="secondary" onclick={copy}>Copy</button>
    {/if}
    <span class="spacer"></span>
    <button class="primary" onclick={() => overlay.close()}>Done</button>
  {/snippet}
</Dialog>

<style>
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 4px;
  }
  .kind {
    font-size: 12.5px;
    font-weight: 600;
  }
  .meta {
    color: var(--text-muted);
    font-size: 11.5px;
  }
  .spacer {
    flex: 1;
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
    width: 100%;
    margin: 0;
    padding: 8px 10px;
    border: 0;
    border-radius: 7px;
    background: var(--identity);
    font: 12px/1.45 'JetBrains Mono Variable', monospace;
    text-align: left;
  }
  .value {
    max-height: 50vh;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .value.dump {
    white-space: pre;
    overflow-wrap: normal;
  }
  .masked {
    color: var(--text-muted);
    letter-spacing: 1px;
  }
  .error {
    color: var(--danger);
  }
</style>
