// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/awsprofiles', () => ({
  Service: {
    List: vi.fn(), PlanSave: vi.fn(), Save: vi.fn(), PlanDelete: vi.fn(), Delete: vi.fn(),
    PlanMakeDefault: vi.fn(), MakeDefault: vi.fn(), Verify: vi.fn(), AccessKeys: vi.fn(),
    PlanRotate: vi.fn(), Rotate: vi.fn(),
  },
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/awsprofiles');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { registerTerminal } = await import('../lib/terminals');
const awsLib = await import('../lib/aws.svelte');
const AwsView = (await import('./AwsView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

type Profile = import('../lib/aws.svelte').Profile;
const prof = (p: Partial<Profile>): Profile => ({
  name: '', kind: 'keys', inCredentials: true, inConfig: false, accessKeyId: '', hasSecret: true, hasSessionToken: false,
  credentialsOther: [], region: '', output: '', settings: [], locked: [], account: '', sourceFor: [], ...p,
});
const PROFILES = [
  prof({ name: 'default', accessKeyId: 'AKIADEFAULT000000001', region: 'us-east-1', settings: [{ key: 'region', value: 'us-east-1' }], inConfig: true, sourceFor: ['admin'] }),
  prof({ name: 'admin', kind: 'role', inCredentials: false, inConfig: true, hasSecret: false, account: '123456789012',
    settings: [{ key: 'role_arn', value: 'arn:aws:iam::123456789012:role/Admin' }, { key: 'source_profile', value: 'default' }, { key: 'duration_seconds', value: '3600' }] }),
  prof({ name: 'ci', accessKeyId: 'AKIACI00000000000001' }),
];
const VERSIONS = { '/home/me/.aws/credentials': 'c1', '/home/me/.aws/config': 'k1' };
const LIST = { profiles: PROFILES, credentialsFile: '/home/me/.aws/credentials', configFile: '/home/me/.aws/config', versions: VERSIONS, problems: [] };

type MenuItem = { label?: string; action?: () => void } | string;
const menuItems = () => ((overlay.current as { entries: MenuItem[] } | null)?.entries ?? []);
const menuLabels = () => menuItems().map((e) => (typeof e === 'string' ? e : e.label));
const pick = (label: string) => (menuItems().find((e) => typeof e !== 'string' && e.label === label) as { action: () => void }).action();
const commands = () => tabs.list.map((t) => focusedPane(t).initialCommand);

let sent: string[] = [];
beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/home/me' });
  overlay.current = null;
  vi.clearAllMocks();
  vi.mocked(Service.List).mockResolvedValue(LIST as never);
  sent = [];
  const pane = focusedPane(tabs.active!);
  registerTerminal(pane.id, {
    focus() {}, clear() {}, selectAll() {}, getSelection: () => '', paste() {}, type() {},
    sendInput: (b) => sent.push(new TextDecoder().decode(b)),
    jumpPrompt() {}, revealCommand: () => true, lastOutput: () => null, blockOutput: () => null, pinLastOutput: () => false, serialize: () => '', snapshot: () => [],
  });
});
afterEach(cleanup);

async function show() {
  const view = render(AwsView);
  render(Overlays);
  await view.findByText('ci');
  return view;
}

