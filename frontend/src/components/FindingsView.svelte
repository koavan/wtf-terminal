<script lang="ts">
  import { untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { Browser, Clipboard } from '@wailsio/runtime';
  import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/findings';
  import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
  import Icon from './Icon.svelte';
  import { prettyPath } from '../lib/backend';
  import { openInEditor } from '../lib/fileActions';
  import {
    findings, group, SEVERITY_LABEL, severityTone, where, type Finding, type RuleGroup, type Source,
  } from '../lib/findings.svelte';
  import { relativeTime } from '../lib/logs';
  import { overlay, type MenuEntry } from '../lib/overlay.svelte';
  import { isMac } from '../lib/platform';
  import { tabs } from '../lib/tabs.svelte';
  import { toast } from '../lib/toast.svelte';

  // Security scanners' findings for the focused shell's repository (see
  // lib/findings). Read when the tab opens, when the shell moves to another
  // repository, on refresh, and when a scan finishes; never polled.

  let filter = $state('');
  /** Rules opened to show where they were found. */
  const open = new SvelteSet<string>();
  /** Severities folded away. */
  const folded = new SvelteSet<string>();

  const cwd = $derived(tabs.activePane?.cwd ?? '');
  // Read when the tab opens (a scan may have run elsewhere), then again only
  // when the focused shell moves to another repository.
  let opened = false;
  $effect(() => {
    const folder = cwd;
    untrack(() => {
      if (folder && (!opened || findings.stale(folder))) findings.load(folder);
      if (folder) opened = true;
    });
  });

  const report = $derived(findings.report);
  const root = $derived(report?.root ?? '');
  const groups = $derived(group(report?.findings ?? [], filter));
  const total = $derived(report?.findings.length ?? 0);

  function copy(text: string, what: string) {
    Clipboard.SetText(text).then(() => toast.show(`Copied ${what}`));
  }

  const fail = (verb: string) => (e: unknown) => toast.show(`Couldn't ${verb}: ${e instanceof Error ? e.message : e}`);

  /** A location wtf can open: a file path, not a package URL or such. */
  const isFile = (f: Finding) => f.path.startsWith('/');

  function openFinding(f: Finding) {
    if (isFile(f)) openInEditor(f.path, f.line || undefined);
  }

  function toggle(set: SvelteSet<string>, key: string) {
    if (set.has(key)) set.delete(key);
    else set.add(key);
  }

  function findingMenu(e: MouseEvent, f: Finding) {
    e.preventDefault();
    const at = where(f, root);
    const entries: MenuEntry[] = [];
    if (isFile(f)) {
      entries.push(
        { label: f.line ? `Open in Editor at Line ${f.line}` : 'Open in Editor', hint: 'click', action: () => openFinding(f) },
        { label: isMac ? 'Reveal in Finder' : 'Open Containing Folder', action: () => Files.Reveal(f.path).catch(fail('reveal it')) },
        'separator',
        { label: 'Copy Path', action: () => copy(f.path, 'path') },
      );
      if (f.line) entries.push({ label: `Copy ${at}`, action: () => copy(at, 'path and line') });
    } else if (f.path) {
      entries.push({ label: 'Copy Location', action: () => copy(f.path, 'location') });
    }
    if (entries.length) entries.push('separator');
    entries.push({ label: 'Copy Message', action: () => copy(f.message, 'message') });
    entries.push(...ruleEntries({ tool: f.tool, ruleId: f.ruleId, helpUri: f.helpUri }));
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  function ruleEntries(r: { tool: string; ruleId: string; helpUri: string }): MenuEntry[] {
    const out: MenuEntry[] = [];
    if (r.ruleId) out.push({ label: 'Copy Rule ID', action: () => copy(r.ruleId, 'rule ID') });
    if (/^https?:\/\//.test(r.helpUri)) out.push({ label: 'Open Rule Documentation', action: () => Browser.OpenURL(r.helpUri) });
    return out;
  }

  function ruleMenu(e: MouseEvent, r: RuleGroup) {
    e.preventDefault();
    overlay.openMenu(e.clientX, e.clientY, [
      { label: open.has(r.key) ? 'Hide Locations' : 'Show Locations', hint: 'click', action: () => toggle(open, r.key) },
      'separator',
      ...ruleEntries(r),
    ]);
  }

  function sourceMenu(e: MouseEvent, s: Source) {
    e.preventDefault();
    const entries: MenuEntry[] = [
      { label: isMac ? 'Reveal in Finder' : 'Open Containing Folder', action: () => Files.Reveal(s.path).catch(fail('reveal it')) },
      { label: 'Copy Path', action: () => copy(s.path, 'path') },
    ];
    if (s.fromScan) {
      entries.push('separator', {
        label: 'Remove These Results…',
        danger: true,
        action: () =>
          overlay.confirm({
            title: `Remove the ${s.tool} results?`,
            message: `Deletes ${prettyPath(s.path)} from wtf's cache. Run the scan again to get them back.`,
            items: [],
            confirmLabel: 'Remove',
            onConfirm: () => Service.Forget(s.path).then(() => findings.reload()).catch(fail('remove them')),
          }),
      });
    }
    overlay.openMenu(e.clientX, e.clientY, entries);
  }

  const ago = (iso: string) => (iso ? relativeTime(new Date(iso)) : '');
  const firstLine = (s: string) => s.split('\n', 1)[0];
</script>

<div class="findings">
  <header>
    <span class="root" title={root ? `Findings for ${prettyPath(root)}` : ''}>{root ? prettyPath(root).split('/').pop() : 'Findings'}</span>
    <button class="tool" title="Run a security scan" aria-label="Run a security scan" onclick={() => overlay.show({ kind: 'scan' })}>
      <Icon name="play" size={12} stroke={2} />
    </button>
    <button class="tool" title="Read the results again" aria-label="Refresh" disabled={findings.loading || !cwd} onclick={() => findings.load(cwd)}>
      <Icon name="refresh" size={13} />
    </button>
  </header>

  {#if total}
    <div class="filter">
      <Icon name="search" size={12} stroke={2} />
      <input bind:value={filter} placeholder="Filter by rule, file, message, tool" spellcheck="false" autocomplete="off" />
    </div>
  {/if}

  <div class="list">
    {#if findings.error}
      <div class="note error" role="alert">{findings.error}</div>
    {:else if !report}
      <div class="note">{cwd ? 'Reading results…' : 'Focus a terminal to see findings for its folder.'}</div>
    {:else}
      {#if report.sources.length}
        <div class="sources">
          {#each report.sources as s (s.path)}
            <div
              class="source"
              role="listitem"
              title="{prettyPath(s.path)}{s.fromScan ? '\nFrom a scan run in wtf (kept in its cache)' : ''}"
              oncontextmenu={(e) => sourceMenu(e, s)}
            >
              <span class="tool-name">{s.tool}{s.version ? ` ${s.version}` : ''}</span>
              {#if s.error}
                <span class="bad">{s.error}</span>
              {:else}
                <span>{s.count} {s.count === 1 ? 'finding' : 'findings'} · {ago(s.modified)}</span>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
      {#if report.truncated}
        <div class="note">Showing the first {total} findings; {report.truncated} more are in the files.</div>
      {/if}

      {#each groups as g (g.severity)}
        <button class="severity {severityTone(g.severity)}" aria-expanded={!folded.has(g.severity)} onclick={() => toggle(folded, g.severity)}>
          <span class="chev" class:open={!folded.has(g.severity)}><Icon name="chevron" size={11} stroke={2.2} /></span>
          <span class="dot"></span>
          {SEVERITY_LABEL[g.severity] ?? g.severity}
          <span class="count">{g.count}</span>
        </button>
        {#if !folded.has(g.severity)}
          {#each g.rules as r (r.key)}
            <div
              class="rule"
              role="button"
              tabindex="-1"
              aria-expanded={open.has(r.key)}
              title="{r.ruleId}{r.description && r.description !== r.title ? `\n${r.description}` : ''}\n{r.tool}"
              onclick={() => toggle(open, r.key)}
              oncontextmenu={(e) => ruleMenu(e, r)}
              onkeydown={() => {}}
            >
              <span class="chev" class:open={open.has(r.key)}><Icon name="chevron" size={10} stroke={2.2} /></span>
              <span class="main">
                <span class="title">{r.title}</span>
                <span class="sub">{r.ruleId !== r.title ? `${r.ruleId} · ` : ''}{r.tool}</span>
              </span>
              <span class="count">{r.items.length}</span>
            </div>
            {#if open.has(r.key)}
              {#each r.items as f, i (i)}
                <div
                  class="hit"
                  class:openable={isFile(f)}
                  role="button"
                  tabindex="-1"
                  title="{f.message}{f.score ? `\nScore ${f.score}` : ''}"
                  onclick={() => openFinding(f)}
                  oncontextmenu={(e) => findingMenu(e, f)}
                  onkeydown={() => {}}
                >
                  <span class="where">{where(f, root) || 'no location'}</span>
                  {#if f.message && f.message !== r.title}<span class="msg">{firstLine(f.message)}</span>{/if}
                </div>
              {/each}
            {/if}
          {/each}
        {/if}
      {:else}
        <div class="note">
          {#if filter}
            No findings match “{filter}”.
          {:else if report.sources.length}
            <strong>No findings</strong>The scanners found nothing in {prettyPath(report.root)}.
          {:else}
            <strong>No scan results for {prettyPath(report.root)}</strong>
            Run a scanner — Trivy, Semgrep, Checkov, Gitleaks or Grype — and its findings show here, grouped by
            severity. A <code>.sarif</code> file at the top of the folder (from CI, say) is read too.
            <button class="primary" onclick={() => overlay.show({ kind: 'scan' })}>Run Scan…</button>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .findings {
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
    height: 30px;
    padding: 0 6px 0 12px;
  }
  .root {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--chrome-text);
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
    text-overflow: ellipsis;
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
  .tool:hover:not(:disabled) {
    background: var(--tab-hover);
    color: var(--chrome-text);
  }
  .tool:disabled {
    opacity: 0.4;
  }

  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    height: 26px;
    margin: 2px 8px 4px;
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

  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 2px 6px 12px;
  }

  .sources {
    display: flex;
    flex-direction: column;
    gap: 1px;
    margin: 0 0 6px;
  }
  .source {
    display: flex;
    gap: 6px;
    padding: 3px 8px;
    border-radius: 6px;
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 11px;
    white-space: nowrap;
  }
  .source:hover {
    background: var(--tab-hover);
  }
  .tool-name {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .bad {
    overflow: hidden;
    color: var(--danger);
    text-overflow: ellipsis;
  }

  .severity {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    height: 26px;
    margin-top: 4px;
    padding: 0 8px 0 4px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--chrome-text);
    font-size: 11.5px;
    font-weight: 700;
    text-align: left;
  }
  .severity:hover {
    background: var(--tab-hover);
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--chrome-muted);
  }
  .severity.bad .dot {
    background: var(--danger);
  }
  .severity.warn .dot {
    background: var(--g-orange);
  }
  .count {
    margin-left: auto;
    color: var(--chrome-muted);
    font-size: 11px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .chev {
    display: grid;
    place-items: center;
    flex: none;
    width: 14px;
    color: var(--chrome-muted);
    transition: transform 0.12s ease;
  }
  .chev.open {
    transform: rotate(90deg);
  }

  .rule {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
    padding: 3px 8px 3px 12px;
    border-radius: 7px;
    color: var(--chrome-text);
    cursor: default;
  }
  .rule:hover,
  .hit:hover {
    background: var(--tab-hover);
  }
  .rule:focus,
  .hit:focus {
    outline: none;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .title,
  .sub {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .title {
    font-size: 12px;
  }
  .sub {
    color: var(--chrome-muted);
    font-size: 11px;
  }

  .hit {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 3px 8px 3px 32px;
    border-radius: 6px;
    cursor: default;
  }
  .hit.openable {
    cursor: pointer;
  }
  .where {
    overflow: hidden;
    color: var(--accent);
    font: 500 11.5px 'JetBrains Mono Variable', monospace;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .hit:not(.openable) .where {
    color: var(--chrome-text);
  }
  .msg {
    overflow: hidden;
    color: var(--chrome-muted);
    font-size: 11px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .note {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    padding: 14px 8px;
    color: var(--chrome-muted);
    font-size: 12px;
    line-height: 1.45;
  }
  .note strong {
    color: var(--chrome-text);
    font-weight: 600;
  }
  .note.error {
    color: var(--danger);
  }
  code {
    font-family: 'JetBrains Mono Variable', monospace;
    font-size: 11px;
  }
  .primary {
    height: 28px;
    margin-top: 4px;
    padding: 0 12px;
    border: 0;
    border-radius: 7px;
    background: var(--accent);
    color: #fff;
    font: inherit;
    font-size: 12px;
    font-weight: 600;
  }
  .primary:hover {
    filter: brightness(1.08);
  }
</style>
