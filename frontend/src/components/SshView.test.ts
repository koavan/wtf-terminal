// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/sshhosts', () => ({
  Service: { List: vi.fn(), Keys: vi.fn(async () => ['~/.ssh/id_ed25519']), Save: vi.fn(), Delete: vi.fn() },
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/sshhosts');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const ssh = await import('../lib/ssh.svelte');
const SshView = (await import('./SshView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

type Host = import('../lib/ssh.svelte').Host;
const host = (h: Partial<Host>): Host => ({
  alias: '', hostName: '', user: '', port: '', proxyJump: '', source: '/home/me/.ssh/config', known: false,
  line: 1, patterns: [h.alias ?? ''], options: [], editable: true, ...h,
});

const HOSTS: Host[] = [
  host({ alias: 'bastion', hostName: 'bastion.example.com', user: 'ops' }),
  host({ alias: 'web1', hostName: 'web1.internal', proxyJump: 'bastion', source: '/home/me/.ssh/config.d/prod' }),
  host({ alias: 'web2', hostName: 'web2.internal', proxyJump: 'bastion', source: '/home/me/.ssh/config.d/prod' }),
  host({ alias: '10.0.0.5', hostName: '10.0.0.5', port: '2222', source: '', known: true }),
];

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/home/me' });
  overlay.current = null;
  vi.mocked(Service.List).mockResolvedValue(LIST as never);
  vi.mocked(Service.Save).mockReset();
  vi.mocked(Service.Delete).mockReset();
});

const LIST = { hosts: HOSTS, problems: [], versions: { '/home/me/.ssh/config': 'v1', '/home/me/.ssh/config.d/prod': 'v2' }, mainConfig: '/home/me/.ssh/config' };

type MenuItem = { label?: string; heading?: string; action?: () => void } | string;
const menuItems = () => ((overlay.current as { entries: MenuItem[] } | null)?.entries ?? []);
const menuLabels = () => menuItems().map((e) => (typeof e === 'string' ? e : (e.label ?? e.heading)));
const pick = (label: string) => (menuItems().find((e) => typeof e !== 'string' && e.label === label) as { action: () => void }).action();
afterEach(cleanup);

const commands = () => tabs.list.map((t) => focusedPane(t).initialCommand);

describe('ssh helpers', () => {
  it('builds the ssh command, keeping config aliases so ssh applies their settings', () => {
    expect(ssh.sshCommand(HOSTS[0])).toBe('ssh bastion');
    expect(ssh.sshCommand(HOSTS[3])).toBe('ssh -p 2222 10.0.0.5');
    expect(ssh.sshCommand(host({ alias: "odd'name" }))).toBe(`ssh 'odd'\\''name'`);
    expect(ssh.sshCommand(host({ alias: 'x', port: '2200' }))).toBe('ssh x'); // the config has the port
  });

  it('describes what the alias does not already say', () => {
    expect(ssh.hostDetail(HOSTS[0])).toBe('ops@bastion.example.com');
    expect(ssh.hostDetail(HOSTS[1])).toBe('web1.internal via bastion');
    expect(ssh.hostDetail(HOSTS[3])).toBe('10.0.0.5:2222');
    expect(ssh.hostDetail(host({ alias: 'a', hostName: 'a' }))).toBe('');
  });

  it('filters on every word', () => {
    expect(HOSTS.filter((h) => ssh.matchesFilter(h, 'web bastion')).map((h) => h.alias)).toEqual(['web1', 'web2']);
    expect(HOSTS.filter((h) => ssh.matchesFilter(h, '  ')).length).toBe(4);
  });

  it('names a group after the hosts’ shared config file', () => {
    expect(ssh.groupName([HOSTS[1], HOSTS[2]])).toBe('prod');
    expect(ssh.groupName([HOSTS[0], HOSTS[1]])).toBe('SSH');
    expect(ssh.groupName([HOSTS[0]])).toBe('SSH'); // the main config isn't a name
  });
});

