// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/svelte';

const StartRecording = vi.fn(async () => {});
const StopRecording = vi.fn(async () => ({ active: false, path: '/tmp/a.cast', seconds: 3, bytes: 10 }));
const SetRedactSecrets = vi.fn(async () => {});
const SaveFile = vi.fn(async () => '/tmp/a.cast');

vi.mock('@wailsio/runtime', () => ({ Dialogs: { SaveFile }, Clipboard: { SetText: vi.fn(async () => {}) }, Events: { On: vi.fn() } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: { Reveal: vi.fn(async () => {}), SaveText: vi.fn(async () => {}) } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({ Service: { SetRedactSecrets } }));
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: { StartRecording, StopRecording, Recording: vi.fn() }, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;

const { settings } = await import('../lib/settings.svelte');
const { testConfig } = await import('../test/config');
const { tabs } = await import('../lib/tabs.svelte');
const { redaction } = await import('../lib/redaction.svelte');
const { recordings } = await import('../lib/recording.svelte');
const ShareView = (await import('./ShareView.svelte')).default;

beforeEach(() => {
  settings.config = testConfig();
  redaction.on = false;
  recordings.active.clear();
  if (!tabs.list.length) tabs.open({ cwd: '/w' });
  tabs.activePane!.sessionId = 's1';
});
afterEach(cleanup);

describe('Share & Record tab', () => {
  it('turns Hide Secrets on and off with its switch', async () => {
    const screen = render(ShareView);
    const sw = screen.getByRole('switch');
    expect(sw.getAttribute('aria-checked')).toBe('false');
    await fireEvent.click(sw);
    expect(redaction.on).toBe(true);
    expect(sw.getAttribute('aria-checked')).toBe('true');
    await fireEvent.click(sw);
    expect(redaction.on).toBe(false);
  });

  it('saves the startup setting', async () => {
    const screen = render(ShareView);
    await fireEvent.click(screen.getByLabelText('Turn on whenever wtf starts'));
    expect(SetRedactSecrets).toHaveBeenCalledWith(true);
  });

  it('starts and stops recording the terminal in front', async () => {
    const screen = render(ShareView);
    await fireEvent.click(screen.getByRole('button', { name: /Start Recording/ }));
    await vi.waitFor(() => expect(StartRecording).toHaveBeenCalledWith('s1', '/tmp/a.cast', expect.any(String)));
    const stop = await screen.findByRole('button', { name: /Stop Recording/ });
    expect(screen.getByText('/tmp/a.cast')).toBeTruthy();
    await fireEvent.click(stop);
    await vi.waitFor(() => expect(StopRecording).toHaveBeenCalledWith('s1'));
    // Listed for later.
    expect(await screen.findByText('a.cast')).toBeTruthy();
  });

  it('needs shell integration for a Markdown transcript', () => {
    tabs.activePane!.integrated = false;
    const screen = render(ShareView);
    expect((screen.getByRole('button', { name: /Save Commands as Markdown/ }) as HTMLButtonElement).disabled).toBe(true);
  });
});
