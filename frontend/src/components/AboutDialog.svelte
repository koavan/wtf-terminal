<script lang="ts">
  import { onMount } from 'svelte';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import { Service, type Info } from '../../bindings/github.com/dayanraj/wtf/internal/about';
  import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
  import appIcon from '../assets/appicon.png';
  import Dialog from './Dialog.svelte';
  import { prettyPath } from '../lib/backend';
  import { openManual } from '../lib/help';
  import { overlay } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { toast } from '../lib/toast.svelte';

  // About wtf: what it is, which build this is, and the facts about this
  // setup that a bug report or a fix by hand needs.

  const TAGLINE = 'A terminal for all the What-The-Fuck moments';

  let info = $state<Info | null>(null);

  onMount(() => {
    Service.Info()
      .then((i) => (info = i))
      .catch(() => {});
  });

  const FEATURES: { label: string; topic: string }[] = [
    { label: 'Tabs, groups & splits', topic: 'tabs' },
    { label: 'Command blocks', topic: 'shell-integration' },
    { label: 'Log-friendly output', topic: 'logs' },
    { label: 'Pinned output', topic: 'pins' },
    { label: 'Hide Secrets', topic: 'secrets' },
    { label: 'SSH', topic: 'ssh-hosts' },
    { label: 'AWS profiles', topic: 'aws' },
    { label: 'Kubernetes', topic: 'kubernetes' },
    { label: 'Snippets & runbooks', topic: 'snippets' },
    { label: 'Workspaces', topic: 'workspaces' },
    { label: 'Port forwards', topic: 'network' },
    { label: 'Broadcast input', topic: 'broadcast' },
  ];

  const shellName = (path: string) => path.slice(path.lastIndexOf('/') + 1);

  /** The facts to paste into a bug report: versions and platform, no paths. */
  function diagnostics(i: Info): string {
    return [
      `wtf ${i.version} (${i.commit}${i.buildDate ? `, built ${i.buildDate}` : ''})`,
      `${i.os} · ${i.arch}`,
      `Go ${i.goVersion}${i.wailsVersion ? ` · Wails ${i.wailsVersion}` : ''}`,
      `Shell: ${shellName(i.shell)}, shell integration ${i.shellIntegration ? 'on' : 'off'}`,
    ].join('\n');
  }

  function copyDiagnostics() {
    if (!info) return;
    Clipboard.SetText(diagnostics(info)).then(() => toast.show('Copied version and system details'));
  }

  function manual(topic: string) {
    overlay.close();
    openManual(topic);
  }

  const reveal = (path: string) => Files.Reveal(path).catch(() => toast.show(`${prettyPath(path)} doesn’t exist yet`));
</script>

