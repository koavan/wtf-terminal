<script lang="ts">
  import ToolHint from './ToolHint.svelte';
  import { onMount } from 'svelte';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/kube';
  import Dialog from './Dialog.svelte';
  import { aws, REGIONS } from '../lib/aws.svelte';
  import { eksCommand, kubeConfigs } from '../lib/kubeconfigs.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { tabs } from '../lib/tabs.svelte';

  // Adds an EKS cluster to the kubeconfig with `aws eks update-kubeconfig`,
  // run in a new tab (the kubeconfig is backed up first). The cluster list
  // is fetched only when asked for.

  let profile = $state('');
  let region = $state('us-east-1');
  let cluster = $state('');
  let alias = $state('');
  let clusters = $state<string[] | null>(null);
  let listing = $state(false);
  let error = $state('');

  onMount(() => {
    if (!aws.loaded) aws.load(); // local files only
  });

  // A profile's region is the likely one.
  $effect(() => {
    const p = aws.profiles.find((x) => x.name === profile);
    if (p?.region) region = p.region;
  });

  async function list() {
    listing = true;
    error = '';
    try {
      clusters = (await Service.EKSClusters(kubeConfigs.env(), profile, region.trim())) ?? [];
      if (clusters.length === 1 && !cluster) cluster = clusters[0];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      clusters = null;
    } finally {
      listing = false;
    }
  }

  /** The shell's first kubeconfig file, when it sets KUBECONFIG. */
  const target = kubeConfigs.env().KUBECONFIG ? (kubeConfigs.files[0] ?? '') : '';
  const command = $derived(cluster.trim() && region.trim() ? eksCommand(cluster.trim(), region.trim(), profile, alias.trim(), target) : '');

  async function add() {
    if (!command) return;
    const c = command;
    await Service.BackupConfigs(kubeConfigs.env()).catch(() => {});
    overlay.close();
    tabs.open({ command: c });
  }
</script>

<Dialog title="Add EKS Cluster" subtitle="Adds a context for an EKS cluster to your kubeconfig, using one of your AWS profiles" onsubmit={add}>
  <div class="row">
    <label class="field grow">
      <span>AWS profile</span>
      <select bind:value={profile}>
        <option value="">default credentials</option>
        {#each aws.profiles as p (p.name)}<option value={p.name}>{p.name}</option>{/each}
      </select>
    </label>
    <label class="field grow">
      <span>Region</span>
      <input bind:value={region} list="eks-regions" spellcheck="false" autocomplete="off" />
      <datalist id="eks-regions">{#each REGIONS as r (r)}<option value={r}></option>{/each}</datalist>
    </label>
  </div>

  <label class="field">
    <span>Cluster</span>
    <div class="row">
      <input class="grow" bind:value={cluster} list="eks-clusters" placeholder="cluster name" spellcheck="false" autocomplete="off" />
      <button class="secondary" onclick={list} disabled={listing || !region.trim()} title="Asks AWS (eks list-clusters)">{listing ? 'Asking…' : 'List Clusters'}</button>
    </div>
    <datalist id="eks-clusters">{#each clusters ?? [] as c (c)}<option value={c}></option>{/each}</datalist>
    {#if clusters}<small>{clusters.length ? `${clusters.length} in ${region}: ${clusters.join(', ')}` : `No clusters in ${region} for this profile`}</small>{/if}
  </label>

  <label class="field">
    <span>Context name</span>
    <input bind:value={alias} placeholder="the cluster's ARN, if empty" spellcheck="false" autocomplete="off" />
  </label>

  {#if command}
    <div class="command">{command}</div>
    <p class="hint">Runs in a new tab. wtf keeps a copy of your kubeconfig first (kube-backups). It logs in with <code>aws eks get-token</code>.</p>
  {/if}
  {#if error}<div class="error" role="alert">{error}<ToolHint {error} /></div>{/if}

  {#snippet buttons()}
    <span class="spacer"></span>
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="primary" onclick={add} disabled={!command}>Add in Terminal</button>
  {/snippet}
</Dialog>
