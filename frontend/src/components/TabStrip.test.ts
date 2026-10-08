// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({
  thisWindow: { name: 'main' },
  otherWindows: vi.fn(async () => []),
  moveTabToNewWindow: vi.fn(),
  moveTabToWindow: vi.fn(),
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { renaming } = await import('../lib/rename.svelte');
const { runCommand } = await import('../lib/commands');
const TabStrip = (await import('./TabStrip.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  overlay.current = null;
  renaming.tabId = null;
  tabs.open({ cwd: '/w/api' });
  tabs.open({ cwd: '/w/web' });
});
afterEach(cleanup);

const titles = (c: HTMLElement) => [...c.querySelectorAll('.tab .title')].map((t) => t.textContent);
const input = (c: HTMLElement) => c.querySelector<HTMLInputElement>('input.rename');
/** Waits for the rename field to appear (waitFor retries only while the callback throws). */
const findInput = (c: HTMLElement) =>
  waitFor(() => {
    const field = input(c);
    if (!field) throw new Error('rename field not shown');
    return field;
  });

describe('renaming tabs', () => {
  it('double-click edits the name; Enter saves it', async () => {
    const screen = render(TabStrip);
    await fireEvent.dblClick(screen.getByText('api'));
    const field = await findInput(screen.container);
    expect(field.value).toBe('api');
    expect(document.activeElement).toBe(field);
    await fireEvent.input(field, { target: { value: 'backend' } });
    await fireEvent.keyDown(field, { key: 'Enter' });
    expect(input(screen.container)).toBeNull();
    expect(titles(screen.container)).toEqual(['backend', 'web']);
  });

  it('Escape cancels', async () => {
    const screen = render(TabStrip);
    await fireEvent.dblClick(screen.getByText('web'));
    const field = await findInput(screen.container);
    await fireEvent.input(field, { target: { value: 'nope' } });
    await fireEvent.keyDown(field, { key: 'Escape' });
    expect(titles(screen.container)).toEqual(['api', 'web']);
    expect(tabs.list[1].customTitle).toBeNull();
  });

  it('clicking away saves; an empty name goes back to automatic', async () => {
    tabs.rename(tabs.list[0].id, 'backend');
    const screen = render(TabStrip);
    await fireEvent.dblClick(screen.getByText('backend'));
    const field = await findInput(screen.container);
    await fireEvent.input(field, { target: { value: '' } });
    await fireEvent.blur(field);
    expect(tabs.list[0].customTitle).toBeNull();
    expect(titles(screen.container)).toEqual(['api', 'web']);
  });

  it("saving without changes doesn't freeze the automatic name", async () => {
    const screen = render(TabStrip);
    await fireEvent.dblClick(screen.getByText('api'));
    await fireEvent.keyDown(await findInput(screen.container), { key: 'Enter' });
    expect(tabs.list[0].customTitle).toBeNull();
  });

  it('typing is not overwritten when the tab changes underneath', async () => {
    const screen = render(TabStrip);
    await fireEvent.dblClick(screen.getByText('api'));
    const field = await findInput(screen.container);
    await fireEvent.input(field, { target: { value: 'my na' } });
    focusedPane(tabs.list[0]).cwd = '/w/elsewhere'; // the shell cds meanwhile
    await new Promise((r) => setTimeout(r, 0));
    expect(field.value).toBe('my na');
  });

  it('the tab menu offers Rename and Use Automatic Name', async () => {
    tabs.rename(tabs.list[1].id, 'frontend');
    render(Overlays);
    const screen = render(TabStrip);
    await fireEvent.contextMenu(screen.getByText('frontend').closest('.tab')!);
    await fireEvent.click(await screen.findByText('Use Automatic Name'));
    expect(titles(screen.container)).toEqual(['api', 'web']);

    await fireEvent.contextMenu(screen.getByText('web').closest('.tab')!);
    await fireEvent.click(await screen.findByText('Rename Tab…'));
    expect(await findInput(screen.container)).toBeTruthy();
  });

  it('the palette command renames the active tab', async () => {
    const screen = render(TabStrip);
    runCommand('tab.rename');
    const field = await findInput(screen.container);
    expect(field.value).toBe('web');
  });

  it('⌘A selects the text being typed, not the terminal', async () => {
    const screen = render(TabStrip);
    await fireEvent.dblClick(screen.getByText('api'));
    const field = await findInput(screen.container);
    field.setSelectionRange(1, 1);
    runCommand('term.selectAll');
    expect([field.selectionStart, field.selectionEnd]).toEqual([0, 3]);
  });
});
