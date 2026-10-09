<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { prettyPath } from '../lib/backend';
  import { overlay } from '../lib/overlay.svelte';
  import { confirmDeleteHost, draftOf, openHosts, ssh, type Host, type HostDraft } from '../lib/ssh.svelte';
  import { toast } from '../lib/toast.svelte';

  let { host, prefill }: { host: Host | null; prefill?: Partial<HostDraft> } = $props();

  // Props are live overlay state, which closing clears: copy what's needed now.
  const editing = untrack(() => host);
  const prefilled = untrack(() => prefill);
  let draft = $state(draftOf(editing, prefilled));
  let error = $state('');
  let busy = $state(false);
  let nameInput = $state<HTMLInputElement>();

  onMount(() => nameInput?.focus());

  const own = new Set((editing?.options ?? []).map((o) => o.key.toLowerCase()));
  /** What a field gets from other entries (Host *) when left empty. */
  const inherited = (key: string, value: string | undefined) =>
    editing && !own.has(key) && value && value !== editing.alias ? `${value} (from another entry)` : '';

  const where = editing
    ? `${prettyPath(editing.source)}, line ${editing.line}`
    : prettyPath(ssh.mainConfig) || '~/.ssh/config';
  const title = editing ? `Edit ${editing.alias}` : prefilled?.hostName ? 'Save Known Host' : 'New SSH Host';
  const aliases = $derived(ssh.hosts.filter((h) => !h.known && h.alias !== editing?.alias).map((h) => h.alias));

  async function save(connect: boolean) {
    if (busy) return;
    busy = true;
    error = '';
    const name = draft.name.trim().split(/\s+/)[0];
    try {
      await ssh.save(editing, draft);
      overlay.close();
      toast.show(editing ? `Saved ${name}` : `Added ${name} to ${prettyPath(ssh.mainConfig)}`);
      if (connect) {
        const added = ssh.hosts.find((h) => !h.known && h.alias === name);
        if (added) openHosts([added]);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }


  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      overlay.close();
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey || (e.target as HTMLElement).tagName === 'INPUT')) {
      e.preventDefault();
      save(false);
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

<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="host-title" tabindex="-1" in:appear onkeydown={onKeydown}>
  <h2 id="host-title">{title}</h2>
  <p class="where">{editing ? 'In' : 'Added to'} {where}</p>

  <label class="field">
    <span>Name</span>
    <input bind:this={nameInput} bind:value={draft.name} placeholder="web1" spellcheck="false" autocomplete="off" />
    <small>What you type after <code>ssh</code>. Separate several names with spaces.</small>
  </label>

  <label class="field">
    <span>Address</span>
    <input bind:value={draft.hostName} placeholder={inherited('hostname', editing?.hostName) || 'web1.example.com or 10.0.0.5'} spellcheck="false" autocomplete="off" />
  </label>

  <div class="row">
    <label class="field grow">
      <span>User</span>
      <input bind:value={draft.user} placeholder={inherited('user', editing?.user) || 'your user name'} spellcheck="false" autocomplete="off" />
    </label>
    <label class="field port">
      <span>Port</span>
      <input bind:value={draft.port} placeholder={inherited('port', editing?.port) || '22'} inputmode="numeric" spellcheck="false" autocomplete="off" />
    </label>
  </div>

  <label class="field">
    <span>Key file</span>
    <input bind:value={draft.identityFile} list="ssh-keys" placeholder="your default keys" spellcheck="false" autocomplete="off" />
    <datalist id="ssh-keys">
      {#each ssh.keys as k (k)}<option value={k}></option>{/each}
    </datalist>
  </label>

  <label class="field">
    <span>Jump host</span>
    <input bind:value={draft.proxyJump} list="ssh-aliases" placeholder={inherited('proxyjump', editing?.proxyJump) || 'none'} spellcheck="false" autocomplete="off" />
    <datalist id="ssh-aliases">
      {#each aliases as a (a)}<option value={a}></option>{/each}
    </datalist>
  </label>

  <label class="field">
    <span>More settings</span>
    <textarea bind:value={draft.more} rows="3" spellcheck="false" placeholder={'ForwardAgent yes\nLocalForward 8080 localhost:80'}></textarea>
    <small>One per line, as in an ssh config file. ⌘↩ saves.</small>
  </label>

  {#if error}<div class="error" role="alert">{error}</div>{/if}

  <div class="buttons">
    {#if editing}<button class="delete" onclick={() => confirmDeleteHost(editing)} disabled={busy}>Delete…</button>{/if}
    <span class="spacer"></span>
    <button class="cancel" onclick={() => overlay.close()}>Cancel</button>
    {#if !editing}<button class="cancel" onclick={() => save(true)} disabled={busy}>Save &amp; Connect</button>{/if}
    <button class="save" onclick={() => save(false)} disabled={busy}>Save</button>
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
    top: 46%;
    left: 50%;
    z-index: 111;
    transform: translate(-50%, -50%);
    width: min(440px, calc(100vw - 32px));
    max-height: calc(100vh - 48px);
    overflow: auto;
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
  .where {
    margin: 2px 0 14px;
    color: var(--text-muted);
    font-size: 12px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
  }
  .field > span {
    color: var(--text-muted);
    font-size: 11.5px;
    font-weight: 600;
  }
  .row {
    display: flex;
    gap: 10px;
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .port {
    width: 90px;
  }

  input,
  textarea {
    width: 100%;
    box-sizing: border-box;
    padding: 6px 9px;
    border: 1px solid var(--hairline);
    border-radius: 7px;
    background: var(--identity);
    color: var(--text);
    font: 12.5px 'JetBrains Mono Variable', monospace;
    -webkit-user-select: text;
    user-select: text;
  }
  input {
    height: 30px;
  }
  textarea {
    resize: vertical;
    line-height: 1.45;
  }
  input:focus,
  textarea:focus {
    outline: none;
    border-color: var(--accent);
  }
  input::placeholder,
  textarea::placeholder {
    color: var(--text-muted);
    opacity: 0.7;
  }
  small {
    color: var(--text-muted);
    font-size: 11px;
  }
  code {
    font-family: 'JetBrains Mono Variable', monospace;
  }

  .error {
    margin: 4px 0 0;
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
  .spacer {
    flex: 1;
  }
  button {
    height: 30px;
    padding: 0 14px;
    border: 0;
    border-radius: 8px;
    font-size: 13px;
    font-weight: 550;
    white-space: nowrap;
  }
  button:disabled {
    opacity: 0.5;
  }
  .cancel {
    background: var(--identity);
    color: var(--text);
  }
  .cancel:hover:not(:disabled) {
    background: var(--tab-hover-strong);
  }
  .save {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 650;
  }
  .save:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
  }
  .delete {
    padding: 0 10px;
    background: transparent;
    color: var(--danger);
  }
  .delete:hover:not(:disabled) {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }
</style>
