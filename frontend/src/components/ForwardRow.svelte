<script lang="ts">
  import { Browser, Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { describeTunnel, localPort, message, sshTunnels, type TunnelStatus } from '../lib/sshAdmin.svelte';
  import { toast } from '../lib/toast.svelte';

  // One port forward — an ssh tunnel or a kubectl port-forward — with its
  // status, a start/stop button and its menu. Used by the SSH tab's Tunnels
  // and the Network tab's Forwards.

  let { t }: { t: TunnelStatus } = $props();

  const STATE_LABEL: Record<string, string> = {
    stopped: 'off',
    connecting: 'connecting…',
    up: 'on',
    retrying: 'reconnecting…',
    failed: 'failed',
  };

  const isOn = (x: TunnelStatus) => x.state === 'up' || x.state === 'connecting' || x.state === 'retrying';

  async function act(what: () => Promise<unknown>) {
    try {
      await what();
    } catch (e) {
      toast.show(message(e), 6000);
    }
  }

  const toggle = () => act(() => (isOn(t) ? sshTunnels.stop(t.id) : sshTunnels.start(t.id)));

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function menu(e: MouseEvent) {
    e.preventDefault();
    const port = localPort(t);
    const entries: MenuEntry[] = [isOn(t) ? { label: 'Stop', action: toggle } : { label: 'Start', action: toggle }];
    if (port && (t.kind === 'local' || t.kind === 'kubectl')) {
      entries.push({ label: `Open localhost:${port} in Browser`, action: () => Browser.OpenURL(`http://localhost:${port}`) });
    }
    if (port) entries.push({ label: `Copy localhost:${port}`, action: () => copy(`localhost:${port}`, `localhost:${port}`) });
    entries.push(
      { label: 'Copy Command', action: () => copy(t.command, 'command') },
      'separator',
      { label: 'Edit…', action: () => overlay.show({ kind: 'tunnel', tunnel: t, host: null }) },
      {
        label: 'Delete',
        danger: true,
        action: () =>
          overlay.confirm({
            title: `Delete ${t.name || 'this port forward'}?`,
            message: `${describeTunnel(t)}${isOn(t) ? '. It stops now.' : '.'}`,
            items: [],
            confirmLabel: 'Delete',
            onConfirm: () => act(() => sshTunnels.remove(t.id)),
          }),
      },
    );
    overlay.openMenu(e.clientX, e.clientY, entries);
  }
</script>

<div
  class="row"
  role="listitem"
  title="{t.command}{t.autoStart ? '\nStarts when wtf opens' : ''}"
  oncontextmenu={menu}
  ondblclick={() => overlay.show({ kind: 'tunnel', tunnel: t, host: null })}
>
  <span class="dot {t.state}" aria-hidden="true"></span>
  <span class="main">
    <span class="name">{t.name || describeTunnel(t)}</span>
    <span class="sub">
      {#if t.name}{`${describeTunnel(t)} · `}{/if}<span class="state {t.state}">{STATE_LABEL[t.state] ?? t.state}</span>
    </span>
    {#if t.error && (t.state === 'failed' || t.state === 'retrying')}<span class="err">{t.error}</span>{/if}
  </span>
  <button
    class="toggle"
    class:on={isOn(t)}
    title={isOn(t) ? 'Stop' : 'Start'}
    aria-label="{isOn(t) ? 'Stop' : 'Start'} {t.name || describeTunnel(t)}"
    onclick={toggle}
    ondblclick={(e) => e.stopPropagation()}
  >
    <Icon name={isOn(t) ? 'stop' : 'play'} size={11} stroke={2} />
  </button>
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 9px;
    min-height: 34px;
    padding: 4px 6px 4px 10px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--chrome-muted);
    opacity: 0.5;
  }
  .dot.up {
    background: var(--mark-ok);
    opacity: 1;
  }
  .dot.connecting,
  .dot.retrying {
    background: var(--g-yellow);
    opacity: 1;
    animation: pulse 1.2s ease-in-out 3; /* then holds still: no endless redraws */
  }
  .dot.failed {
    background: var(--danger);
    opacity: 1;
  }
  @keyframes pulse {
    50% {
      opacity: 0.35;
    }
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .name {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font: 500 12px 'JetBrains Mono Variable', monospace;
  }
  .sub {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--chrome-muted);
    font-size: 11px;
  }
  .state.failed,
  .err {
    color: var(--danger);
  }
  .err {
    font-size: 11px;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
  .toggle {
    display: grid;
    place-items: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--accent);
  }
  .toggle.on {
    color: var(--chrome-muted);
  }
  .toggle:hover {
    background: var(--accent-soft);
    color: var(--accent);
  }
</style>
