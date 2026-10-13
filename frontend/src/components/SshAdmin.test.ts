// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
const events = vi.hoisted(() => ({ handlers: new Map<string, (e: { data: unknown }) => void>() }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
  Browser: { OpenURL: vi.fn() },
  Dialogs: { OpenFile: vi.fn(async () => []) },
  Events: { On: (name: string, fn: (e: { data: unknown }) => void) => (events.handlers.set(name, fn), () => {}) },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: { Reveal: vi.fn() } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/sshhosts', () => ({
  Service: {
    List: vi.fn(), Keys: vi.fn(async () => []), Save: vi.fn(), Delete: vi.fn(),
    KeyList: vi.fn(), PublicKey: vi.fn(), WritePublicKey: vi.fn(), ProtectKey: vi.fn(), DeleteKey: vi.fn(),
    KeyNameFree: vi.fn(async () => true), AgentRemove: vi.fn(), AgentRemoveAll: vi.fn(),
    HostKeys: vi.fn(), ScanHostKeys: vi.fn(), ForgetHostKeys: vi.fn(), TrustHostKeys: vi.fn(),
    Tunnels: vi.fn(), SaveTunnel: vi.fn(), StartTunnel: vi.fn(), StopTunnel: vi.fn(), DeleteTunnel: vi.fn(),
  },
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/sshhosts');
const { Clipboard } = await import('@wailsio/runtime');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { sidebar } = await import('../lib/sidebar.svelte');
const { commandFinished } = await import('../lib/terminals');
const admin = await import('../lib/sshAdmin.svelte');
const SshView = (await import('./SshView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

type Host = import('../lib/ssh.svelte').Host;
type Key = import('../lib/sshAdmin.svelte').Key;
type TunnelStatus = import('../lib/sshAdmin.svelte').TunnelStatus;

const host = (h: Partial<Host>): Host => ({
  alias: '', hostName: '', user: '', port: '', proxyJump: '', forwardAgent: '', strictHostKeyChecking: '', source: '/home/me/.ssh/config', known: false,
  line: 1, patterns: [h.alias ?? ''], options: [], editable: true, ...h,
});
const HOSTS = [host({ alias: 'web', hostName: 'web.example.com' }), host({ alias: '10.0.0.5', hostName: '10.0.0.5', port: '2222', known: true, source: '' })];

const key = (k: Partial<Key>): Key => ({
  path: '', name: '', type: 'ED25519', bits: 256, fingerprint: 'SHA256:aaa', comment: '', encrypted: false,
  inAgent: false, unprotected: '', hasPub: true, usedBy: [], modified: '', ...k,
});
const KEYS: Key[] = [
  key({ path: '/home/me/.ssh/id_ed25519', name: '~/.ssh/id_ed25519', comment: 'me@laptop', inAgent: true, usedBy: ['web'], fingerprint: 'SHA256:ed' }),
  key({ path: '/home/me/.ssh/old rsa', name: '~/.ssh/old rsa', type: 'RSA', bits: 2048, unprotected: '0644', hasPub: false, fingerprint: 'SHA256:rsa', modified: '2023-03-14T09:26:00Z' }),
  key({ name: 'yubikey', comment: 'yubikey', type: 'ED25519-SK', inAgent: true, fingerprint: 'SHA256:sk' }),
];

const tunnel = (t: Partial<TunnelStatus>): TunnelStatus => ({
  id: 't1', name: '', host: 'web', port: '', kind: 'local', spec: '5432:db.internal:5432', autoStart: false,
  state: 'stopped', error: '', since: 0, command: 'ssh -N -L 5432:db.internal:5432 web', ...t,
});

type MenuItem = { label?: string; heading?: string; action?: () => void } | string;
const menuItems = () => ((overlay.current as { entries: MenuItem[] } | null)?.entries ?? []);
const menuLabels = () => menuItems().map((e) => (typeof e === 'string' ? e : (e.label ?? e.heading)));
const pick = (label: string) => (menuItems().find((e) => typeof e !== 'string' && e.label === label) as { action: () => void }).action();
const commands = () => tabs.list.map((t) => focusedPane(t).initialCommand);

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/home/me' });
  overlay.current = null;
  sidebar.sshSection = 'hosts';
  admin.sshTunnels.loaded = false; // a module-wide store: load afresh per test
  vi.clearAllMocks();
  vi.mocked(Service.List).mockResolvedValue({ hosts: HOSTS, problems: [], versions: {}, mainConfig: '/home/me/.ssh/config' } as never);
  vi.mocked(Service.KeyList).mockResolvedValue({ keys: KEYS, agentError: '', agentSocket: '/tmp/agent', problems: [] } as never);
  vi.mocked(Service.Tunnels).mockResolvedValue([]);
});
afterEach(cleanup);

describe('commands', () => {
  it('quotes paths but lets the shell expand ~', () => {
    expect(admin.quotePath('/home/me/.ssh/my key')).toBe(`~/'.ssh/my key'`);
    expect(admin.quotePath('/home/me')).toBe('~');
    expect(admin.quotePath('/opt/x y')).toBe(`'/opt/x y'`);
  });

  it('builds key commands', () => {
    expect(admin.keygenCommand({ type: 'ed25519', name: 'work', comment: 'me at work', addToAgent: false })).toBe(
      `ssh-keygen -t ed25519 -C 'me at work' -f ~/.ssh/work`,
    );
    expect(admin.keygenCommand({ type: 'rsa', name: 'x', comment: '', addToAgent: true })).toMatch(
      /^ssh-keygen -t rsa -b 4096 -f ~\/\.ssh\/x && ssh-add (--apple-use-keychain )?~\/\.ssh\/x$/,
    );
    expect(admin.installKeyCommand(KEYS[0], HOSTS[0])).toBe('ssh-copy-id -i ~/.ssh/id_ed25519.pub web');
    expect(admin.installKeyCommand(KEYS[1], HOSTS[1])).toBe(`ssh-copy-id -i ~/'.ssh/old rsa.pub' -p 2222 10.0.0.5`);
    expect(admin.changePassphraseCommand(KEYS[0])).toBe('ssh-keygen -p -f ~/.ssh/id_ed25519');
    expect(admin.freeKeyName('id_ed25519', KEYS)).toBe('id_ed25519_2');
    expect(admin.freeKeyName('id_ecdsa', KEYS)).toBe('id_ecdsa');
  });

  it('builds file transfer commands, with scp’s -P for known-host ports', () => {
    expect(admin.uploadCommand(HOSTS[0], ['/home/me/a b.txt', '/srv/dir'], '')).toBe(`scp -r ~/'a b.txt' /srv/dir web:`);
    expect(admin.uploadCommand(HOSTS[1], ['/tmp/x'], '/var/tmp')).toBe('scp -r -P 2222 /tmp/x 10.0.0.5:/var/tmp');
    expect(admin.downloadCommand(HOSTS[0], '/var/log/app log', '/home/me/Downloads')).toBe(`scp -r 'web:/var/log/app log' ~/Downloads`);
    expect(admin.sftpCommand(HOSTS[1])).toBe('sftp -P 2222 10.0.0.5');
  });

  it('round-trips tunnel specs', () => {
    for (const spec of ['8080:localhost:80', '0.0.0.0:8080:10.0.0.5:80', '8080:[fe80::1]:80']) {
      const t = tunnel({ spec });
      expect(admin.tunnelSpec(admin.draftOfTunnel(t))).toBe(spec);
    }
    expect(admin.tunnelSpec(admin.draftOfTunnel(tunnel({ kind: 'dynamic', spec: '1080' })))).toBe('1080');
  });

  it('describes tunnels and finds their local port', () => {
    expect(admin.describeTunnel(tunnel({}))).toBe('localhost:5432 → db.internal:5432 via web');
    expect(admin.describeTunnel(tunnel({ kind: 'remote', spec: '9000:localhost:3000' }))).toBe('web:9000 → localhost:3000 here');
    expect(admin.describeTunnel(tunnel({ kind: 'dynamic', spec: '1080' }))).toBe('SOCKS proxy on localhost:1080 via web');
    expect(admin.localPort(tunnel({ spec: '127.0.0.1:15432:db:5432' }))).toBe('15432');
    expect(admin.localPort(tunnel({ kind: 'dynamic', spec: '1080' }))).toBe('1080');
    expect(admin.localPort(tunnel({ kind: 'remote', spec: '9000:localhost:3000' }))).toBeNull();
  });
});

describe('keys', () => {
  async function showKeys() {
    sidebar.sshSection = 'keys';
    const view = render(SshView);
    render(Overlays);
    await view.findByText('id_ed25519');
    return view;
  }

  it('lists key files, then agent-only keys, with facts as pills', async () => {
    const view = await showKeys();
    const rows = [...view.container.querySelectorAll('.row .name')].map((e) => e.textContent);
    expect(rows).toEqual(['id_ed25519', 'old rsa', 'yubikey']);
    view.getByText('Agent only');
    view.getByText('0644');
    view.getByText('1 host');
    view.getByText('ssh-agent: 2 keys loaded');
    // Type with its size where it varies, and when the file was written.
    const made = new Date('2023-03-14T09:26:00Z').toLocaleDateString(undefined, { month: 'short', year: 'numeric' });
    view.getByText(`RSA 2048 · from ${made}`);
    view.getByText('ED25519 · me@laptop');
  });

  it('runs passphrase-taking actions in a tab and quick ones in the backend', async () => {
    const view = await showKeys();
    await fireEvent.contextMenu(view.getByText('old rsa'));
    expect(menuLabels()).toContain('Make Private (chmod 600)');
    expect(menuLabels()).toContain('Create .pub File');
    expect(menuLabels()).not.toContain('Unload from Agent');
    pick('Load into Agent');
    expect(commands().at(-1)).toBe(`ssh-add ~/'.ssh/old rsa'`);

    await fireEvent.contextMenu(view.getByText('old rsa'));
    pick('Make Private (chmod 600)');
    await waitFor(() => expect(Service.ProtectKey).toHaveBeenCalledWith('/home/me/.ssh/old rsa'));

    await fireEvent.contextMenu(view.getByText('yubikey'));
    expect(menuLabels()).not.toContain('Delete Key…');
    pick('Unload from Agent');
    await waitFor(() => expect(Service.AgentRemove).toHaveBeenCalledWith('SHA256:sk'));
  });

  it('copies the public key on double-click', async () => {
    vi.mocked(Service.PublicKey).mockResolvedValue('ssh-ed25519 AAAA me@laptop');
    const view = await showKeys();
    await fireEvent.dblClick(view.getByText('id_ed25519'));
    await waitFor(() => expect(Clipboard.SetText).toHaveBeenCalledWith('ssh-ed25519 AAAA me@laptop'));
  });

  it('deletes after asking, naming the hosts that use the key', async () => {
    const view = await showKeys();
    await fireEvent.contextMenu(view.getByText('id_ed25519'));
    pick('Delete Key…');
    await view.findByText('Used by web');
    await fireEvent.click(view.getByText('Delete Key'));
    await waitFor(() => expect(Service.DeleteKey).toHaveBeenCalledWith('/home/me/.ssh/id_ed25519'));
  });

  it('opens the Generate tab from +', async () => {
    const view = await showKeys();
    await fireEvent.click(view.getByLabelText('New key'));
    expect(sidebar.tab).toBe('generate');
    expect(sidebar.generateSection).toBe('key');
  });

  it('generates a key in a tab', async () => {
    const view = await showKeys();
    overlay.show({ kind: 'keyGen' }); // "Generate in a terminal instead"
    const name = (await view.findByDisplayValue('id_ed25519_2')) as HTMLInputElement;
    await fireEvent.input(name, { target: { value: 'deploy' } });
    await fireEvent.click(view.getByLabelText(/Load it into ssh-agent/));
    await fireEvent.click(view.getByText('Generate in Terminal'));
    await waitFor(() => expect(commands().at(-1)).toBe('ssh-keygen -t ed25519 -f ~/.ssh/deploy'));
    expect(Service.KeyNameFree).toHaveBeenCalledWith('deploy');
  });

  it('refuses a key name that exists', async () => {
    vi.mocked(Service.KeyNameFree).mockResolvedValue(false);
    const view = await showKeys();
    overlay.show({ kind: 'keyGen' });
    await fireEvent.click(await view.findByText('Generate in Terminal'));
    await view.findByText(/already exists/);
    expect(tabs.list.length).toBe(1);
  });

  it('reloads when ssh-add finishes in a terminal', async () => {
    await showKeys();
    const before = vi.mocked(Service.KeyList).mock.calls.length;
    commandFinished('ls -la', 0);
    commandFinished('ssh-add ~/.ssh/id_ed25519', 0);
    await waitFor(() => expect(vi.mocked(Service.KeyList).mock.calls.length).toBe(before + 1));
  });
});

describe('host actions', () => {
  async function showHosts() {
    const view = render(SshView);
    render(Overlays);
    await view.findByText('web');
    return view;
  }

  it('installs a key on a host, writing the .pub first when missing', async () => {
    const view = await showHosts();
    await fireEvent.contextMenu(view.getByText('10.0.0.5'));
    pick('Install Public Key…');
    const select = (await view.findByText(/~\/.ssh\/old rsa — RSA/)).closest('select')!;
    await fireEvent.change(select, { target: { value: '/home/me/.ssh/old rsa' } });
    await fireEvent.click(view.getByText('Install in Terminal'));
    await waitFor(() => expect(commands().at(-1)).toBe(`ssh-copy-id -i ~/'.ssh/old rsa.pub' -p 2222 10.0.0.5`));
    expect(Service.WritePublicKey).toHaveBeenCalledWith('/home/me/.ssh/old rsa');
  });

  it('shows host keys, scans, and trusts what was shown', async () => {
    vi.mocked(Service.HostKeys).mockResolvedValue({
      target: 'web.example.com', hostName: 'web.example.com', port: '22', files: ['/home/me/.ssh/known_hosts'], via: '',
      stored: [{ type: 'ED25519', bits: 256, fingerprint: 'SHA256:old', file: '/home/me/.ssh/known_hosts', line: 4, marker: '' }],
    });
    vi.mocked(Service.ScanHostKeys).mockResolvedValue([
      { type: 'ED25519', bits: 256, fingerprint: 'SHA256:new', file: '', line: 0, marker: '' },
    ]);
    const view = await showHosts();
    await fireEvent.contextMenu(view.getByText('web'));
    pick('Host Keys…');
    await view.findByText('SHA256:old');
    view.getByText('~/.ssh/known_hosts:4');
    await fireEvent.click(view.getByText('Scan'));
    await view.findByText('not stored');
    view.getByText(/None of the server’s keys match the stored ones/);
    await fireEvent.click(view.getByText('Replace with Server’s Keys'));
    await waitFor(() => expect(Service.TrustHostKeys).toHaveBeenCalledWith('web', '', ['SHA256:new']));
  });

  it('can’t scan through a jump host, and says so', async () => {
    vi.mocked(Service.HostKeys).mockResolvedValue({ target: 'inner', hostName: 'inner', port: '22', files: [], via: 'bastion', stored: [] });
    const view = await showHosts();
    await fireEvent.contextMenu(view.getByText('web'));
    pick('Host Keys…');
    await view.findByText(/Reached through/);
    expect(view.queryByText('Scan')).toBeNull();
  });

  it('opens a file dropped from the tree as an upload', async () => {
    const view = await showHosts();
    const row = view.getByText('web').closest('.row')!;
    const dataTransfer = { types: ['application/x-wtf-path'], getData: () => '/home/me/build.tar.gz', dropEffect: '' };
    await fireEvent.dragOver(row, { dataTransfer });
    await fireEvent.drop(row, { dataTransfer });
    await view.findByText('~/build.tar.gz');
    await fireEvent.input(view.getByPlaceholderText('your home folder'), { target: { value: '/tmp' } });
    await fireEvent.click(view.getByText('Upload'));
    expect(commands().at(-1)).toBe('scp -r ~/build.tar.gz web:/tmp');
  });

  it('downloads into ~/Downloads by default', async () => {
    const view = await showHosts();
    await fireEvent.contextMenu(view.getByText('web'));
    pick('Download…');
    await fireEvent.input(await view.findByPlaceholderText('/var/log/app.log'), { target: { value: '/etc/hosts' } });
    await fireEvent.click(view.getByText('Download'));
    expect(commands().at(-1)).toBe('scp -r web:/etc/hosts ~/Downloads');
  });
});

describe('tunnels', () => {
  it('saves a kubectl port-forward with the shell’s cluster variables', async () => {
    vi.mocked(Service.SaveTunnel).mockResolvedValue([tunnel({ id: 'k1', kind: 'kubectl' })]);
    vi.mocked(Service.StartTunnel).mockResolvedValue([tunnel({ id: 'k1', kind: 'kubectl', state: 'connecting' })]);
    focusedPane(tabs.active!).env = { KUBECONFIG: '/home/me/.kube/qe.yaml', AWS_PROFILE: 'devops', PATH: '/opt/bin', HOME: '/home/me' };
    const view = render(Overlays);
    overlay.show({ kind: 'tunnel', tunnel: null, host: null, prefill: { kind: 'kubectl', context: 'qe', namespace: 'shop', resource: 'service/web', listenPort: '8080', destPort: '80' } });
    await view.findByText(/KUBECONFIG=\/home\/me\/.kube\/qe.yaml AWS_PROFILE=devops kubectl --context qe --namespace shop port-forward service\/web 8080:80/);
    await fireEvent.click(view.getByText('Save & Start'));
    await waitFor(() => expect(Service.StartTunnel).toHaveBeenCalledWith('k1'));
    expect(Service.SaveTunnel).toHaveBeenCalledWith(expect.objectContaining({
      kind: 'kubectl', context: 'qe', namespace: 'shop', resource: 'service/web', spec: '8080:80',
      env: { KUBECONFIG: '/home/me/.kube/qe.yaml', AWS_PROFILE: 'devops', PATH: '/opt/bin' },
    }));
  });

  it('runs a one-off kubectl forward in a terminal instead', async () => {
    const view = render(Overlays);
    overlay.show({ kind: 'tunnel', tunnel: null, host: null, prefill: { kind: 'kubectl', context: 'qe', namespace: 'shop', resource: 'pod/web-1', listenPort: '9000', destPort: '9000', env: {} } });
    await fireEvent.click(await view.findByText('Run in Terminal'));
    expect(commands().at(-1)).toBe('kubectl --context qe --namespace shop port-forward pod/web-1 9000');
    expect(Service.SaveTunnel).not.toHaveBeenCalled();
  });

  it('lists tunnels, toggles them and follows backend updates', async () => {
    vi.mocked(Service.Tunnels).mockResolvedValue([tunnel({ name: 'staging db' })]);
    vi.mocked(Service.StartTunnel).mockResolvedValue([tunnel({ name: 'staging db', state: 'connecting' })]);
    sidebar.sshSection = 'tunnels';
    const view = render(SshView);
    await view.findByText('staging db');
    view.getByText('off');
    await fireEvent.click(view.getByLabelText('Start staging db'));
    expect(Service.StartTunnel).toHaveBeenCalledWith('t1');
    await view.findByText('connecting…');

    events.handlers.get('ssh:tunnels')!({ data: [tunnel({ name: 'staging db', state: 'failed', error: 'Permission denied (publickey).' })] });
    await view.findByText('Permission denied (publickey).');
    expect(view.container.querySelector('.badge')).toBeNull();

    events.handlers.get('ssh:tunnels')!({ data: [tunnel({ name: 'staging db', state: 'up' })] });
    await waitFor(() => expect(view.container.querySelector('.badge')?.textContent).toBe('1'));
  });

  it('creates a tunnel from a host’s menu', async () => {
    vi.mocked(Service.SaveTunnel).mockResolvedValue([tunnel({ id: 'n1' })]);
    vi.mocked(Service.StartTunnel).mockResolvedValue([tunnel({ id: 'n1', state: 'connecting' })]);
    const view = render(SshView);
    render(Overlays);
    await fireEvent.contextMenu(await view.findByText('10.0.0.5'));
    pick('New Tunnel…');
    await view.findByText('New Port Forward');
    expect((view.getByPlaceholderText('bastion') as HTMLInputElement).value).toBe('10.0.0.5');
    await fireEvent.input(view.getByPlaceholderText('8080'), { target: { value: '15432' } });
    await fireEvent.input(view.getByPlaceholderText('localhost'), { target: { value: 'db.internal' } });
    await fireEvent.input(view.getByPlaceholderText('80'), { target: { value: '5432' } });
    await fireEvent.click(view.getByText('Save & Start'));
    await waitFor(() => expect(Service.StartTunnel).toHaveBeenCalledWith('n1'));
    expect(Service.SaveTunnel).toHaveBeenCalledWith({
      id: '', name: '', host: '10.0.0.5', port: '2222', kind: 'local', spec: '15432:db.internal:5432', autoStart: false,
      context: '', namespace: '', resource: '', env: {},
    });
  });
});
