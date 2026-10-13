import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@wailsio/runtime', () => ({ Clipboard: { SetText: vi.fn(async () => {}), Text: vi.fn() } }));
vi.mock('./settings.svelte', () => ({ settings: { config: { clipboardClear: 30 } } }));
vi.mock('./toast.svelte', () => ({ toast: { show: vi.fn() } }));

const { Clipboard } = await import('@wailsio/runtime');
const { settings } = await import('./settings.svelte');
const { toast } = await import('./toast.svelte');
const { copySecret } = await import('./clipboard');

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  settings.config.clipboardClear = 30;
});
afterEach(() => vi.useRealTimers());

describe('copySecret', () => {
  it('empties the clipboard after the configured time, only if it still holds the secret', async () => {
    vi.mocked(Clipboard.Text).mockResolvedValueOnce('s3cret').mockResolvedValueOnce('other');
    await copySecret('s3cret', 'password');
    expect(toast.show).toHaveBeenCalledWith('Copied password. The clipboard clears in 30 seconds');
    await vi.advanceTimersByTimeAsync(29_999);
    expect(Clipboard.SetText).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(Clipboard.SetText).toHaveBeenLastCalledWith('');

    vi.mocked(Clipboard.SetText).mockClear();
    await copySecret('s3cret', 'password');
    await vi.advanceTimersByTimeAsync(30_000);
    expect(Clipboard.SetText).toHaveBeenCalledTimes(1); // something else was copied since
  });

  it('follows the setting, and keeps the secret when it is 0', async () => {
    settings.config.clipboardClear = 5;
    vi.mocked(Clipboard.Text).mockResolvedValue('tok');
    await copySecret('tok', 'token');
    await vi.advanceTimersByTimeAsync(5_000);
    expect(Clipboard.SetText).toHaveBeenLastCalledWith('');

    settings.config.clipboardClear = 0;
    vi.mocked(Clipboard.SetText).mockClear();
    await copySecret('tok', 'token');
    expect(toast.show).toHaveBeenLastCalledWith('Copied token');
    await vi.advanceTimersByTimeAsync(600_000);
    expect(Clipboard.SetText).toHaveBeenCalledTimes(1);
  });
});
