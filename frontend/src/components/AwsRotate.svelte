<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/awsprofiles';
  import Dialog from './Dialog.svelte';
  import { aws, type Profile } from '../lib/aws.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { toast } from '../lib/toast.svelte';

  // Rotates a profile's access key: shows the checked plan, and runs it
  // only when the user confirms. The new secret never leaves the backend.

  let { profile }: { profile: Profile } = $props();
  const p = untrack(() => profile);

  let deleteOld = $state(true);
  let plan = $state<string[] | null>(null);
  let steps = $state<string[]>([]);
  let error = $state('');
  let phase = $state<'checking' | 'ready' | 'rotating' | 'done' | 'failed'>('checking');

  onMount(check);

  const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

  async function check() {
    phase = 'checking';
    error = '';
    try {
      plan = (await Service.PlanRotate(p.name, deleteOld)) ?? [];
      phase = 'ready';
    } catch (e) {
      error = message(e);
      phase = 'failed';
    }
  }

  async function rotate() {
    phase = 'rotating';
    error = '';
    try {
      const res = await Service.Rotate(p.name, deleteOld, aws.versions);
      steps = res.steps ?? [];
      aws.apply(res.list);
      aws.checks.delete(p.name);
      phase = 'done';
      toast.show(`${p.name} now uses ${res.newKeyId}`);
    } catch (e) {
      error = message(e);
      phase = 'failed';
      aws.load();
    }
  }
</script>

<Dialog title="Rotate Access Key — {p.name}" subtitle="Replaces {p.accessKeyId} with a new key for the same IAM user" width={480} onsubmit={() => phase === 'ready' && rotate()}>
  {#if phase === 'checking'}
    <p class="hint">Checking the key with AWS…</p>
  {:else if phase === 'ready' && plan}
    <ol class="plan">
      {#each plan as line, i (i)}<li>{line}</li>{/each}
    </ol>
    <label class="check">
      <input type="checkbox" bind:checked={deleteOld} onchange={check} />
      Delete the old key afterwards (otherwise it’s left deactivated; AWS allows two keys per user)
    </label>
    <p class="hint">If AWS doesn’t accept the new key, wtf puts the old one back and deletes the new one.</p>
  {:else if phase === 'rotating'}
    <p class="hint">Rotating — this takes up to half a minute while AWS takes the new key on…</p>
  {:else if phase === 'done'}
    <ol class="plan">
      {#each steps as line, i (i)}<li>{line}</li>{/each}
    </ol>
  {/if}
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    {#if phase === 'ready'}
      <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
      <button class="primary" onclick={rotate}>Rotate Key</button>
    {:else}
      <button class="secondary" onclick={() => overlay.close()} disabled={phase === 'rotating'}>{phase === 'done' ? 'Done' : 'Close'}</button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .plan {
    margin: 0 0 10px;
    padding: 8px 10px 8px 28px;
    border-radius: 8px;
    background: var(--identity);
    font-size: 12px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
</style>
