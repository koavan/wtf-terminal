<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/awsprofiles';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { aws, draftOf, KIND_FIELDS, KIND_LABEL, REGIONS, toDraft, type Profile, type ProfileKind } from '../lib/aws.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { toast } from '../lib/toast.svelte';

  // Adds or edits a profile in two steps: fill in, then review the exact
  // list of changes the backend will make, and only then save.

  let { profile }: { profile: Profile | null } = $props();

  const editing = untrack(() => profile);
  let draft = $state(draftOf(editing));
  let plan = $state<string[] | null>(null);
  let error = $state('');
  let busy = $state(false);
  let nameInput = $state<HTMLInputElement>();

  onMount(() => nameInput?.focus());

  const KINDS: ProfileKind[] = ['keys', 'role', 'sso', 'process'];
  const others = $derived(aws.profiles.filter((p) => p.name !== editing?.name).map((p) => p.name));
  /** Changing the key ID needs its secret; otherwise an empty secret keeps the old one. */
  const keepsSecret = $derived(!!editing?.hasSecret && draft.accessKeyId.trim() === editing.accessKeyId);

  function setKind(k: ProfileKind) {
    draft.kind = k;
    for (const f of KIND_FIELDS[k]) draft.fields[f.key] ??= '';
  }

  async function review() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      plan = (await Service.PlanSave(toDraft(draft, editing?.name ?? '', aws.versions))) ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function save() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      aws.apply(await Service.Save(toDraft(draft, editing?.name ?? '', aws.versions)));
      overlay.close();
      toast.show(editing ? `Saved ${draft.name.trim()}` : `Added ${draft.name.trim()}`);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      plan = null;
    } finally {
      busy = false;
    }
  }
</script>

<Dialog
  title={plan ? 'Review Changes' : editing ? `Edit ${editing.name}` : 'New AWS Profile'}
  subtitle={plan ? 'Nothing is written until you confirm.' : `In ${prettyPath(aws.credentialsFile)} and ${prettyPath(aws.configFile)}`}
  width={470}
  onsubmit={() => (plan ? plan.length && save() : review())}
>
  {#if plan}
    {#if plan.length}
      <ul class="plan">
        {#each plan as line, i (i)}<li>{line}</li>{/each}
      </ul>
      <p class="hint">A copy of each file is kept in wtf’s cache folder (aws-backups) before it changes.</p>
    {:else}
      <p class="hint">Nothing to change.</p>
    {/if}
  {:else}
    <label class="field">
      <span>Name</span>
      <input bind:this={nameInput} bind:value={draft.name} placeholder="prod" spellcheck="false" autocomplete="off" />
      {#if editing?.sourceFor.length}<small>Source profile for {editing.sourceFor.join(', ')}: they follow a rename.</small>{/if}
    </label>

    <div class="kinds" role="radiogroup" aria-label="Credentials">
      {#each KINDS as k (k)}
        <button role="radio" aria-checked={draft.kind === k} class:on={draft.kind === k} onclick={() => setKind(k)}>{KIND_LABEL[k]}</button>
      {/each}
    </div>

    {#if draft.kind === 'keys'}
      <label class="field">
        <span>Access key ID</span>
        <input bind:value={draft.accessKeyId} placeholder="AKIA…" spellcheck="false" autocomplete="off" />
      </label>
      <label class="field">
        <span>Secret access key</span>
        <input type="password" bind:value={draft.secret} placeholder={keepsSecret ? 'unchanged' : ''} spellcheck="false" autocomplete="new-password" />
        <small>{keepsSecret ? 'Leave empty to keep the current secret. ' : ''}wtf never shows a saved secret again.</small>
      </label>
      <label class="field">
        <span>Session token</span>
        <input type="password" bind:value={draft.sessionToken} disabled={draft.removeSessionToken} placeholder={editing?.hasSessionToken ? 'unchanged' : 'only for temporary (ASIA…) keys'} spellcheck="false" autocomplete="off" />
      </label>
      {#if editing?.hasSessionToken}
        <label class="check"><input type="checkbox" bind:checked={draft.removeSessionToken} /> Remove the session token</label>
      {/if}
    {:else}
      {#each KIND_FIELDS[draft.kind] as f (f.key)}
        <label class="field">
          <span>{f.label}</span>
          {#if f.key === 'source_profile'}
            <input bind:value={draft.fields[f.key]} list="aws-profiles" placeholder={f.placeholder} spellcheck="false" autocomplete="off" />
          {:else}
            <input bind:value={draft.fields[f.key]} placeholder={f.placeholder} spellcheck="false" autocomplete="off" />
          {/if}
        </label>
      {/each}
      <datalist id="aws-profiles">{#each others as o (o)}<option value={o}></option>{/each}</datalist>
    {/if}

    <div class="row">
      <label class="field grow">
        <span>Region</span>
        <input bind:value={draft.region} list="aws-regions" placeholder="us-east-1" spellcheck="false" autocomplete="off" />
        <datalist id="aws-regions">{#each REGIONS as r (r)}<option value={r}></option>{/each}</datalist>
      </label>
      <label class="field grow">
        <span>Output</span>
        <select bind:value={draft.output}>
          <option value="">default (json)</option>
          {#each ['json', 'yaml', 'yaml-stream', 'text', 'table'] as o (o)}<option value={o}>{o}</option>{/each}
        </select>
      </label>
    </div>

    <label class="field">
      <span>More settings</span>
      <textarea bind:value={draft.more} rows="2" spellcheck="false" placeholder="duration_seconds = 3600"></textarea>
      <small>Other ~/.aws/config settings, one “key = value” per line.{editing?.locked.length ? ` Nested settings (${editing.locked.join(', ')}) are kept as they are.` : ''}</small>
    </label>
    {#if editing?.credentialsOther.length}
      <p class="hint">Also in the credentials file, kept as written: {editing.credentialsOther.map((s) => `${s.key} = ${s.value}`).join(', ')}.</p>
    {/if}
  {/if}

  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    {#if plan}
      <button class="secondary" onclick={() => (plan = null)} disabled={busy}>Back</button>
      {#if plan.length}<button class="primary" onclick={save} disabled={busy}>Confirm &amp; Save</button>{/if}
    {:else}
      <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
      <button class="primary" onclick={review} disabled={busy || !draft.name.trim()}>Review…</button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .kinds {
    display: flex;
    gap: 2px;
    margin: 0 0 12px;
    padding: 2px;
    border-radius: 8px;
    background: var(--identity);
  }
  .kinds button {
    flex: 1;
    height: 26px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 550;
    white-space: nowrap;
  }
  .kinds button.on {
    background: var(--popover);
    color: var(--text);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.15);
  }
  textarea {
    height: auto !important;
    padding: 6px 9px !important;
    line-height: 1.45;
    resize: vertical;
  }
  .plan {
    max-height: 50vh;
    overflow: auto;
    margin: 0;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--identity);
    list-style: none;
    font: 11.5px/1.6 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
  }
</style>
