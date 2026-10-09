// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' }, Windows: {}, closeThisWindow: vi.fn(), moveTabToNewWindow: vi.fn() }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/snippets', () => ({
  Service: { List: vi.fn(), PlanEdit: vi.fn(), ApplyEdit: vi.fn(), Delete: vi.fn(), OpenUserFile: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/windows', () => ({ Service: { NewWindow: vi.fn(async () => 'w2') } }));

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/snippets');
const { Service: Windows } = await import('../../bindings/github.com/dayanraj/wtf/internal/windows');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { leaves } = await import('../lib/layout');
const { overlay } = await import('../lib/overlay.svelte');
const ws = await import('../lib/workspaces.svelte');
const Overlays = (await import('./Overlays.svelte')).default;

type Workspace = import('../lib/snippets.svelte').Workspace;
const tab = (t: Partial<Workspace['tabs'][number]>) => ({ title: '', group: '', color: '', cwd: '', command: '', layout: '', panes: [], ...t });
const ONCALL: Workspace = {
  id: 'f#w0', name: 'Payments on call', description: '', window: '', source: '/home/me/.config/wtf/snippets.toml', index: 0, project: false,
  params: [{ name: 'ns', description: 'Namespace', default: '$kube_namespace', choices: [], raw: false }],
  tabs: [
    tab({ title: 'api-1', group: 'payments', color: 'red', command: 'ssh pay-api-1' }),
    tab({ title: 'api-2', group: 'payments', command: 'ssh pay-api-2' }),
    tab({ title: '{{ns}} pods', layout: 'column', panes: [{ cwd: '~/src/pay', command: 'kubectl -n {{ns}} get pods -w' }, { cwd: '', command: 'kubectl -n {{ns}} get events -w' }, { cwd: '/tmp', command: '' }] }),
  ],
};
const USER = '/home/me/.config/wtf/snippets.toml';
const LIST = {
  snippets: [], workspaces: [ONCALL], files: [USER], problems: [], userFile: USER, projectFile: '',
  targets: [{ path: USER, kind: 'user', exists: true }], versions: { [USER]: 'v1' },
};

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/w' });
  const pane = focusedPane(tabs.active!);
  pane.cwd = '/w';
  pane.infra = { kubeNamespace: 'shop', kubeContext: 'prod' } as never;
  overlay.current = null;
  vi.clearAllMocks();
  vi.mocked(Service.List).mockResolvedValue(structuredClone(LIST) as never);
});
afterEach(cleanup);

describe('planning and opening', () => {
  it('fills placeholders everywhere, quoting only commands', () => {
    expect(ws.workspaceFields(ONCALL).map((f) => f.name)).toEqual(['ns']);
    const plan = ws.planWorkspace(ONCALL, { ns: 'pay ments' });
    expect(plan.tabs[2].title).toBe('pay ments pods');
    expect(plan.tabs[2].panes[0]).toEqual({ cwd: '/home/me/src/pay', command: `kubectl -n 'pay ments' get pods -w` });
    expect(() => ws.planWorkspace(ONCALL, { ns: '' })).toThrow('Fill in ns');
  });

  it('opens tabs with groups, names, even splits and startup commands', () => {
    const before = tabs.list.length;
    ws.openPlan(ws.planWorkspace(ONCALL, { ns: 'shop' }));
    const opened = tabs.list.slice(before);
    expect(opened.map((t) => t.customTitle)).toEqual(['api-1', 'api-2', 'shop pods']);
    expect(tabs.groups).toHaveLength(1);
    expect(tabs.groups[0]).toMatchObject({ name: 'payments', color: 'red' });
    expect(opened.slice(0, 2).every((t) => t.groupId === tabs.groups[0].id)).toBe(true);
    expect(opened[2].groupId).toBeNull();

    const panes = leaves(opened[2].root);
    expect(panes.map((p) => [p.initialCwd, p.initialCommand])).toEqual([
      ['/home/me/src/pay', 'kubectl -n shop get pods -w'],
      ['/home/me/src/pay', 'kubectl -n shop get events -w'], // no folder: the tab's first
      ['/tmp', undefined],
    ]);
    const root = opened[2].root;
    expect(root.kind === 'split' && root.dir === 'column' && Math.abs(root.ratio - 1 / 3) < 1e-9).toBe(true);
    expect(opened[2].focusedPaneId).toBe(panes[0].id);
    expect(tabs.activeId).toBe(opened[0].id);
  });

  it('hands the plan to a new window when asked', async () => {
    await ws.openWorkspace({ ...ONCALL, window: 'new' }, { ns: 'shop' });
    const payload = JSON.parse(vi.mocked(Windows.NewWindow).mock.calls[0][0]);
    expect(payload.kind).toBe('workspace');
    expect(payload.plan.tabs).toHaveLength(3);
  });

  it('captures this window: names, groups, splits, folders, running commands', () => {
    const id = tabs.list[0].id;
    tabs.rename(id, 'main');
    const g = tabs.createGroup(id)!;
    g.name = 'ops';
    tabs.split('row', { cwd: '/home/me/logs' });
    const right = focusedPane(tabs.get(id)!);
    right.cwd = '/home/me/logs';
    right.running = true;
    right.command = 'tail -f app.log';
    const def = ws.captureWindow();
    expect(def.tabs![0]).toMatchObject({ title: 'main', group: 'ops', color: g.color, layout: 'row' });
    expect(def.tabs![0].panes).toEqual([{ cwd: '/w', command: '' }, { cwd: '~/logs', command: 'tail -f app.log' }]);
  });
});