describe('drafts', () => {
  it('splits a profile into fields and builds it back', () => {
    const d = awsLib.draftOf(PROFILES[1]);
    expect(d.kind).toBe('role');
    expect(d.fields).toEqual({ role_arn: 'arn:aws:iam::123456789012:role/Admin', source_profile: 'default', mfa_serial: '', external_id: '' });
    expect(d.more).toBe('duration_seconds = 3600');
    expect(awsLib.toDraft(d, 'admin', VERSIONS)).toEqual({
      original: 'admin', name: 'admin', versions: VERSIONS, useKeys: false, accessKeyId: '', secretAccessKey: '', sessionToken: '',
      removeSessionToken: false,
      settings: [
        { key: 'role_arn', value: 'arn:aws:iam::123456789012:role/Admin' },
        { key: 'source_profile', value: 'default' },
        { key: 'duration_seconds', value: '3600' },
      ],
    });
  });

  it('keeps the secret empty (unchanged) and refuses a malformed line', () => {
    const d = awsLib.draftOf(PROFILES[0]);
    expect(awsLib.toDraft(d, 'default', VERSIONS)).toMatchObject({ useKeys: true, accessKeyId: 'AKIADEFAULT000000001', secretAccessKey: '' });
    expect(() => awsLib.toDraft({ ...d, more: 'oops' }, 'default', VERSIONS)).toThrow('key = value');
  });

  it('quotes the profile for the shell and counts days', () => {
    expect(awsLib.useCommand('prod')).toBe('export AWS_PROFILE=prod');
    expect(awsLib.useCommand("o'k x")).toBe(`export AWS_PROFILE='o'\\''k x'`);
    expect(awsLib.daysSince('2026-01-01T00:00:00Z', Date.parse('2026-01-31T12:00:00Z'))).toBe(30);
    expect(awsLib.shortKey('AKIAABCDEFGH12345678')).toBe('AKIA…5678');
  });
});

describe('AwsView', () => {
  it('lists profiles with facts and marks the one the shell uses', async () => {
    focusedPane(tabs.active!).infra = { awsProfile: 'ci' } as never;
    const view = await show();
    expect([...view.container.querySelectorAll('.name')].map((e) => e.textContent?.trim().split(/\s/)[0])).toEqual(['default', 'admin', 'ci']);
    view.getByText('AKIA…0001 · us-east-1');
    view.getByText('Assume role · 123456789012');
    expect(view.getByText('in use').closest('.row')?.textContent).toContain('ci');
  });

  it('switches the focused shell at its prompt, or opens a tab when it’s busy', async () => {
    const view = await show();
    await fireEvent.dblClick(view.getByText('admin'));
    expect(sent).toEqual(['export AWS_PROFILE=admin\r']);

    focusedPane(tabs.active!).running = true;
    await fireEvent.dblClick(view.getByText('ci'));
    expect(sent.length).toBe(1);
    expect(commands().at(-1)).toBe('export AWS_PROFILE=ci');
  });

  it('deletes only after showing the plan and getting a yes', async () => {
    vi.mocked(Service.PlanDelete).mockResolvedValue(['~/.aws/credentials: remove [ci] (access key AKIACI00000000000001)']);
    vi.mocked(Service.Delete).mockResolvedValue({ ...LIST, profiles: PROFILES.slice(0, 2) } as never);
    const view = await show();
    await fireEvent.contextMenu(view.getByText('ci'));
    pick('Delete…');
    await view.findByText('~/.aws/credentials: remove [ci] (access key AKIACI00000000000001)');
    expect(Service.PlanDelete).toHaveBeenCalledWith('ci', VERSIONS);
    expect(Service.Delete).not.toHaveBeenCalled();
    await fireEvent.click(view.getByText('Delete Profile'));
    expect(Service.Delete).toHaveBeenCalledWith('ci', VERSIONS);
    await waitFor(() => expect(view.queryByText('ci')).toBeNull());
  });

  it('offers rotation only for long-term keys', async () => {
    const view = await show();
    await fireEvent.contextMenu(view.getByText('admin'));
    expect(menuLabels()).not.toContain('Rotate Access Key…');
    expect(menuLabels()).not.toContain('Check Access Keys (iam)');
    await fireEvent.contextMenu(view.getByText('ci'));
    expect(menuLabels()).toContain('Rotate Access Key…');
  });

  it('shows what AWS says about a profile', async () => {
    vi.mocked(Service.Verify).mockResolvedValue({ account: '123456789012', arn: 'arn:aws:iam::123456789012:user/ci-bot', userId: 'AIDA' });
    vi.mocked(Service.AccessKeys).mockResolvedValue([
      { id: 'AKIACI00000000000001', status: 'Active', created: new Date(Date.now() - 40 * 86_400_000).toISOString(), lastUsed: '', lastUsedService: '', current: true },
    ]);
    const view = await show();
    await fireEvent.contextMenu(view.getByText('ci'));
    pick('Check Identity (sts)');
    await view.findByText('123456789012:user/ci-bot');
    await fireEvent.contextMenu(view.getByText('ci'));
    pick('Check Access Keys (iam)');
    await view.findByText(/AKIA…0001 \(this one\) · active · 40 days old · never used/);
  });
});

