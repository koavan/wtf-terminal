<script lang="ts">
  import { onMount } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { Clipboard } from '@wailsio/runtime';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { groupName, hostDetail, hostKey, matchesFilter, openHosts, ssh, sshCommand, type Host, type OpenWhere } from '../lib/ssh.svelte';
  import { toast } from '../lib/toast.svelte';

  let filter = $state('');
  const selected = new SvelteSet<string>();
  /** Where shift-click selection extends from. */
  let anchor: string | null = null;

  onMount(() => {
    ssh.load();
  });

  const shown = $derived(ssh.hosts.filter((h) => matchesFilter(h, filter)));

  /** Config hosts by file (when there are several files), then known hosts. */
  const sections = $derived.by(() => {
    const configured = shown.filter((h) => !h.known);
    const known = shown.filter((h) => h.known);
    const files = [...new Set(configured.map((h) => h.source))];
    const out: { title: string; hint: string; hosts: Host[] }[] = files.map((f) => ({
      title: files.length > 1 ? prettyPath(f) : 'SSH config',
      hint: prettyPath(f),
      hosts: configured.filter((h) => h.source === f),
    }));
    if (known.length) {
      out.push({ title: 'Known hosts', hint: 'Servers in ~/.ssh/known_hosts that your config doesn’t name', hosts: known });
    }
    return out;
  });

  /** Rows in display order, for shift-click ranges. */
  const order = $derived(sections.flatMap((s) => s.hosts));
  const chosen = $derived(order.filter((h) => selected.has(hostKey(h))));

  // Hosts that disappear (filtered out or gone from the config) drop out of the selection.
  $effect(() => {
    const visible = new Set(order.map(hostKey));
    for (const k of selected) if (!visible.has(k)) selected.delete(k);
  });

  function click(e: MouseEvent, h: Host) {
    const key = hostKey(h);
    if (e.shiftKey && anchor) {
      const keys = order.map(hostKey);
      const [a, b] = [keys.indexOf(anchor), keys.indexOf(key)].sort((x, y) => x - y);
      if (a >= 0) {
        if (!(isMac ? e.metaKey : e.ctrlKey)) selected.clear();
        for (const k of keys.slice(a, b + 1)) selected.add(k);
        return;
      }
    }
    if (isMac ? e.metaKey : e.ctrlKey) {
      if (selected.has(key)) selected.delete(key);
      else selected.add(key);
    } else {
      selected.clear();
      selected.add(key);
    }
    anchor = key;
  }

  function open(hosts: Host[], where: OpenWhere) {
    openHosts(hosts, where);
    selected.clear();
  }

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function menu(e: MouseEvent, h: Host) {
    e.preventDefault();
    if (!selected.has(hostKey(h))) {
      selected.clear();
      selected.add(hostKey(h));
      anchor = hostKey(h);
    }
    const many = chosen.length > 1 ? chosen : null;
    const entries: MenuEntry[] = many
      ? [
          { label: `Open ${many.length} Hosts in a Tab Group`, hint: '↩', action: () => open(many, 'group') },
          { label: `Open ${many.length} Hosts in Tabs`, action: () => open(many, 'tab') },
          'separator',
          { label: 'Copy ssh Commands', action: () => copy(many.map(sshCommand).join('\n'), 'ssh commands') },
        ]
      : [
          { label: 'Open in New Tab', hint: 'double-click', action: () => open([h], 'tab') },
          { label: 'Open in Split Right', action: () => open([h], 'right') },
          { label: 'Open in Split Down', action: () => open([h], 'down') },
          'separator',
          { label: 'Copy ssh Command', action: () => copy(sshCommand(h), 'ssh command') },
          { label: 'Copy Host Name', action: () => copy(h.hostName || h.alias, 'host name') },
        ];
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && chosen.length) {
      e.preventDefault();
      open(chosen, chosen.length > 1 ? 'group' : 'tab');
    } else if (e.key === 'Escape') {
      selected.clear();
    }
  }