<Dialog title="" label="About wtf" width={520}>
  <div class="hero">
    <img src={appIcon} alt="" width="72" height="72" />
    <div>
      <h1>wtf</h1>
      <p class="tagline">{TAGLINE}</p>
      {#if info}
        <p class="build" title="The git commit this build was made from">
          Version {info.version} · {info.commit}{info.buildDate ? ` · built ${info.buildDate}` : ''}
        </p>
      {/if}
    </div>
  </div>

  <p class="what">
    A browser-like terminal for DevOps work: tabs you can group, split and move between windows, an address bar that says which
    cluster and AWS account each shell points at, and a side pane for your files, SSH hosts, AWS profiles, Kubernetes clusters,
    snippets, workspaces, port forwards and processes.
  </p>

  <div class="features">
    {#each FEATURES as f (f.topic + f.label)}
      <button class="chip" onclick={() => manual(f.topic)} title="Open the manual at {f.label}">{f.label}</button>
    {/each}
  </div>

  {#if info}
    <dl>
      <dt>Shell</dt>
      <dd>{shellName(info.shell)} <span class="muted">({prettyPath(info.shell)})</span> · shell integration {info.shellIntegration ? 'on' : 'off'}</dd>
      <dt>System</dt>
      <dd>{info.os} · {info.arch}</dd>
      <dt>Built with</dt>
      <dd>Go {info.goVersion.replace(/^go/, '')}{info.wailsVersion ? ` · Wails ${info.wailsVersion}` : ''}</dd>
      <dt>Settings</dt>
      <dd><button class="path" onclick={() => reveal(info!.configFile)} title="Show in {isMac ? 'Finder' : 'the file manager'}">{prettyPath(info.configFile)}</button></dd>
      <dt>Snippets</dt>
      <dd><button class="path" onclick={() => reveal(info!.snippetsFile)} title="Show in {isMac ? 'Finder' : 'the file manager'}">{prettyPath(info.snippetsFile)}</button></dd>
      <dt>Session</dt>
      <dd><button class="path" onclick={() => reveal(info!.stateDir)} title="Saved windows and tabs, port forwards">{prettyPath(info.stateDir)}</button></dd>
      <dt>Backups</dt>
      <dd>
        <button class="path" onclick={() => reveal(info!.cacheDir)} title="Copies of ssh, AWS and kubeconfig files kept before wtf changed them, and deleted SSH keys">
          {prettyPath(info.cacheDir)}
        </button>
      </dd>
    </dl>
  {/if}

  <p class="credits">
    Built with <a href="https://wails.io" onclick={(e) => (e.preventDefault(), Browser.OpenURL('https://wails.io'))}>Wails</a>,
    <a href="https://svelte.dev" onclick={(e) => (e.preventDefault(), Browser.OpenURL('https://svelte.dev'))}>Svelte</a> and
    <a href="https://xtermjs.org" onclick={(e) => (e.preventDefault(), Browser.OpenURL('https://xtermjs.org'))}>xterm.js</a>. Icons from Lucide (ISC). Font: JetBrains Mono (OFL).
  </p>

  {#snippet buttons()}
    <button class="secondary" onclick={copyDiagnostics} disabled={!info} title="Version and system details for a bug report (no file paths)">Copy Diagnostics</button>
    <span class="spacer"></span>
    {#if info}<button class="secondary" onclick={() => Browser.OpenURL(info!.repository)}>GitHub</button>{/if}
    <button class="secondary" onclick={() => manual('shortcuts')}>Shortcuts</button>
    <button class="primary" onclick={() => manual('getting-started')}>Manual</button>
  {/snippet}
</Dialog>

<style>
  .hero {
    display: flex;
    align-items: center;
    gap: 16px;
    margin: 0 0 12px;
  }
  .hero img {
    flex: none;
    border-radius: 16px;
  }
  h1 {
    margin: 0;
    font: 700 26px 'JetBrains Mono Variable', monospace;
    letter-spacing: -0.02em;
  }
  .tagline {
    margin: 2px 0 4px;
    font-size: 13.5px;
    font-weight: 550;
  }
  .build {
    margin: 0;
    color: var(--text-muted);
    font: 11.5px 'JetBrains Mono Variable', monospace;
    -webkit-user-select: text;
    user-select: text;
  }
  .what {
    margin: 0 0 10px;
    color: var(--text-muted);
    font-size: 12.5px;
    line-height: 1.5;
  }
  .features {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    margin-bottom: 14px;
  }
  .chip {
    height: 22px;
    padding: 0 9px;
    border: 0;
    border-radius: 11px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11.5px;
    font-weight: 600;
  }
  .chip:hover {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
  }
  dl {
    display: grid;
    grid-template-columns: 82px 1fr;
    gap: 5px 10px;
    margin: 0 0 12px;
    font-size: 12px;
  }
  dt {
    color: var(--text-muted);
    font-weight: 600;
  }
  dd {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .muted {
    color: var(--text-muted);
  }
  .path {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: 11.5px 'JetBrains Mono Variable', monospace;
    text-align: left;
    overflow-wrap: anywhere;
  }
  .path:hover {
    text-decoration: underline;
  }
  .credits {
    margin: 0;
    color: var(--text-muted);
    font-size: 11px;
  }
  .credits a {
    color: inherit;
    text-decoration: underline;
  }
</style>