describe('workspace dialogs', () => {
  it('previews every tab and opens on confirmation', async () => {
    const view = render(Overlays);
    overlay.show({ kind: 'workspaceOpen', workspace: ONCALL });
    await view.findByDisplayValue('shop'); // $kube_namespace from the focused shell
    view.getByText('kubectl -n shop get pods -w');
    view.getByText('3 panes, stacked');
    const before = tabs.list.length;
    await fireEvent.click(view.getByText('Open Here'));
    expect(tabs.list.length).toBe(before + 3);
    expect(overlay.current).toBeNull();
  });

  it('saves this window as a workspace after review', async () => {
    vi.mocked(Service.PlanEdit).mockResolvedValue({ file: USER, text: '[[workspace]]\nname = "Mine"\n', newFile: false });
    vi.mocked(Service.ApplyEdit).mockResolvedValue(LIST as never);
    const view = render(Overlays);
    overlay.show({ kind: 'workspaceEdit', workspace: null, draft: ws.captureWindow() });
    await view.findByText('New Workspace');
    await fireEvent.input(view.getByPlaceholderText('payments on call'), { target: { value: 'Mine' } });
    await fireEvent.click(view.getByText('Add Tab'));
    const cmds = view.getAllByPlaceholderText('startup command (optional)');
    await fireEvent.input(cmds.at(-1)!, { target: { value: 'htop' } });
    await waitFor(() => expect((view.getByText('Review…') as HTMLButtonElement).disabled).toBe(false));
    await fireEvent.click(view.getByText('Review…'));
    await view.findByText(/name = "Mine"/);
    const e = vi.mocked(Service.PlanEdit).mock.calls[0][0];
    expect(e).toMatchObject({ kind: 'workspace', file: USER, index: -1, version: 'v1' });
    expect(e.workspace.name).toBe('Mine');
    expect(e.workspace.tabs!.map((t) => t.panes!.map((p) => p.command))).toEqual([[''], ['htop']]);
    expect(Service.ApplyEdit).not.toHaveBeenCalled();
    await fireEvent.click(view.getByText('Confirm & Save'));
    await waitFor(() => expect(Service.ApplyEdit).toHaveBeenCalled());
  });

  it('lists workspaces in the Snippets tab', async () => {
    const SnippetsView = (await import('./SnippetsView.svelte')).default;
    const view = render(SnippetsView, { view: 'workspaces' });
    render(Overlays);
    await view.findByText('Payments on call');
    view.getByText('3 tabs · group payments');
    await fireEvent.click(view.getByLabelText('Open Payments on call'));
    await view.findByText('Open “Payments on call”');
  });
});
