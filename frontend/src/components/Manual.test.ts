// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};
Element.prototype.scrollTo ??= function () {};
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' }, Windows: {}, closeThisWindow: vi.fn(), moveTabToNewWindow: vi.fn() }));
vi.mock('@wailsio/runtime', async (original) => ({ ...(await original<object>()), Browser: { OpenURL: vi.fn() } }));

const { overlay } = await import('../lib/overlay.svelte');
const { sidebar } = await import('../lib/sidebar.svelte');
const { helpTopic } = await import('../lib/help');
const { runCommand } = await import('../lib/commands');
const Overlays = (await import('./Overlays.svelte')).default;

beforeEach(() => {
  overlay.current = null;
  sidebar.open = false;
});
afterEach(cleanup);

const heading = () => document.querySelector('.page h1')?.textContent;

describe('Manual', () => {
  it('opens from the palette command, at the start when the side pane is closed', async () => {
    render(Overlays);
    runCommand('help.manual');
    await waitFor(() => expect(heading()).toBe('Welcome to wtf'));
  });

  it('opens at the page for the side-pane tab on screen', () => {
    sidebar.open = true;
    sidebar.tab = 'ssh';
    sidebar.sshSection = 'tunnels';
    expect(helpTopic()).toBe('ssh-tunnels');
    sidebar.tab = 'kube';
    sidebar.kubeView = 'contexts';
    expect(helpTopic()).toBe('kube-contexts');
    sidebar.kubeView = 'cluster';
    expect(helpTopic()).toBe('kubernetes');
    sidebar.tab = 'aws';
    expect(helpTopic()).toBe('aws');
  });

  it('searches, follows links between pages and goes back', async () => {
    const view = render(Overlays);
    overlay.show({ kind: 'manual', topic: 'getting-started' });
    await waitFor(() => expect(heading()).toBe('Welcome to wtf'));

    await fireEvent.input(view.getByPlaceholderText('Search the manual'), { target: { value: 'rotate access key' } });
    const topics = [...document.querySelectorAll('.topic')].map((b) => b.textContent);
    expect(topics).toContain('AWS profiles');
    expect(topics).not.toContain('Split panes');

    await fireEvent.click(document.querySelector('a[data-topic="tabs"]')!);
    expect(heading()).toBe('Tabs and tab groups');
    await fireEvent.click(view.getByLabelText('Back'));
    expect(heading()).toBe('Welcome to wtf');
  });

  it('lists every shortcut for this computer', async () => {
    render(Overlays);
    overlay.show({ kind: 'manual', topic: 'shortcuts' });
    await waitFor(() => expect(heading()).toBe('Keyboard shortcuts'));
    const rows = [...document.querySelectorAll('.page tbody tr')].map((r) => r.textContent);
    expect(rows.some((r) => r?.startsWith('Command Palette'))).toBe(true);
    expect(rows.some((r) => r?.startsWith('Open Manual'))).toBe(true);
  });

  it('closes with Escape (after clearing a search)', async () => {
    const view = render(Overlays);
    overlay.show({ kind: 'manual', topic: 'files' });
    const search = await view.findByPlaceholderText('Search the manual');
    await fireEvent.input(search, { target: { value: 'pin' } });
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect((search as HTMLInputElement).value).toBe('');
    expect(overlay.current?.kind).toBe('manual');
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(overlay.current).toBeNull();
  });
});
