// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};
Element.prototype.scrollIntoView ??= function () {};
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' }, Windows: {}, closeThisWindow: vi.fn(), moveTabToNewWindow: vi.fn() }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/snippets', () => ({
  Service: { List: vi.fn(), PlanEdit: vi.fn(), ApplyEdit: vi.fn(), Delete: vi.fn(), OpenUserFile: vi.fn(async () => {}) },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({ Service: { Themes: vi.fn(async () => []) } }));

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/snippets');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { registerTerminal } = await import('../lib/terminals');
const sn = await import('../lib/snippets.svelte');
const Overlays = (await import('./Overlays.svelte')).default;

type Snippet = import('../lib/snippets.svelte').Snippet;
const snippet = (s: Partial<Snippet>): Snippet => ({
  id: 'f#0', name: '', description: '', tags: [], command: '', steps: [], params: [], source: '/home/me/.config/wtf/snippets.toml', index: 0, project: false, ...s,
});
const RESTART = snippet({ id: 'a', name: 'Restart a deployment', command: 'kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}' });
const DRAIN = snippet({
  id: 'b', name: 'Drain a node', steps: [{ title: 'Cordon', command: 'kubectl cordon {{node}}' }, { title: 'Drain', command: 'kubectl drain {{node}} {{flags}}' }],
  params: [{ name: 'node', description: 'The node', default: '', choices: ['n1', 'n2'], raw: false }, { name: 'flags', description: '', default: '--ignore-daemonsets', choices: [], raw: true }],
});
const LS = snippet({ id: 'c', name: 'List here', command: 'ls -la', project: true, source: '/w/.wtf/snippets.toml' });
const USER = '/home/me/.config/wtf/snippets.toml';
const LIST = {
  snippets: [RESTART, { ...DRAIN, index: 1 }, LS],
  files: ['/w/.wtf/snippets.toml', USER],
  problems: [],
  userFile: USER,
  projectFile: '/w/.wtf/snippets.toml',
  targets: [{ path: '/w/.wtf/snippets.toml', kind: 'project', exists: true }, { path: USER, kind: 'user', exists: true }],
  versions: { [USER]: 'v1', '/w/.wtf/snippets.toml': 'p1' },
};

const typed: string[] = [];
beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/w' });
  const pane = focusedPane(tabs.active!);
  pane.cwd = '/w';
  pane.infra = { kubeContext: 'prod', kubeNamespace: 'shop', awsProfile: 'devops', awsRegion: 'us-east-1' } as never;
  overlay.current = null;
  typed.length = 0;
  registerTerminal(pane.id, {
    focus() {}, clear() {}, selectAll() {}, getSelection: () => '', paste() {}, sendInput() {},
    type: (t) => typed.push(t),
    jumpPrompt() {}, revealCommand: () => true, lastOutput: () => null, serialize: () => '', snapshot: () => [],
  });
  vi.clearAllMocks();
  vi.mocked(Service.List).mockResolvedValue(structuredClone(LIST) as never);
});
afterEach(cleanup);

describe('filling in snippets', () => {
  it('finds placeholders in order, with defaults from the line or the file', () => {
    expect(sn.fieldsOf(RESTART).map((f) => [f.name, f.default])).toEqual([['ns', '$kube_namespace'], ['name', '']]);
    expect(sn.fieldsOf(DRAIN).map((f) => [f.name, f.default, f.raw])).toEqual([['node', '', false], ['flags', '--ignore-daemonsets', true]]);
  });

  it('leaves Go templates alone', () => {
    const s = snippet({ command: `kubectl get pods -o go-template='{{range .items}}{{.metadata.name}}{{"\\n"}}{{end}}' -n {{ns}}` });
    expect(sn.fieldsOf(s).map((f) => f.name)).toEqual(['ns']);
    expect(sn.fill(s.command, { ns: 'shop' }, sn.fieldsOf(s))).toContain('{{.metadata.name}}');
  });

  it('fills in values, quoting them unless raw', () => {
    const f = sn.fieldsOf(DRAIN);
    expect(sn.fill('kubectl drain {{node}} {{flags}}', { node: "my node's", flags: '--force --grace-period=0' }, f)).toBe(
      `kubectl drain 'my node'\\''s' --force --grace-period=0`,
    );
    expect(() => sn.fill('kubectl cordon {{node}}', { node: ' ' }, f)).toThrow('Fill in node');
  });

  it('takes $variables from the focused shell', () => {
    const facts = sn.shellFacts();
    expect(facts).toMatchObject({ kube_namespace: 'shop', kube_context: 'prod', aws_profile: 'devops', aws_region: 'us-east-1', cwd: '/w' });
    expect(sn.resolveDefault('$kube_namespace', facts)).toBe('shop');
    expect(sn.resolveDefault('$nope', facts)).toBe('$nope');
    expect(sn.resolveDefault('literal', facts)).toBe('literal');
  });
});

