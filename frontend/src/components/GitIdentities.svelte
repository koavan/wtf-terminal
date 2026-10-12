<script lang="ts">
  import { onMount } from 'svelte';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { gitIdentities, paneIdentity, type GitIdentity } from '../lib/gitIdentities.svelte';
  import { relativeTime } from '../lib/logs';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { message } from '../lib/sshAdmin.svelte';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // Git identities: who each tab works as on GitHub (an SSH key plus the
  // author to commit as). Using one changes only that tab's shell.

  let error = $state('');
  /** Identities being tested against GitHub, by id. */
  let testing = $state<Record<string, boolean>>({});

  onMount(() => {
    gitIdentities.load().catch((e) => (error = message(e)));
  });

  const current = $derived(paneIdentity(tabs.activePane));

  async function test(i: GitIdentity) {
    testing = { ...testing, [i.id]: true };
    try {
      const r = await gitIdentities.test(i);
      toast.show(r.ok ? `${i.name} logs in to GitHub as ${r.user}` : `${i.name}: ${r.message}`, r.ok ? undefined : 10000);
    } catch (e) {
      toast.show(message(e), 6000);
    } finally {
      testing = { ...testing, [i.id]: false };
    }
  }

  async function copyPublic(i: GitIdentity): Promise<boolean> {
    try {
      await Clipboard.SetText(await Service.PublicKey(i.keyPath));
      return true;
    } catch (e) {
      toast.show(message(e), 6000);
      return false;
    }
  }

  /** GitHub's own page for adding a key; the user pastes it there while signed in as that account. */
  async function addToGitHub(i: GitIdentity) {
    if (!(await copyPublic(i))) return;
    toast.show(`Copied ${keyName(i)}.pub. Paste it on GitHub while signed in as the account for ${i.name}`, 8000);
    Browser.OpenURL('https://github.com/settings/ssh/new');
  }

  function confirmDelete(i: GitIdentity) {
    overlay.confirm({
      title: `Delete the identity ${i.name}?`,
      message:
        `wtf forgets it. The key stays in ${prettyPath(i.keyPath)}, and GitHub keeps it. ` +
        'Tabs using it keep their variables until they change them or close.',
      items: [],
      confirmLabel: 'Delete Identity',
      onConfirm: () => gitIdentities.remove(i.id).catch((e) => toast.show(message(e), 6000)),
    });
  }

  function menu(e: MouseEvent, i: GitIdentity) {
    e.preventDefault();
    const entries: MenuEntry[] = [
      { label: current === i.name ? 'Use Again in This Tab' : 'Use in This Tab', hint: 'double-click', action: () => gitIdentities.use(i) },
      { label: `Open New Tab as ${i.name}`, action: () => gitIdentities.openTab(i) },
    ];
    if (current === i.name) entries.push({ label: 'Stop Using in This Tab', action: () => gitIdentities.stop() });
    entries.push(
      'separator',
      { label: 'Test GitHub Login', action: () => test(i) },
      { label: 'Add Key to GitHub…', action: () => addToGitHub(i) },
      { label: 'Copy Public Key', action: () => copyPublic(i).then((ok) => ok && toast.show(`Copied ${keyName(i)}.pub`)) },
      'separator',
      { label: 'Edit…', action: () => overlay.show({ kind: 'gitIdentity', identity: i }) },
      { label: 'Delete…', danger: true, action: () => confirmDelete(i) },
    );
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  const keyName = (i: GitIdentity) => prettyPath(i.keyPath).replace(/^~\/\.ssh\//, '');

  function author(i: GitIdentity): string {
    if (i.authorName && i.authorEmail) return `${i.authorName} <${i.authorEmail}>`;
    return i.authorName || i.authorEmail || 'author from git config';
  }

  function tooltip(i: GitIdentity): string {
    return [
      `Commits as: ${author(i)}`,
      `SSH key: ${prettyPath(i.keyPath)} (offered alone: IdentitiesOnly)`,
      i.githubUser
        ? `GitHub account: ${i.githubUser}, as of the last test (${relativeTime(new Date(i.testedAt))})`
        : 'GitHub account: not tested yet (right-click → Test GitHub Login)',
    ].join('\n');
  }
</script>

<div class="identities">
  <header>
    <span class="title">Git identities</span>
    <button class="tool" title="New identity" aria-label="New identity" onclick={() => overlay.show({ kind: 'gitIdentity', identity: null })}>
      <Icon name="plus" size={14} />
    </button>
  </header>

  {#if current}
    <div class="current">
      <span>This tab: <strong>{current}</strong></span>
      <button class="link" onclick={() => gitIdentities.stop()}>Stop Using</button>
    </div>
  {/if}

  <div class="list" role="list">
    {#each gitIdentities.list as i (i.id)}
      <div
        class="row"
        class:on={current === i.name}
        role="listitem"
        title={tooltip(i)}
        oncontextmenu={(e) => menu(e, i)}
        ondblclick={() => gitIdentities.use(i)}
      >
        <span class="icon"><Icon name="key" size={13} /></span>
        <span class="main">
          <span class="name">{i.name}</span>
          <span class="sub">{author(i)} · {keyName(i)}</span>
        </span>
        <span class="pills">
          {#if testing[i.id]}
            <span class="pill">testing…</span>
          {:else if i.githubUser}
            <span class="pill gh" title="The GitHub account this key logged in as at the last test">@{i.githubUser}</span>
          {/if}
          {#if current === i.name}<span class="pill on">this tab</span>{/if}
        </span>
      </div>
    {:else}
      {#if gitIdentities.loaded}
        <div class="note">
          <strong>Work on GitHub as more than one user</strong>
          An identity pairs an SSH key with the name and email to commit as. Use one in a tab and git in that tab
          pushes and commits as that user, while other tabs stay as they are. Press + to add one.
        </div>
      {/if}
    {/each}
    {#if error}<div class="problem">{error}</div>{/if}
  </div>
</div>

<style>
  .identities {
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
  .current {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex: none;
    margin: 0 8px 4px;
    padding: 5px 8px;
    border-radius: 7px;
    background: var(--accent-soft);
    color: var(--chrome-text);
    font-size: 11.5px;
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font-size: 11.5px;
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
    color: var(--chrome-muted);
  }
  .row.on .icon {
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
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 12px;
    font-weight: 600;
  }
  .sub {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--chrome-muted);
    font-size: 11px;
  }
  .pills {
    display: flex;
    gap: 3px;
    flex: none;
  }
  .pill {
    padding: 0 5px;
    border-radius: 4px;
    background: var(--tab-hover);
    color: var(--chrome-muted);
    font-size: 10px;
    font-weight: 600;
    line-height: 16px;
  }
  .pill.on {
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
  .problem {
    padding: 8px;
    color: var(--danger);
    font-size: 11px;
  }
</style>
