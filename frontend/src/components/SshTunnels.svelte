<script lang="ts">
  import { Browser, Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { describeTunnel, localPort, message, sshTunnels, type TunnelStatus } from '../lib/sshAdmin.svelte';
  import { toast } from '../lib/toast.svelte';

  // Saved port forwards. The backend runs them and reports every change
  // (see internal/sshhosts/tunnels.go), so nothing polls here.

  const STATE_LABEL: Record<string, string> = {
    stopped: 'off',
    connecting: 'connecting…',
    up: 'on',
    retrying: 'reconnecting…',
    failed: 'failed',
  };

  const isOn = (t: TunnelStatus) => t.state === 'up' || t.state === 'connecting' || t.state === 'retrying';

  async function act(what: () => Promise<unknown>) {
    try {
      await what();
    } catch (e) {
      toast.show(message(e), 6000);
    }
  }

  const toggle = (t: TunnelStatus) => act(() => (isOn(t) ? sshTunnels.stop(t.id) : sshTunnels.start(t.id)));

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function menu(e: MouseEvent, t: TunnelStatus) {
    e.preventDefault();
    const port = localPort(t);
    const entries: MenuEntry[] = [
      isOn(t) ? { label: 'Stop', action: () => toggle(t) } : { label: 'Start', action: () => toggle(t) },
    ];
    if (port && t.kind === 'local') entries.push({ label: `Open localhost:${port} in Browser`, action: () => Browser.OpenURL(`http://localhost:${port}`) });
    if (port) entries.push({ label: `Copy localhost:${port}`, action: () => copy(`localhost:${port}`, `localhost:${port}`) });
    entries.push(
      { label: 'Copy ssh Command', action: () => copy(t.command, 'ssh command') },
      'separator',
      { label: 'Edit…', action: () => overlay.show({ kind: 'tunnel', tunnel: t, host: null }) },
      {
        label: 'Delete',
        danger: true,
        action: () =>
          overlay.confirm({
            title: `Delete ${t.name || 'this tunnel'}?`,
            message: `${describeTunnel(t)}${isOn(t) ? '. It stops now.' : '.'}`,
            items: [],
            confirmLabel: 'Delete Tunnel',
            onConfirm: () => act(() => sshTunnels.remove(t.id)),
          }),
      },
    );
    overlay.openMenu(e.clientX, e.clientY, entries);
  }
</script>

<div class="tunnels">
  <header>
    <span class="title">Port forwards</span>
    <button class="tool" title="New tunnel" aria-label="New tunnel" onclick={() => overlay.show({ kind: 'tunnel', tunnel: null, host: null })}>
      <Icon name="plus" size={14} />
    </button>
  </header>

  <div class="list" role="list">
    {#each sshTunnels.list as t (t.id)}
      <div
        class="row"
        role="listitem"
        title="{t.command}{t.autoStart ? '\nStarts when wtf opens' : ''}"
        oncontextmenu={(e) => menu(e, t)}
        ondblclick={() => overlay.show({ kind: 'tunnel', tunnel: t, host: null })}
      >
        <span class="dot {t.state}" aria-hidden="true"></span>
        <span class="main">
          <span class="name">{t.name || describeTunnel(t)}</span>
          <span class="sub">
            {#if t.name}{describeTunnel(t)} · {/if}<span class="state {t.state}">{STATE_LABEL[t.state] ?? t.state}</span>
          </span>
          {#if t.error && (t.state === 'failed' || t.state === 'retrying')}<span class="err">{t.error}</span>{/if}
        </span>
        <button
          class="toggle"
          class:on={isOn(t)}
          title={isOn(t) ? 'Stop' : 'Start'}
          aria-label="{isOn(t) ? 'Stop' : 'Start'} {t.name || describeTunnel(t)}"
          onclick={() => toggle(t)}
          ondblclick={(e) => e.stopPropagation()}
        >
          <Icon name={isOn(t) ? 'stop' : 'play'} size={11} stroke={2} />
        </button>
      </div>
    {:else}
      {#if sshTunnels.loaded}
        <div class="note">
          <strong>No tunnels</strong>
          Forward a port through ssh: reach a database behind a bastion on <code>localhost</code>, expose a local dev
          server to a server, or browse through a SOCKS proxy. Press + to add one, or right-click a host.
        </div>
      {/if}
    {/each}
  </div>
</div>

<style>
  .tunnels {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  header {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
    padding: 0 6px 4px 14px;
  }
  .title {
    flex: 1;
    color: var(--chrome-muted);
    font-size: 11.5px;
  }
  .tool {
    display: grid;
    place-items: center;
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
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }
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
    animation: pulse 1.2s ease-in-out infinite;
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
  .note {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 8px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
  code {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11px;
  }
</style>
