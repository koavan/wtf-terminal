<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import Dialog from './Dialog.svelte';
  import { decodeJwt } from '../lib/inspect';
  import { relativeTime } from '../lib/logs';
  import { overlay } from '../lib/overlay.svelte';
  import { toast } from '../lib/toast.svelte';

  // A JWT's header and claims, decoded (right-click or ⌘-click a token in
  // the output). The signature isn't checked: wtf has no key to check it
  // with. The token lives only in this dialog.

  let { token }: { token: string } = $props();
  let jwt = $state.raw(untrack(() => decodeJwt(token)));
  onMount(() => () => (jwt = null));

  const now = new Date();
  const LABEL = { exp: 'Expires', nbf: 'Not valid before', iat: 'Issued', auth_time: 'Signed in' } as const;
  const when = (d: Date) =>
    `${d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' })} · ${relativeTime(d, now)}`;
  const exp = $derived(jwt?.times.find((t) => t.claim === 'exp'));
  const nbf = $derived(jwt?.times.find((t) => t.claim === 'nbf'));
  const status = $derived(
    exp && exp.date <= now ? 'expired' : nbf && nbf.date > now ? 'not yet valid' : exp ? 'not expired' : 'no expiry',
  );
  const pretty = (v: unknown) => JSON.stringify(v, null, 2);

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }
</script>

<Dialog
  title="JWT"
  subtitle="Decoded from the terminal's output. The signature isn't verified: wtf has no key to check it with."
  width={600}
>
  {#if !jwt}
    <div class="error" role="alert">This isn't a JWT wtf can read.</div>
  {:else}
    <dl class="facts">
      <dt>Status</dt>
      <dd><span class="state {status.replace(/ /g, '-')}">{status}</span></dd>
      {#each jwt.times as t (t.claim)}
        <dt>{LABEL[t.claim]}</dt>
        <dd>{when(t.date)}</dd>
      {/each}
      {#if typeof jwt.claims.iss === 'string'}<dt>Issuer</dt><dd class="mono">{jwt.claims.iss}</dd>{/if}
      {#if typeof jwt.claims.sub === 'string'}<dt>Subject</dt><dd class="mono">{jwt.claims.sub}</dd>{/if}
      {#if jwt.claims.aud !== undefined}<dt>Audience</dt><dd class="mono">{[jwt.claims.aud].flat().join(', ')}</dd>{/if}
      <dt>Algorithm</dt>
      <dd class="mono">{jwt.alg || 'none'}{jwt.signed ? '' : ' · unsigned'}</dd>
    </dl>

    <h4>Claims</h4>
    <pre class="code">{pretty(jwt.claims)}</pre>
    <h4>Header</h4>
    <pre class="code">{pretty(jwt.header)}</pre>
  {/if}

  {#snippet buttons()}
    {#if jwt}
      <button class="secondary" onclick={() => copy(pretty(jwt!.claims), 'claims')}>Copy Claims</button>
    {/if}
    <span class="spacer"></span>
    <button class="primary" onclick={() => overlay.close()}>Done</button>
  {/snippet}
</Dialog>

<style>
  .facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 5px 14px;
    margin: 0 0 6px;
    font-size: 12.5px;
  }
  dt {
    color: var(--text-muted);
  }
  dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .mono {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 12px;
  }
  .state {
    padding: 1px 7px;
    border-radius: 5px;
    background: var(--identity);
    font-size: 11.5px;
    font-weight: 600;
  }
  .state.not-expired {
    color: var(--mark-ok);
  }
  .state.expired,
  .state.not-yet-valid {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger);
  }
  h4 {
    margin: 12px 0 4px;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
  .code {
    max-height: 32vh;
    margin: 0;
    padding: 8px 10px;
    overflow: auto;
    border-radius: 7px;
    background: var(--identity);
    font: 12px/1.45 'JetBrains Mono Variable', monospace;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .error {
    color: var(--danger);
  }
</style>
