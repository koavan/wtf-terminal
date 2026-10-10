// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

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
const { history } = await import('../lib/history.svelte');
const { sidebar } = await import('../lib/sidebar.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { toast } = await import('../lib/toast.svelte');
const { registerTerminal } = await import('../lib/terminals');
const HistoryView = (await import('./HistoryView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

const handle = () => ({
  focus: vi.fn(), clear: vi.fn(), selectAll: vi.fn(), getSelection: () => '', paste: vi.fn(), type: vi.fn(), sendInput: vi.fn(),
  jumpPrompt: vi.fn(), lastOutput: () => null, blockOutput: () => null, pinLastOutput: () => false, plainText: () => '', transcript: () => null, revealCommand: vi.fn(() => true), serialize: () => '', snapshot: () => [],
});
let apiTerm: ReturnType<typeof handle>;
let webTerm: ReturnType<typeof handle>;
let apiPane: number;
let webPane: number;

const at = (h: number, m: number, s = 0) => new Date(2026, 9, 7, h, m, s).getTime();

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(at(16, 30));
  tabs.list = [];
  tabs.groups = [];
  sidebar.historyScope = 'pane';
  overlay.current = null;

  tabs.open({ cwd: '/api' });
  apiPane = tabs.activePane!.id;
  tabs.open({ cwd: '/web' });
  webPane = tabs.activePane!.id;
  for (const p of [apiPane, webPane]) history.clear(p);
  for (const t of tabs.list) {
    const p = tabs.get(t.id)!;
    const pane = p.root.kind === 'pane' ? p.root.pane : null;
    if (pane) {
      pane.integrated = true;
      pane.process = 'bash';
      pane.cwd = pane.initialCwd;
    }
  }
  apiTerm = handle();
  webTerm = handle();
  registerTerminal(apiPane, apiTerm);
  registerTerminal(webPane, webTerm);

  history.started(apiPane, 1, 'npm install', '/api', at(16, 0));
  history.finished(apiPane, 0, 12_000, at(16, 0, 12));
  history.started(webPane, 1, 'git status', '/web', at(16, 5));
  history.finished(webPane, 0, 80, at(16, 5));
  history.started(webPane, 2, 'npm test', '/web', at(16, 10));
  history.finished(webPane, 1, 3_400, at(16, 10, 3));
  history.started(webPane, 3, 'npm run dev', '/web', at(16, 20));
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const commands = (c: HTMLElement) => [...c.querySelectorAll('.cmd')].map((n) => n.textContent);

describe('HistoryView', () => {
  it("lists the focused pane's commands, newest first, with completion time and duration", async () => {
    const screen = render(HistoryView);
    expect(commands(screen.container)).toEqual(['npm run dev', 'npm test', 'git status']);
    const failed = screen.getByText('npm test').closest('.row')!;
    expect(failed.textContent).toContain('4:10:03 PM');
    expect(failed.textContent).toContain('3s');
    expect(failed.textContent).toContain('exit 1');
    expect(screen.getByText('npm run dev').closest('.row')!.textContent).toContain('running · since 4:20:00 PM');
  });

  it('follows the focused pane', async () => {
    const screen = render(HistoryView);
    tabs.activate(tabs.list[0].id);
    await waitFor(() => expect(commands(screen.container)).toEqual(['npm install']));
  });

  it('All panes merges every shell, labeled with its tab', async () => {
    sidebar.historyScope = 'all';
    const screen = render(HistoryView);
    expect(commands(screen.container)).toEqual(['npm run dev', 'npm test', 'git status', 'npm install']);
    expect(screen.getAllByText('api')).toHaveLength(1);
    expect(screen.getAllByText('web')).toHaveLength(3);
  });

  it('click inserts the command; double-click runs it again', async () => {
    const screen = render(HistoryView);
    const row = screen.getByText('git status').closest('.row')!;
    await fireEvent.click(row, { detail: 1 });
    expect(webTerm.paste).toHaveBeenCalledWith('git status');
    await fireEvent.dblClick(row);
    expect(webTerm.type).toHaveBeenCalledWith('\x15git status\r');
  });

  it('does not run again while the shell is busy', async () => {
    tabs.activePane!.running = true;
    const screen = render(HistoryView);
    await fireEvent.dblClick(screen.getByText('git status').closest('.row')!);
    expect(webTerm.type).not.toHaveBeenCalled();
    expect(toast.message).toMatch(/busy/);
  });

  it('Show in Terminal switches to the pane and scrolls to the command', async () => {
    sidebar.historyScope = 'all';
    render(Overlays);
    const screen = render(HistoryView);
    await fireEvent.contextMenu(screen.getByText('npm install').closest('.row')!);
    await fireEvent.click(await screen.findByText('Show in Terminal'));
    expect(tabs.activePane!.id).toBe(apiPane);
    expect(apiTerm.revealCommand).toHaveBeenCalledWith(1);
  });

  it('filters', async () => {
    const screen = render(HistoryView);
    await fireEvent.input(screen.getByPlaceholderText('Filter commands'), { target: { value: 'npm' } });
    expect(commands(screen.container)).toEqual(['npm run dev', 'npm test']);
  });

  it("clearing asks first, then empties the list and the shell's session history", async () => {
    render(Overlays);
    const screen = render(HistoryView);
    await fireEvent.click(screen.getByLabelText('Clear history'));
    expect(history.of(webPane)).toHaveLength(3); // nothing yet
    expect(screen.getByText(/Removes 3 commands/)).toBeTruthy();
    await fireEvent.click(screen.getByText('Clear History'));
    expect(commands(screen.container)).toEqual([]);
    expect(webTerm.type).toHaveBeenCalledWith('\x18\x1d');
    expect(history.of(apiPane)).toHaveLength(1); // only this pane
    expect(apiTerm.type).not.toHaveBeenCalled();
  });

  it("never sends keys to a busy pane, but still clears its list", async () => {
    tabs.activePane!.running = true;
    render(Overlays);
    const screen = render(HistoryView);
    await fireEvent.click(screen.getByLabelText('Clear history'));
    expect(screen.getByText(/only the list will be cleared/)).toBeTruthy();
    await fireEvent.click(screen.getByText('Clear History'));
    expect(history.of(webPane)).toHaveLength(0);
    expect(webTerm.type).not.toHaveBeenCalled();
    expect(toast.message).toMatch(/left as is/);
  });

  it('All panes clears every idle shell', async () => {
    sidebar.historyScope = 'all';
    render(Overlays);
    const screen = render(HistoryView);
    await fireEvent.click(screen.getByLabelText('Clear history'));
    await fireEvent.click(screen.getByText('Clear History'));
    expect(apiTerm.type).toHaveBeenCalledWith('\x18\x1d');
    expect(webTerm.type).toHaveBeenCalledWith('\x18\x1d');
    expect(history.of(apiPane)).toHaveLength(0);
  });

  it('explains when the shell has no integration', async () => {
    tabs.activePane!.integrated = false;
    history.clear(webPane);
    const screen = render(HistoryView);
    expect(screen.getByText('No history for this shell')).toBeTruthy();
  });
});