describe('Run Snippet', () => {
  it('lists snippets in the palette and puts a plain one straight at the prompt', async () => {
    const view = render(Overlays);
    overlay.togglePalette('snippets');
    await view.findByText('Restart a deployment');
    view.getByText('Open Snippets File…');
    expect(Service.List).toHaveBeenCalledWith('/w');
    await fireEvent.click(view.getByText('List here'));
    expect(typed).toEqual(['\x15ls -la']); // inserted, not run
  });

  it('fills a snippet in, previews it, and inserts or runs it', async () => {
    const view = render(Overlays);
    sn.openSnippet(RESTART);
    const ns = (await view.findByDisplayValue('shop')) as HTMLInputElement;
    view.getByText('Fill in name');
    const inputs = [...document.querySelectorAll('.dialog input')] as HTMLInputElement[];
    await fireEvent.input(inputs[1], { target: { value: 'web' } });
    view.getByText('kubectl -n shop rollout restart deploy/web');
    await fireEvent.input(ns, { target: { value: 'staging' } });
    await fireEvent.click(view.getByText('Run'));
    expect(typed).toEqual(['\x15kubectl -n staging rollout restart deploy/web\r']);
    expect(overlay.current).toBeNull();
  });

  it('runs a runbook one step at a time', async () => {
    const view = render(Overlays);
    sn.openSnippet(DRAIN);
    await view.findByText('Cordon');
    view.getByText('kubectl cordon n1'); // first choice
    view.getByText('kubectl drain n1 --ignore-daemonsets');
    const runs = view.getAllByText('Run');
    await fireEvent.click(runs[0]);
    expect(typed).toEqual(['\x15kubectl cordon n1\r']);
    expect(overlay.current?.kind).toBe('snippet'); // stays open for the next step
    expect(document.querySelector('li.done .title')?.textContent).toBe('Cordon');
  });

  it('refuses to type into a busy shell', async () => {
    focusedPane(tabs.active!).running = true;
    expect(sn.runSnippetCommand('ls', 'run')).toBe(false);
    expect(typed).toEqual([]);
    expect(sn.runSnippetCommand('ls', 'tab')).toBe(true);
    expect(focusedPane(tabs.active!).initialCommand ?? focusedPane(tabs.list.at(-1)!).initialCommand).toBe('ls');
  });
});