describe('editor', () => {
  it('adds a profile: review lists the changes, and only Confirm saves', async () => {
    vi.mocked(Service.PlanSave).mockResolvedValue(['~/.aws/credentials: add [prod]', '~/.aws/credentials: [prod] new secret access key (not shown)']);
    vi.mocked(Service.Save).mockResolvedValue(LIST as never);
    const view = await show();
    await fireEvent.click(view.getByLabelText('Add profile'));
    const dialog = document.querySelector('.dialog')!;
    const input = (label: string) => [...dialog.querySelectorAll('label')].find((l) => l.textContent?.trim().startsWith(label))!.querySelector('input')!;
    await fireEvent.input(input('Name'), { target: { value: 'prod' } });
    await fireEvent.input(input('Access key ID'), { target: { value: 'AKIAPROD000000000001' } });
    await fireEvent.input(input('Secret access key'), { target: { value: 'sssssssssssssssssssss' } });
    await fireEvent.input(input('Region'), { target: { value: 'eu-west-1' } });
    await fireEvent.click(view.getByText('Review…'));

    await view.findByText('~/.aws/credentials: add [prod]');
    expect(Service.Save).not.toHaveBeenCalled();
    expect(Service.PlanSave).toHaveBeenCalledWith(expect.objectContaining({
      original: '', name: 'prod', useKeys: true, accessKeyId: 'AKIAPROD000000000001', secretAccessKey: 'sssssssssssssssssssss',
      settings: [{ key: 'region', value: 'eu-west-1' }], versions: VERSIONS,
    }));
    await fireEvent.click(view.getByText('Confirm & Save'));
    await waitFor(() => expect(Service.Save).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(overlay.current).toBeNull());
  });

  it('shows the backend’s refusal and stays open', async () => {
    vi.mocked(Service.PlanSave).mockRejectedValue(new Error('enter the secret access key that goes with this key ID'));
    const view = await show();
    await fireEvent.contextMenu(view.getByText('ci'));
    pick('Edit…');
    await fireEvent.click(await view.findByText('Review…'));
    await view.findByText(/enter the secret access key/);
    expect(overlay.current?.kind).toBe('awsProfile');
  });
});

describe('rotation', () => {
  it('shows the checked plan and rotates on confirmation', async () => {
    vi.mocked(Service.PlanRotate).mockResolvedValue(['Create a new access key for IAM user ci-bot (account 123456789012)', 'Delete the old key AKIACI00000000000001']);
    vi.mocked(Service.Rotate).mockResolvedValue({ newKeyId: 'AKIANEW0000000000001', steps: ['Created AKIANEW0000000000001', 'Deleted AKIACI00000000000001'], list: LIST } as never);
    const view = await show();
    await fireEvent.contextMenu(view.getByText('ci'));
    pick('Rotate Access Key…');
    await view.findByText(/IAM user ci-bot/);
    expect(Service.Rotate).not.toHaveBeenCalled();
    await fireEvent.click(view.getByText('Rotate Key'));
    expect(Service.Rotate).toHaveBeenCalledWith('ci', true, VERSIONS);
    await view.findByText('Created AKIANEW0000000000001');
  });

  it('says why a profile can’t be rotated', async () => {
    vi.mocked(Service.PlanRotate).mockRejectedValue(new Error('the user already has two access keys, AWS’s limit'));
    const view = await show();
    await fireEvent.contextMenu(view.getByText('ci'));
    pick('Rotate Access Key…');
    await view.findByText(/two access keys/);
    expect(view.queryByText('Rotate Key')).toBeNull();
  });
});
