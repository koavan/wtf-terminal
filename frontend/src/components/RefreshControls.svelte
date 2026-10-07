<script lang="ts">
  import { Service as ConfigService } from '../../bindings/github.com/dayanraj/wtf/internal/config';
  import Icon from './Icon.svelte';
  import { settings } from '../lib/settings.svelte';
  import { toast } from '../lib/toast.svelte';

  /** Pause/resume background updates (a config setting, shared by every tab), and refresh now. */
  let { onrefresh }: { onrefresh: () => void } = $props();

  const auto = $derived(settings.config.sidebarAutoRefresh);

  function toggle() {
    ConfigService.SetSidebarAutoRefresh(!auto)
      .then(() => toast.show(auto ? 'Side pane auto-refresh off' : 'Side pane auto-refresh on'))
      .catch((e) => toast.show(`Couldn't change the setting: ${e}`));
  }
</script>

<button
  class="tool"
  class:paused={!auto}
  title={auto ? 'Pause auto-refresh (side pane updates only when you ask)' : 'Resume auto-refresh'}
  aria-label={auto ? 'Pause auto-refresh' : 'Resume auto-refresh'}
  aria-pressed={!auto}
  onclick={toggle}
>
  <Icon name={auto ? 'pause' : 'play'} size={12} />
</button>
<button class="tool" title="Refresh now" aria-label="Refresh now" onclick={onrefresh}><Icon name="refresh" size={13} /></button>

<style>
  .tool {
    display: grid;
    place-items: center;
    flex: none;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-muted);
  }
  .tool:hover {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tool.paused {
    color: var(--accent);
  }
</style>
