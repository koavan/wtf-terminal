<script lang="ts">
  import KubeCluster from './KubeCluster.svelte';
  import KubeContexts from './KubeContexts.svelte';
  import { sidebar, type KubeView } from '../lib/sidebar.svelte';

  // The Kubernetes tab: the cluster the focused shell points at, and the
  // contexts it can point at.

  const VIEWS: { id: KubeView; label: string }[] = [
    { id: 'cluster', label: 'Resources' },
    { id: 'contexts', label: 'Contexts' },
  ];
</script>

<div class="kube-view">
  <div class="sections" role="tablist" aria-label="Kubernetes">
    {#each VIEWS as v (v.id)}
      <button role="tab" aria-selected={sidebar.kubeView === v.id} class:on={sidebar.kubeView === v.id} onclick={() => (sidebar.kubeView = v.id)}>
        {v.label}
      </button>
    {/each}
  </div>
  <div class="body">
    {#if sidebar.kubeView === 'contexts'}
      <KubeContexts />
    {:else}
      <KubeCluster />
    {/if}
  </div>
</div>

<style>
  .kube-view {
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
  }
</style>
