<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import { Service as Certs, type Report } from '../../bindings/github.com/dayanraj/wtf/internal/certs';
  import Dialog from './Dialog.svelte';
  import { relativeTime } from '../lib/logs';
  import { overlay, type CertSource } from '../lib/overlay.svelte';
  import { prettyPath } from '../lib/backend';
  import { toast } from '../lib/toast.svelte';

  // The certificate inspector: the chain a TLS server presents (a URL in
  // the output, a selected host name, or one typed here), or the
  // certificates in a file or a selection. Dates, names, keys and the TLS
  // library's own verdict on the chain — nothing more is judged.

  let { source }: { source: CertSource } = $props();
  const src = untrack(() => source);

  let host = $state('host' in src ? src.host : '');
  let report = $state.raw<Report | null>(null);
  let error = $state('');
  let busy = $state(false);
  let now = $state(new Date());

  async function inspect() {
    error = '';
    report = null;
    busy = true;
    try {
      if ('file' in src) report = await Certs.InspectFile(src.file);
      else if ('text' in src) report = await Certs.InspectText(src.text);
      else if (host.trim()) report = await Certs.InspectHost(host.trim());
      now = new Date();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  onMount(() => {
    if (!('host' in src) || host) void inspect();
  });

  const title = 'host' in src ? 'TLS Certificate' : 'Certificate';
  const subtitle =
    'file' in src ? prettyPath(src.file)
    : 'text' in src ? 'From the selected text.'
    : 'Connects to the server and shows the certificates it presents. Nothing is sent but the TLS handshake.';

  const date = (ms: number) => new Date(ms).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
  const validity = (notBefore: number, notAfter: number) =>
    notAfter <= now.getTime() ? `expired ${relativeTime(new Date(notAfter), now)}`
    : notBefore > now.getTime() ? `not valid until ${relativeTime(new Date(notBefore), now)}`
    : `expires ${relativeTime(new Date(notAfter), now)}`;
  const expired = (c: { notBefore: number; notAfter: number }) => c.notAfter <= now.getTime() || c.notBefore > now.getTime();

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }
</script>

<Dialog {title} {subtitle} width={640} onsubmit={'host' in src ? inspect : undefined}>
  {#if 'host' in src}
    <div class="target">
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="host"
        bind:value={host}
        placeholder="example.com, host:8443 or https://…"
        spellcheck="false"
        autocapitalize="off"
        autofocus={!host}
      />
      <button class="secondary" disabled={busy || !host.trim()} onclick={inspect}>Inspect</button>
    </div>
  {/if}

  {#if error}
    <div class="error" role="alert">{error}</div>
  {:else if busy}
    <p class="hint">{'host' in src ? `Connecting to ${host.trim()}…` : 'Reading…'}</p>
  {:else if report}
    {#if 'host' in src}
      <dl class="facts">
        <dt>Connected to</dt>
        <dd class="mono">{report.address}</dd>
        <dt>Protocol</dt>
        <dd class="mono">{report.protocol} · {report.cipher}{report.alpn ? ` · ${report.alpn}` : ''}</dd>
        <dt>Trust</dt>
        <dd>
          {#if report.verified}
            <span class="tag ok">verifies</span> against this computer's trusted roots, for {host.trim().replace(/^\w+:\/\//, '').replace(/[:/].*$/, '')}
          {:else}
            <span class="tag bad">doesn't verify</span> <span class="mono">{report.verifyError}</span>
          {/if}
        </dd>
      </dl>
    {/if}
    {#each report.notes ?? [] as note, i (i)}
      <p class="hint">{note}</p>
    {/each}

    <ol class="chain">
      {#each report.certificates ?? [] as c, i (i)}
        <li>
          <div class="who">
            <span class="subject">{c.subject || '(no subject)'}</span>
            {#if 'host' in src}<span class="tag">{i === 0 ? 'server' : c.selfSigned ? 'root' : 'intermediate'}</span>{/if}
            {#if c.isCA}<span class="tag">CA</span>{/if}
            {#if c.selfSigned}<span class="tag">self-signed</span>{/if}
          </div>
          <dl class="facts">
            <dt>Valid</dt>
            <dd>
              {date(c.notBefore)} – {date(c.notAfter)}
              <span class="tag" class:bad={expired(c)}>{validity(c.notBefore, c.notAfter)}</span>
            </dd>
            {#if c.names?.length}
              <dt>Names</dt>
              <dd class="mono">{c.names.join(', ')}</dd>
            {/if}
            <dt>Issuer</dt>
            <dd>{c.issuer}</dd>
            <dt>Key</dt>
            <dd>{c.key} · signed with {c.signatureAlgorithm}</dd>
            <dt>Serial</dt>
            <dd class="mono small">{c.serial}</dd>
            <dt>SHA-256</dt>
            <dd class="mono small">
              {c.sha256}
              <button class="link" onclick={() => copy(c.sha256, 'fingerprint')}>Copy</button>
            </dd>
          </dl>
        </li>
      {/each}
    </ol>
  {/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="primary" onclick={() => overlay.close()}>Done</button>
  {/snippet}
</Dialog>

<style>
  .target {
    display: flex;
    gap: 8px;
    margin-bottom: 10px;
  }
  .target .host {
    flex: 1;
    font-family: 'JetBrains Mono Variable', monospace;
  }
  .chain {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-height: 56vh;
    margin: 8px 0 0;
    padding: 0;
    overflow: auto;
    list-style: none;
  }
  .chain li {
    padding: 9px 11px;
    border-radius: 8px;
    background: var(--identity);
  }
  .who {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
  }
  .subject {
    font-weight: 600;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 4px 14px;
    margin: 0;
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
  .small {
    font-size: 11px;
  }
  .tag {
    display: inline-block;
    padding: 0 6px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--text-muted) 16%, transparent);
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 600;
  }
  .tag.ok {
    color: var(--mark-ok);
  }
  .tag.bad {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger);
  }
  .link {
    margin-left: 6px;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    font-family: system-ui, sans-serif;
    cursor: pointer;
  }
  .hint {
    margin: 4px 0;
    color: var(--text-muted);
    font-size: 12.5px;
  }
  .error {
    color: var(--danger);
    overflow-wrap: anywhere;
  }
</style>
