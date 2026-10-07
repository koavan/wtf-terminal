// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

type Proc = { pid: number; ppid: number; name: string; command: string; cpu: number; memory: number; elapsed: number; session: string; shell: boolean };
let procs: Proc[] = [];
const kill = vi.fn(async (_pid: number, _force: boolean) => {});
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/procs', () => ({
  Service: { List: vi.fn(async () => procs), Kill: (pid: number, force: boolean) => kill(pid, force) },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/netconn', () => ({ Service: { List: vi.fn(async () => []), Kill: vi.fn() } }));
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { tabs } = await import('../lib/tabs.svelte');
const { settings } = await import('../lib/settings.svelte');
const { testConfig } = await import('../test/config');
const { overlay } = await import('../lib/overlay.svelte');
const { network } = await import('../lib/network.svelte');
const { sidebar } = await import('../lib/sidebar.svelte');
const { processes, formatBytes, formatElapsed, sortProcs, processTree } = await import('../lib/processes.svelte');
const ProcessView = (await import('./ProcessView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

const proc = (over: Partial<Proc>): Proc => ({
  pid: 1, ppid: 1, name: 'x', command: 'x', cpu: 0, memory: 1024 * 1024, elapsed: 10, session: '', shell: false, ...over,
});

beforeEach(() => {
  settings.config = testConfig();
  tabs.list = [];
  tabs.groups = [];
  tabs.open({ cwd: '/web' });
  tabs.activePane!.cwd = '/web';
  tabs.setSession(tabs.activePane!.id, 's2');
  tabs.open({ cwd: '/api' });
  tabs.activePane!.cwd = '/api';
  tabs.setSession(tabs.activePane!.id, 's1');
  processes.scope = 'terminals';
  processes.sort = 'cpu';
  overlay.current = null;
  network.filter = '';
  procs = [
    proc({ pid: 100, name: 'bash', command: '-bash', session: 's1', shell: true }),
    proc({ pid: 101, ppid: 100, name: 'npm', command: 'npm run dev', session: 's1' }),
    proc({ pid: 102, ppid: 101, name: 'node', command: 'node server.js', session: 's1', cpu: 62, memory: 300 * 1024 ** 2 }),
    proc({ pid: 200, name: 'zsh', command: '-zsh', session: 's2', shell: true }),
    proc({ pid: 500, name: 'Finder', command: '/System/Finder', cpu: 3, memory: 2 * 1024 ** 3 }),
  ];
  kill.mockClear();
});
afterEach(cleanup);

const names = (c: HTMLElement) => [...c.querySelectorAll('.row .name')].map((n) => n.textContent);

describe('helpers', () => {
  it('formats sizes and durations', () => {
    expect(formatBytes(1536)).toBe('2 KB');
    expect(formatBytes(12 * 1024 ** 2)).toBe('12 MB');
    expect(formatBytes(1.5 * 1024 ** 3)).toBe('1.5 GB');
    expect(formatElapsed(45)).toBe('45s');
    expect(formatElapsed(725)).toBe('12m');
    expect(formatElapsed(11100)).toBe('3h 5m');
    expect(formatElapsed(187200)).toBe('2d 4h');
  });

  it('sorts by cpu, memory and name', () => {
    expect(sortProcs(procs, 'cpu')[0].name).toBe('node');
    expect(sortProcs(procs, 'memory')[0].name).toBe('Finder');
    expect(sortProcs(procs, 'name').map((p) => p.name)).toEqual(['bash', 'Finder', 'node', 'npm', 'zsh']);
  });

  it('builds a tree under the shell', () => {
    const tree = processTree(procs.filter((p) => p.session === 's1'));
    expect(tree.map((r) => `${r.depth}:${r.proc.name}`)).toEqual(['0:bash', '1:npm', '2:node']);
  });
});

describe('ProcessView', () => {
  it('groups terminal processes by tab, in tab order, as trees', async () => {
    const screen = render(ProcessView);
    await waitFor(() => expect(names(screen.container)).toEqual(['zsh', 'bash', 'npm', 'node']));
    const groups = [...screen.container.querySelectorAll('.group span')].map((s) => s.textContent);
    expect(groups).toEqual(['web', 'api']); // the /web tab comes first in the strip
    expect(screen.queryByText('Finder')).toBeNull();
    expect(screen.getByText('62%').classList.contains('hot')).toBe(true);
  });

  it('All lists everything, sortable', async () => {
    processes.scope = 'all';
    const screen = render(ProcessView);
    await waitFor(() => expect(names(screen.container)[0]).toBe('node'));
    await fireEvent.click(screen.getByText('Memory'));
    expect(names(screen.container)[0]).toBe('Finder');
    expect(screen.getByText('2.0 GB')).toBeTruthy();
  });

  it('filters by command line', async () => {
    const screen = render(ProcessView);
    await screen.findByText('node');
    await fireEvent.input(screen.getByPlaceholderText(/Filter/), { target: { value: 'server.js' } });
    await waitFor(() => expect(names(screen.container)).toEqual(['node']));
  });

  it('ending a process asks first; force quit sends SIGKILL', async () => {
    render(Overlays);
    const screen = render(ProcessView);
    const row = (await screen.findByText('node')).closest('.row')!;

    await fireEvent.contextMenu(row);
    await fireEvent.click(await screen.findByText('End node…'));
    expect(kill).not.toHaveBeenCalled();
    await fireEvent.click(await screen.findByText('End Process'));
    expect(kill).toHaveBeenLastCalledWith(102, false);

    await fireEvent.contextMenu(row);
    await fireEvent.click(await screen.findByText('Force Quit node…'));
    expect(await screen.findByText(/stop immediately/)).toBeTruthy();
    await fireEvent.click(screen.getByText('Force Quit'));
    expect(kill).toHaveBeenLastCalledWith(102, true);
  });

  it('a shell row offers Close Tab instead of ending the shell', async () => {
    render(Overlays);
    const screen = render(ProcessView);
    await fireEvent.contextMenu((await screen.findByText('bash')).closest('.row')!);
    expect(await screen.findByText('Close Tab…')).toBeTruthy();
    expect(screen.queryByText('End bash…')).toBeNull();
  });

  it('Show Network Connections opens the Network tab filtered to that PID', async () => {
    render(Overlays);
    const screen = render(ProcessView);
    await fireEvent.contextMenu((await screen.findByText('node')).closest('.row')!);
    await fireEvent.click(await screen.findByText('Show Network Connections'));
    expect(network.filter).toBe('pid:102');
    expect(sidebar.tab).toBe('network');
  });
});