describe('SshView', () => {
  it('lists hosts by file, then known hosts', async () => {
    const { container } = render(SshView);
    await waitFor(() => expect(container.querySelectorAll('.row').length).toBe(4));
    const sections = [...container.querySelectorAll('.section')].map((e) => e.textContent);
    expect(sections).toEqual(['~/.ssh/config', '~/.ssh/config.d/prod', 'Known hosts']);
  });

  it('double-click opens the host in a new tab', async () => {
    const { findByText } = render(SshView);
    await fireEvent.dblClick(await findByText('bastion'));
    expect(tabs.list.length).toBe(2);
    expect(commands()[1]).toBe('ssh bastion');
  });

  it('opens a multi-selection as a named tab group', async () => {
    const { findByText, getByText } = render(SshView);
    await fireEvent.click(await findByText('web1'));
    await fireEvent.click(getByText('web2'), { metaKey: true, ctrlKey: true });
    getByText('2 selected');
    await fireEvent.click(getByText('Open as Tab Group'));

    expect(commands().slice(1)).toEqual(['ssh web1', 'ssh web2']);
    expect(tabs.groups.length).toBe(1);
    expect(tabs.groups[0].name).toBe('prod');
    expect(tabs.list.slice(1).every((t) => t.groupId === tabs.groups[0].id)).toBe(true);
    expect(tabs.activeId).toBe(tabs.list[1].id);
  });

  it('shift-click selects a range; Enter opens it', async () => {
    const { findByText, getByText, container } = render(SshView);
    await fireEvent.click(await findByText('bastion'));
    await fireEvent.click(getByText('web2'), { shiftKey: true });
    expect(container.querySelectorAll('.row.on').length).toBe(3);
    await fireEvent.keyDown(container.querySelector('.list')!, { key: 'Enter' });
    expect(commands().slice(1)).toEqual(['ssh bastion', 'ssh web1', 'ssh web2']);
    expect(tabs.groups[0].name).toBe('SSH');
  });

  it('filters the list', async () => {
    const { findByText, getByPlaceholderText, container } = render(SshView);
    await findByText('bastion');
    await fireEvent.input(getByPlaceholderText('Filter hosts'), { target: { value: '10.0' } });
    expect([...container.querySelectorAll('.alias')].map((e) => e.textContent)).toEqual(['10.0.0.5']);
  });

  it('offers to open in a split from the menu', async () => {
    const { findByText } = render(SshView);
    await fireEvent.contextMenu(await findByText('web1'));
    const menu = overlay.current as { kind: string; entries: { label: string; action: () => void }[] } | null;
    expect(menu?.entries.map((e) => (typeof e === 'string' ? e : e.label))).toContain('Open in Split Right');
    menu!.entries.find((e) => e.label === 'Open in Split Right')!.action();
    expect(tabs.list.length).toBe(1);
    const root = tabs.list[0].root;
    expect(root.kind === 'split' && root.b.kind === 'pane' && root.b.pane.initialCommand).toBe('ssh web1');
  });

  it('explains an empty list', async () => {
    vi.mocked(Service.List).mockResolvedValue({ hosts: [], problems: [] } as never);
    const { findByText } = render(SshView);
    await findByText('No SSH hosts');
  });
});

describe('drafts', () => {
  it('splits an entry into fields, keeping the rest as lines', () => {
    const d = ssh.draftOf(
      host({
        alias: 'web1',
        patterns: ['web1', 'web2'],
        options: [
          { key: 'HostName', value: '%h.internal' },
          { key: 'IdentityFile', value: '"~/.ssh/my key"' },
          { key: 'IdentityFile', value: '~/.ssh/other' },
          { key: 'ForwardAgent', value: 'yes' },
        ],
      }),
    );
    expect(d).toEqual({
      name: 'web1 web2', hostName: '%h.internal', user: '', port: '', identityFile: '~/.ssh/my key', proxyJump: '',
      more: 'IdentityFile ~/.ssh/other\nForwardAgent yes',
    });
    expect(ssh.optionsOf(d)).toEqual([
      { key: 'HostName', value: '%h.internal' },
      { key: 'IdentityFile', value: '~/.ssh/my key' },
      { key: 'IdentityFile', value: '~/.ssh/other' },
      { key: 'ForwardAgent', value: 'yes' },
    ]);
  });

  it('accepts Key=value and refuses a setting without a value', () => {
    const d = ssh.draftOf(null, { name: 'a', more: 'LocalForward=8080 localhost:80\n\n' });
    expect(ssh.optionsOf(d)).toEqual([{ key: 'LocalForward', value: '8080 localhost:80' }]);
    expect(() => ssh.optionsOf({ ...d, more: 'ForwardAgent' })).toThrow('ForwardAgent needs a value');
  });
});