describe('the snippet editor', () => {
  const fieldIn = (label: string) =>
    [...document.querySelectorAll('.dialog label.field')].find((l) => l.textContent?.trim().startsWith(label))!.querySelector('input, select') as HTMLInputElement;

  it('saves a command as a snippet, after showing the block to be written', async () => {
    vi.mocked(Service.PlanEdit).mockResolvedValue({ file: USER, text: '[[snippet]]\nname = "Pods"\n', newFile: false });
    vi.mocked(Service.ApplyEdit).mockResolvedValue(LIST as never);
    const view = render(Overlays);
    overlay.show({ kind: 'snippetEdit', snippet: null, command: 'kubectl get pods -n shop' });
    await view.findByText('New Snippet');
    await waitFor(() => expect(fieldIn('Save to').value).toBe(USER));
    expect([...document.querySelectorAll('.dialog option')].map((o) => o.textContent)).toEqual([
      '/w/.wtf/snippets.toml — this project’s, for git',
      '~/.config/wtf/snippets.toml — yours',
    ]);
    await fireEvent.input(fieldIn('Name'), { target: { value: 'Pods' } });
    await fireEvent.input(fieldIn('Command'), { target: { value: 'kubectl get pods -n {{ns=$kube_namespace}}' } });
    // The placeholder gets its settings row.
    await view.findByText('ns');
    const param = document.querySelector('.param')!;
    await fireEvent.input(param.querySelectorAll('input')[0], { target: { value: 'The namespace' } });

    await fireEvent.click(view.getByText('Review…'));
    await view.findByText(/name = "Pods"/);
    expect(Service.ApplyEdit).not.toHaveBeenCalled();
    const sent = vi.mocked(Service.PlanEdit).mock.calls[0];
    expect(sent[1]).toBe('/w');
    expect(sent[0]).toMatchObject({
      file: USER, index: -1, version: 'v1',
      snippet: { name: 'Pods', command: 'kubectl get pods -n {{ns=$kube_namespace}}', steps: [], params: [{ name: 'ns', description: 'The namespace', default: '', choices: [], raw: false }] },
    });
    await fireEvent.click(view.getByText('Confirm & Save'));
    await waitFor(() => expect(Service.ApplyEdit).toHaveBeenCalledWith(sent[0], '/w'));
  });

  it('edits a runbook in place: its file and position, steps reordered', async () => {
    vi.mocked(Service.PlanEdit).mockResolvedValue({ file: USER, text: '[[snippet]]\n', newFile: false });
    const view = render(Overlays);
    overlay.show({ kind: 'snippetEdit', snippet: { ...DRAIN, index: 1 } });
    await view.findByText('Edit “Drain a node”');
    expect(view.queryByText('Save to')).toBeNull(); // stays in its own file
    await fireEvent.click(view.getByLabelText('Move step 2 up'));
    await fireEvent.click(view.getByText('Review…'));
    await waitFor(() => expect(Service.PlanEdit).toHaveBeenCalled());
    const e = vi.mocked(Service.PlanEdit).mock.calls[0][0];
    expect(e).toMatchObject({ file: USER, index: 1, version: 'v1' });
    expect(e.snippet.steps!.map((st) => st.title)).toEqual(['Drain', 'Cordon']);
    expect(e.snippet.params!.find((p) => p.name === 'node')).toMatchObject({ description: 'The node', choices: ['n1', 'n2'] });
  });
});

describe('the Snippets tab', () => {
  it('lists snippets by file and runs, edits or deletes them', async () => {
    vi.mocked(Service.Delete).mockResolvedValue({ ...LIST, snippets: [RESTART, LS] } as never);
    const SnippetsView = (await import('./SnippetsView.svelte')).default;
    const view = render(SnippetsView);
    render(Overlays);
    await view.findByText('Restart a deployment');
    expect([...view.container.querySelectorAll('.section')].map((e) => e.textContent)).toEqual(['This project · /w/.wtf/snippets.toml', 'Yours']);
    view.getByText('2 steps');
    view.getByText('Cordon → Drain');

    await fireEvent.click(view.getByLabelText('Run List here'));
    expect(typed).toEqual(['\x15ls -la']);

    await fireEvent.contextMenu(view.getByText('Drain a node'));
    const entries = (overlay.current as { entries: ({ label?: string; action?: () => void } | string)[] }).entries;
    expect(entries.map((e) => (typeof e === 'string' ? e : e.label))).toEqual(['Run…', 'separator', 'Edit…', 'Duplicate…', 'separator', 'Delete…']);
    (entries.find((e) => typeof e !== 'string' && e.label === 'Delete…') as { action: () => void }).action();
    await fireEvent.click(await view.findByText('Delete Snippet'));
    expect(Service.Delete).toHaveBeenCalledWith(USER, 1, 'v1', '/w');
    await waitFor(() => expect(view.queryByText('Drain a node')).toBeNull());
  });
});