</script>

<div class="ssh">
  <header>
    <div class="filter">
      <Icon name="search" size={12} stroke={2} />
      <input bind:value={filter} placeholder="Filter hosts" spellcheck="false" autocomplete="off" onkeydown={keydown} />
    </div>
    <button class="tool" title="Reload ~/.ssh/config" aria-label="Reload" disabled={ssh.loading} onclick={() => ssh.load()}>
      <Icon name="refresh" size={13} />
    </button>
  </header>

  <div class="list" role="listbox" aria-multiselectable="true" tabindex="-1" onkeydown={keydown}>
    {#each sections as s (s.title)}
      <div class="section" title={s.hint}>{s.title}</div>
      {#each s.hosts as h (hostKey(h))}
        {@const detail = hostDetail(h)}
        <div
          class="row"
          class:on={selected.has(hostKey(h))}
          role="option"
          aria-selected={selected.has(hostKey(h))}
          tabindex="-1"
          title="{sshCommand(h)}{h.source ? `\nfrom ${prettyPath(h.source)}` : ''}"
          onclick={(e) => click(e, h)}
          ondblclick={() => open([h], 'tab')}
          oncontextmenu={(e) => menu(e, h)}
          onkeydown={() => {}}
        >
          <span class="icon" class:known={h.known}><Icon name="server" size={13} /></span>
          <span class="main">
            <span class="alias">{h.alias}</span>
            {#if detail}<span class="sub">{detail}</span>{/if}
          </span>
        </div>
      {/each}
    {:else}
      {#if ssh.loaded}
        <div class="note">
          {#if filter}
            No hosts match “{filter}”.
          {:else}
            <strong>No SSH hosts</strong>
            Hosts from <code>~/.ssh/config</code> (and the files it includes) show here, along with servers
            you’ve connected to before.
          {/if}
        </div>
      {/if}
    {/each}
    {#each ssh.problems as p (p)}
      <div class="problem">{p}</div>
    {/each}
  </div>

  {#if chosen.length > 1}
    <footer>
      <span>{chosen.length} selected</span>
      <button class="primary" onclick={() => open(chosen, 'group')} title="Opens each host in a tab, grouped as “{groupName(chosen)}”">
        Open as Tab Group
      </button>
    </footer>
  {/if}
</div>

<style>
  .ssh {
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
    padding: 0 6px 4px 8px;
  }
  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 0;
    height: 26px;
    padding: 0 8px;
    border-radius: 7px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
  }
  .filter input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--chrome-text);
    font: inherit;
    font-size: 12px;
    -webkit-user-select: text;
    user-select: text;
  }
  .filter input:focus {
    outline: none;
  }
  .filter input::placeholder {
    color: var(--chrome-muted);
  }
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
  .tool:hover:not(:disabled) {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tool:disabled {
    opacity: 0.4;
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }
  .list:focus {
    outline: none;
  }

  .section {
    padding: 10px 8px 4px;
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .section:first-child {
    padding-top: 4px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 34px;
    padding: 3px 6px 3px 8px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .row:hover {
    background: var(--tab-hover);
  }
  .row.on {
    background: var(--accent-soft);
  }
  .row:focus {
    outline: none;
  }

  .icon {
    display: grid;
    place-items: center;
    flex: none;
    color: var(--accent);
  }
  .icon.known {
    color: var(--chrome-muted);
  }

  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .alias {
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

  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex: none;
    padding: 8px 8px 2px;
    border-top: 1px solid var(--hairline);
    color: var(--chrome-muted);
    font-size: 11.5px;
  }
  .primary {
    height: 24px;
    padding: 0 10px;
    border: 0;
    border-radius: 6px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11.5px;
    font-weight: 600;
  }
  .primary:hover {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
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
  .problem {
    padding: 8px;
    color: var(--danger);
    font-size: 11px;
  }
  code {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11px;
  }
</style>