describe('host editor', () => {
  async function renderBoth() {
    const view = render(SshView);
    render(Overlays);
    await view.findByText('bastion');
    return view;
  }

  it('adds a host to ~/.ssh/config and can connect to it', async () => {
    const view = await renderBoth();
    const added = host({ alias: 'db', hostName: '10.0.0.7', line: 12 });
    vi.mocked(Service.Save).mockResolvedValue({ ...LIST, hosts: [...HOSTS, added] } as never);

    await fireEvent.click(view.getByLabelText('Add host'));
    const dialog = document.querySelector('.dialog')!;
    const input = (label: string) => [...dialog.querySelectorAll('label')].find((l) => l.textContent?.startsWith(label))!.querySelector('input, textarea')!;
    await fireEvent.input(input('Name'), { target: { value: 'db' } });
    await fireEvent.input(input('Address'), { target: { value: '10.0.0.7' } });
    await fireEvent.input(input('User'), { target: { value: 'admin' } });
    await fireEvent.click(view.getByText('Save & Connect'));

    await waitFor(() => expect(overlay.current).toBeNull());
    expect(Service.Save).toHaveBeenCalledWith({
      file: '', line: 0, version: 'v1', patterns: ['db'],
      options: [{ key: 'HostName', value: '10.0.0.7' }, { key: 'User', value: 'admin' }],
    });
    expect(commands().at(-1)).toBe('ssh db');
  });

  it('edits an entry in its own file and shows what went wrong', async () => {
    const view = await renderBoth();
    vi.mocked(Service.Save).mockRejectedValue(new Error('ssh doesn’t accept this: Bad configuration option: bogus'));
    await fireEvent.contextMenu(view.getByText('web1'));
    pick('Edit Host…');
    await waitFor(() => expect(document.querySelector('.dialog h2')?.textContent).toBe('Edit web1'));
    await fireEvent.click(view.getByText('Save'));

    await view.findByText(/Bad configuration option/);
    expect(overlay.current?.kind).toBe('sshHost'); // still open, to fix it
    expect(Service.Save).toHaveBeenCalledWith(expect.objectContaining({ file: '/home/me/.ssh/config.d/prod', line: 1, version: 'v2' }));
  });

  it('saves a known host as a config entry', async () => {
    const view = await renderBoth();
    await fireEvent.contextMenu(view.getByText('10.0.0.5'));
    pick('Save as Host…');
    await waitFor(() => expect(document.querySelector('.dialog h2')?.textContent).toBe('Save Known Host'));
    const values = [...document.querySelectorAll('.dialog input')].map((i) => (i as HTMLInputElement).value);
    expect(values.slice(0, 4)).toEqual(['10.0.0.5', '10.0.0.5', '', '2222']);
  });

  it('deletes after asking', async () => {
    const view = await renderBoth();
    vi.mocked(Service.Delete).mockResolvedValue({ ...LIST, hosts: HOSTS.slice(1) } as never);
    await fireEvent.contextMenu(view.getByText('bastion'));
    pick('Delete Host…');
    await fireEvent.click(await view.findByText('Delete Host'));
    expect(Service.Delete).toHaveBeenCalledWith('/home/me/.ssh/config', 1, 'v1');
    await waitFor(() => expect(view.queryByText('bastion')).toBeNull());
  });

  it('says who manages hosts it can’t edit', async () => {
    vi.mocked(Service.List).mockResolvedValue({
      ...LIST, hosts: [host({ alias: 'orb', source: '/home/me/.orbstack/ssh/config', editable: false })],
    } as never);
    const view = render(SshView);
    await fireEvent.contextMenu(await view.findByText('orb'));
    expect(menuLabels()).toContain('Managed by ~/.orbstack/ssh/config');
    expect(menuLabels()).not.toContain('Edit Host…');
  });

  it('connects from the row button', async () => {
    const view = render(SshView);
    await fireEvent.click(await view.findByLabelText('Connect to web2'));
    expect(commands().at(-1)).toBe('ssh web2');
  });
});
