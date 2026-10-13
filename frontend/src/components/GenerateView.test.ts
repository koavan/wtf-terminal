// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}), Text: vi.fn(async () => '') },
  Browser: { OpenURL: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/sshhosts', () => ({
  Service: {
    KeyList: vi.fn(async () => ({ keys: [{ path: '/home/me/.ssh/id_ed25519', name: 'id_ed25519', usedBy: [] }], agentError: '', agentSocket: '', problems: [] })),
    GenerateKey: vi.fn(),
  },
}));

window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/sshhosts');
const { sidebar } = await import('../lib/sidebar.svelte');
const { redaction } = await import('../lib/redaction.svelte');
const GenerateView = (await import('./GenerateView.svelte')).default;

beforeEach(() => {
  vi.mocked(Service.GenerateKey).mockReset();
  redaction.on = false;
});
afterEach(cleanup);

describe('SSH key form', () => {
  beforeEach(() => (sidebar.generateSection = 'key'));

  it('picks a free file name and sends the form to the backend', async () => {
    vi.mocked(Service.GenerateKey).mockResolvedValue({
      path: '/home/me/.ssh/id_ed25519_2', publicKey: 'ssh-ed25519 AAAA ada@laptop', fingerprint: 'SHA256:x', inAgent: true, agentError: '',
    } as never);
    const r = render(GenerateView);
    await waitFor(() => r.getByDisplayValue('id_ed25519_2')); // id_ed25519 is taken
    await fireEvent.input(r.getByPlaceholderText('you@this-computer'), { target: { value: 'ada@laptop' } });
    await fireEvent.input(r.getByLabelText('Passphrase'), { target: { value: 'hunter22' } });
    await fireEvent.input(r.getByLabelText('Passphrase again'), { target: { value: 'hunter22' } });
    await fireEvent.click(r.getByText('Generate Key'));
    await waitFor(() => expect(Service.GenerateKey).toHaveBeenCalledWith({
      type: 'ed25519', name: 'id_ed25519_2', comment: 'ada@laptop', passphrase: 'hunter22', addToAgent: true,
    }));
    await r.findByText('ssh-ed25519 AAAA ada@laptop');
    expect(r.getByText('Copy Public Key')).toBeTruthy();
  });

  it("won't generate while the passphrases differ or are too short", async () => {
    const r = render(GenerateView);
    await waitFor(() => r.getByDisplayValue('id_ed25519_2'));
    const button = r.getByText('Generate Key') as HTMLButtonElement;
    await fireEvent.input(r.getByLabelText('Passphrase'), { target: { value: 'hunter22' } });
    await fireEvent.input(r.getByLabelText('Passphrase again'), { target: { value: 'hunter23' } });
    expect(button.disabled).toBe(true);
    r.getByText('The two passphrases differ.');
    await fireEvent.input(r.getByLabelText('Passphrase'), { target: { value: 'abc' } });
    await fireEvent.input(r.getByLabelText('Passphrase again'), { target: { value: 'abc' } });
    expect(button.disabled).toBe(true);
    await fireEvent.input(r.getByLabelText('Passphrase'), { target: { value: '' } });
    await fireEvent.input(r.getByLabelText('Passphrase again'), { target: { value: '' } });
    expect(button.disabled).toBe(false); // no passphrase is allowed
  });

  it('shows the error the backend gives', async () => {
    vi.mocked(Service.GenerateKey).mockRejectedValue(new Error('~/.ssh/id_ed25519_2 already exists: choose another name'));
    const r = render(GenerateView);
    await waitFor(() => r.getByDisplayValue('id_ed25519_2'));
    await fireEvent.click(r.getByText('Generate Key'));
    await r.findByText(/already exists/);
  });
});

describe('password generator', () => {
  beforeEach(() => (sidebar.generateSection = 'password'));

  it('shows a password of the chosen length, hidden while Hide Secrets is on', async () => {
    const r = render(GenerateView);
    const text = () => r.container.querySelector('.output .text')?.textContent ?? '';
    expect(text()).toHaveLength(24);
    await fireEvent.input(r.getByLabelText('Length'), { target: { value: '40' } });
    await waitFor(() => expect(text()).toHaveLength(40));

    redaction.on = true;
    await waitFor(() => expect(r.container.querySelector('.output .text')).toBeNull());
    await fireEvent.click(r.getByText('Show'));
    await waitFor(() => expect(text()).toHaveLength(40));
  });

  it('asks for a kind of character when none is chosen', async () => {
    const r = render(GenerateView);
    for (const label of ['Lowercase', 'Uppercase', 'Digits', 'Symbols']) await fireEvent.click(r.getByLabelText(new RegExp(label)));
    await r.findByText('Choose at least one kind of character');
  });
});
