<script lang="ts">
  import { onMount } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/awsprofiles';
  import ToolHint from './ToolHint.svelte';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { ago, aws, daysSince, focusedProfile, KIND_LABEL, shortKey, useCommand, useInShell, type Profile } from '../lib/aws.svelte';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { shellQuote } from '../lib/shell';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // AWS CLI profiles. Reading and checking are free; anything that changes
  // a file or AWS shows exactly what it will do and waits for a yes.

  let filter = $state('');

  onMount(() => {
    aws.load();
  });

  const shown = $derived(
    aws.profiles.filter((p) => {
      const q = filter.trim().toLowerCase();
      return !q || `${p.name} ${p.accessKeyId} ${p.region} ${p.account}`.toLowerCase().includes(q);
    }),
  );
  const inUse = $derived(tabs.activePane ? focusedProfile() : '');

  const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  function use(p: Profile) {
    const where = useInShell(p.name);
    toast.show(where === 'here' ? `This shell now uses ${p.name}` : `Opened a tab using ${p.name}`);
  }

  /** Plans a change, shows the plan, and makes it only on confirmation. */
  async function confirmChange(
    title: string,
    label: string,
    plan: () => Promise<string[] | null>,
    run: () => Promise<unknown>,
    done: string,
    note = '',
  ) {
    let steps: string[];
    try {
      steps = (await plan()) ?? [];
    } catch (e) {
      toast.show(message(e), 6000);
      return;
    }
    if (steps.length === 0) {
      toast.show('Nothing to change');
      return;
    }
    overlay.confirm({
      title,
      message: `wtf will make these changes${note}. A copy of each file is kept in wtf’s cache folder (aws-backups).`,
      items: steps,
      showAll: true,
      confirmLabel: label,
      onConfirm: () =>
        run()
          .then(() => toast.show(done))
          .catch((e) => toast.show(message(e), 6000)),
    });
  }

  function remove(p: Profile) {
    confirmChange(
      `Delete ${p.name}?`,
      'Delete Profile',
      () => Service.PlanDelete(p.name, aws.versions),
      async () => aws.apply(await Service.Delete(p.name, aws.versions)),
      `Deleted ${p.name}`,
      p.accessKeyId ? ` (the key stays valid in AWS; this only removes it from this computer)` : '',
    );
  }

  function makeDefault(p: Profile) {
    confirmChange(
      `Copy ${p.name} to default?`,
      'Copy to Default',
      () => Service.PlanMakeDefault(p.name, aws.versions),
      async () => aws.apply(await Service.MakeDefault(p.name, aws.versions)),
      `default now has ${p.name}’s settings`,
      ' — the CLI uses [default] when no profile is chosen',
    );
  }

  function menu(e: MouseEvent, p: Profile) {
    e.preventDefault();
    const entries: MenuEntry[] = [
      { label: 'Use in This Shell', hint: 'double-click', action: () => use(p) },
      { label: 'Open New Tab with Profile', action: () => tabs.open({ command: useCommand(p.name) }) },
    ];
    if (p.kind === 'sso') entries.push({ label: 'Log In with SSO', action: () => tabs.open({ command: `aws sso login --profile ${shellQuote(p.name)}` }) });
    entries.push('separator', { label: 'Check Identity (sts)', action: () => aws.verify(p.name) });
    if (p.kind === 'keys') entries.push({ label: 'Check Access Keys (iam)', action: () => aws.checkKeys(p.name) });
    entries.push('separator', { label: 'Edit…', action: () => overlay.show({ kind: 'awsProfile', profile: p }) });
    if (p.kind === 'keys' && p.accessKeyId.startsWith('AKIA')) {
      entries.push({ label: 'Rotate Access Key…', action: () => overlay.show({ kind: 'awsRotate', profile: p }) });
    }
    if (p.name !== 'default') entries.push({ label: 'Copy to Default…', action: () => makeDefault(p) });
    entries.push('separator');
    if (p.accessKeyId) entries.push({ label: 'Copy Access Key ID', action: () => copy(p.accessKeyId, 'access key ID') });
    if (p.account) entries.push({ label: `Copy Account ID (${p.account})`, action: () => copy(p.account, 'account ID') });
    entries.push({ label: 'Copy Name', action: () => copy(p.name, 'profile name') }, 'separator', {
      label: 'Delete…',
      danger: true,
      action: () => remove(p),
    });
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  /** "AKIA…WXYZ · us-east-1", or the role/SSO account. */
  function detail(p: Profile): string {
    const parts: string[] = [];
    if (p.kind === 'keys') parts.push(shortKey(p.accessKeyId) + (p.hasSessionToken ? ' (temporary)' : ''));
    else parts.push(KIND_LABEL[p.kind] ?? p.kind);
    if (p.account) parts.push(p.account);
    if (p.region) parts.push(p.region);
    return parts.join(' · ');
  }

  function tooltip(p: Profile): string {
    const lines = [`Kind: ${KIND_LABEL[p.kind] ?? p.kind}`];
    if (p.accessKeyId) lines.push(`Access key: ${p.accessKeyId}`);
    if (p.inCredentials) lines.push(`In ${prettyPath(aws.credentialsFile)}`);
    if (p.inConfig) lines.push(`In ${prettyPath(aws.configFile)}`);
    for (const s of p.credentialsOther) lines.push(`${s.key} = ${s.value} (in the credentials file)`);
    if (p.sourceFor.length) lines.push(`Source profile for ${p.sourceFor.join(', ')}`);
    return lines.join('\n');
  }
</script>

<div class="aws">
  <header>
    <div class="filter">
      <Icon name="search" size={12} stroke={2} />
      <input bind:value={filter} placeholder="Filter profiles" spellcheck="false" autocomplete="off" />
    </div>
    <button class="tool" title="Add profile" aria-label="Add profile" onclick={() => overlay.show({ kind: 'awsProfile', profile: null })}>
      <Icon name="plus" size={14} />
    </button>
    <button class="tool" title="Reload" aria-label="Reload" disabled={aws.loading} onclick={() => aws.load()}>
      <Icon name="refresh" size={13} />
    </button>
  </header>

  <div class="list" role="list">
    {#each shown as p (p.name)}
      {@const check = aws.checks.get(p.name)}
      <div class="row" role="listitem" title={tooltip(p)} oncontextmenu={(e) => menu(e, p)} ondblclick={() => use(p)}>
        <span class="icon" class:on={p.name === inUse}><Icon name="cloud" size={13} /></span>
        <span class="main">
          <span class="name">
            {p.name}
            {#if p.name === inUse}<span class="pill on" title="The focused shell uses this profile">in use</span>{/if}
          </span>
          <span class="sub">{detail(p)}</span>
          {#if check?.busy}
            <span class="check">Asking AWS…</span>
          {/if}
          {#if check?.identity}
            <span class="check ok" title={check.identity.arn}>{check.identity.arn.replace(/^arn:aws[\w-]*:(iam|sts)::/, '')}</span>
          {:else if check?.identityError}
            <span class="check bad">{check.identityError}<ToolHint error={check.identityError} /></span>
          {/if}
          {#if check?.keys}
            {#each check.keys as k (k.id)}
              <span class="check" title="Created {k.created}{k.lastUsed ? `\nLast used ${k.lastUsed}` : ''}">
                {shortKey(k.id)}{k.current ? ' (this one)' : ''} · {k.status.toLowerCase()} · {daysSince(k.created)} days old ·
                {k.lastUsed ? `used ${ago(k.lastUsed)}${k.lastUsedService ? ` (${k.lastUsedService})` : ''}` : 'never used'}
              </span>
            {/each}
          {:else if check?.keysError}
            <span class="check bad">{check.keysError}<ToolHint error={check.keysError} /></span>
          {/if}
        </span>
        <button
          class="use"
          title="Use in this shell"
          aria-label="Use {p.name}"
          onclick={(e) => {
            e.stopPropagation();
            use(p);
          }}
          ondblclick={(e) => e.stopPropagation()}
        >
          <Icon name="play" size={11} stroke={2} />
        </button>
      </div>
    {:else}
      {#if aws.loaded}
        <div class="note">
          {#if filter}
            No profiles match “{filter}”.
          {:else}
            <strong>No AWS profiles</strong>
            Profiles from <code>~/.aws/credentials</code> and <code>~/.aws/config</code> show here. Press + to add one.
          {/if}
        </div>
      {/if}
    {/each}
    {#each aws.problems as pr (pr)}
      <div class="problem">{pr}</div>
    {/each}
  </div>
</div>

<style>
  .aws {
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
  .icon {
    display: grid;
    place-items: center;
    flex: none;
    align-self: flex-start;
    margin-top: 4px;
    color: var(--chrome-muted);
  }
  .icon.on {
    color: var(--accent);
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font: 500 12px 'JetBrains Mono Variable', monospace;
  }
  .sub,
  .check {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--chrome-muted);
    font-size: 11px;
  }
  .check {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .check.ok {
    color: var(--mark-ok);
  }
  .check.bad {
    color: var(--danger);
  }
  .pill {
    padding: 0 5px;
    border-radius: 4px;
    font: 600 10px system-ui, sans-serif;
    line-height: 15px;
  }
  .pill.on {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .use {
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
    opacity: 0;
  }
  .row:hover .use {
    opacity: 1;
  }
  .use:hover {
    background: var(--accent-soft);
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
