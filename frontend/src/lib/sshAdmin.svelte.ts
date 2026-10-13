import { Events } from '@wailsio/runtime';
import {
  Service, type Key as RawKey, type Tunnel, type TunnelStatus,
} from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
import { prettyPath } from './backend';
import { isMac } from './platform';
import { shellQuote } from './shell';
import { ssh, type Host } from './ssh.svelte';
import { tabs } from './tabs.svelte';
import { onCommandFinished, runInPane } from './terminals';

export type { HostKey, HostKeys, Tunnel, TunnelStatus } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';

/** A key, with Go's nil slice made empty. */
export type Key = RawKey & { usedBy: string[] };

// The SSH tab's Keys and Tunnels sections, and the commands behind its
// admin actions (see internal/sshhosts). Anything that can ask for a
// passphrase or password runs as a real command in the focused shell (a
// new tab when it's busy): it lands in the shell history, and the secret
// never passes through wtf.

export const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** A path for a command line: ~/… stays unquoted at the front so the shell expands it. */
export function quotePath(path: string): string {
  const p = prettyPath(path);
  if (p === '~') return p;
  return p.startsWith('~/') ? `~/${shellQuote(p.slice(2))}` : shellQuote(p);
}

/** Runs a command in the focused shell when it's at a prompt, else a new tab. */
export function runInShell(command: string): 'here' | 'tab' {
  return runInPane(tabs.activePane, command);
}

/** "-p 2222 " for a known host listed with a port; config hosts carry their own. */
function portFlag(h: Host, flag = '-p'): string {
  return h.known && h.port && h.port !== '22' ? `${flag} ${shellQuote(h.port)} ` : '';
}

// ── Keys ─────────────────────────────────────────────────────────────────

export type KeyType = 'ed25519' | 'rsa' | 'ecdsa';

export const KEY_TYPES: { id: KeyType; label: string; args: string; file: string }[] = [
  { id: 'ed25519', label: 'Ed25519 (recommended)', args: '-t ed25519', file: 'id_ed25519' },
  { id: 'rsa', label: 'RSA 4096 (for older servers)', args: '-t rsa -b 4096', file: 'id_rsa' },
  { id: 'ecdsa', label: 'ECDSA P-521', args: '-t ecdsa -b 521', file: 'id_ecdsa' },
];

export interface KeyDraft {
  type: KeyType;
  name: string;
  comment: string;
  addToAgent: boolean;
}

/** ssh-keygen asks for the passphrase itself, in the tab. */
export function keygenCommand(d: KeyDraft): string {
  const type = KEY_TYPES.find((t) => t.id === d.type) ?? KEY_TYPES[0];
  const path = `~/.ssh/${shellQuote(d.name.trim())}`;
  const comment = d.comment.trim() ? ` -C ${shellQuote(d.comment.trim())}` : '';
  const gen = `ssh-keygen ${type.args}${comment} -f ${path}`;
  return d.addToAgent ? `${gen} && ${agentAddCommand(path, isMac)}` : gen;
}

/** On macOS the passphrase can go to the Keychain, so the key loads again after a restart. */
export function agentAddCommand(quotedPath: string, keychain: boolean): string {
  return `ssh-add ${keychain ? '--apple-use-keychain ' : ''}${quotedPath}`;
}

export function changePassphraseCommand(k: Key): string {
  return `ssh-keygen -p -f ${quotePath(k.path)}`;
}

/** Appends the public key to the host's authorized_keys; ssh-copy-id asks for the password. */
export function installKeyCommand(k: Key, h: Host): string {
  return `ssh-copy-id -i ${quotePath(k.path + '.pub')} ${portFlag(h)}${shellQuote(h.alias)}`;
}

/** A free file name like id_ed25519, id_ed25519_2, … */
export function freeKeyName(base: string, keys: Key[]): string {
  const taken = new Set(keys.map((k) => prettyPath(k.path)));
  for (let i = 1; ; i++) {
    const name = i === 1 ? base : `${base}_${i}`;
    if (!taken.has(`~/.ssh/${name}`)) return name;
  }
}

const KEY_TOOLS = /^\s*(?:sudo\s+)?(?:ssh-keygen|ssh-add|ssh-copy-id|ssh-keyscan)\b/;

class SshKeys {
  keys = $state.raw<Key[]>([]);
  agentError = $state('');
  agentSocket = $state('');
  problems = $state.raw<string[]>([]);
  loaded = $state(false);
  loading = $state(false);
  #watching = false;

  async load() {
    this.#watch();
    this.loading = true;
    try {
      const l = await Service.KeyList();
      this.keys = (l.keys ?? []).map((k) => ({ ...k, usedBy: k.usedBy ?? [] }));
      this.agentError = l.agentError;
      this.agentSocket = l.agentSocket;
      this.problems = l.problems ?? [];
    } catch (e) {
      this.problems = [message(e)];
    } finally {
      this.loading = false;
      this.loaded = true;
    }
  }

  /** Keys and hosts change when ssh-keygen, ssh-add or ssh-copy-id finish in a tab. */
  #watch() {
    if (this.#watching) return;
    this.#watching = true;
    onCommandFinished((command) => {
      if (!KEY_TOOLS.test(command)) return;
      if (this.loaded) this.load();
      if (ssh.loaded) ssh.load();
    });
  }
}

export const sshKeys = new SshKeys();

// ── Tunnels ──────────────────────────────────────────────────────────────

/** A forward spec's parts: [listen (maybe with a bind address), dest host, dest port]. */
const FORWARD = /^(.*?):(\[[^\]]+\]|[^:]+):(\d+)$/;

export type TunnelKind = 'local' | 'remote' | 'dynamic' | 'kubectl';

export interface TunnelDraft {
  name: string;
  host: string;
  port: string;
  kind: TunnelKind;
  /** local: the port here; remote: the port on the server; dynamic: the SOCKS port. */
  listenPort: string;
  /** Where connections go: from the server (local) or from here (remote). */
  destHost: string;
  destPort: string;
  autoStart: boolean;
  /** kubectl only: the context, namespace and target (svc/web, deploy/api, pod/x). */
  context: string;
  namespace: string;
  resource: string;
  /** kubectl only: the shell variables it runs with (KUBECONFIG, AWS profile, PATH). */
  env: Record<string, string>;
}

export function emptyTunnel(host = '', port = ''): TunnelDraft {
  return {
    name: '', host, port, kind: 'local', listenPort: '', destHost: 'localhost', destPort: '', autoStart: false,
    context: '', namespace: '', resource: '', env: {},
  };
}

export function tunnelSpec(d: TunnelDraft): string {
  if (d.kind === 'dynamic') return d.listenPort.trim();
  if (d.kind === 'kubectl') {
    const local = d.listenPort.trim();
    const remote = d.destPort.trim();
    return remote && remote !== local ? `${local}:${remote}` : local;
  }
  const host = d.destHost.trim() || 'localhost';
  const bracketed = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
  return `${d.listenPort.trim()}:${bracketed}:${d.destPort.trim()}`;
}

/** The reverse of tunnelSpec, for editing. Keeps a bind address with the port. */
export function draftOfTunnel(t: Tunnel): TunnelDraft {
  const d: TunnelDraft = { ...emptyTunnel(t.host, t.port), name: t.name, kind: t.kind as TunnelKind, autoStart: t.autoStart };
  if (t.kind === 'dynamic') {
    d.listenPort = t.spec;
    return d;
  }
  if (t.kind === 'kubectl') {
    const [local, remote] = t.spec.split(':');
    return { ...d, listenPort: local, destPort: remote ?? local, context: t.context ?? '', namespace: t.namespace ?? '', resource: t.resource ?? '', env: { ...(t.env ?? {}) } as Record<string, string> };
  }
  const m = FORWARD.exec(t.spec);
  if (m) {
    d.listenPort = m[1];
    d.destHost = m[2].replace(/^\[(.*)\]$/, '$1');
    d.destPort = m[3];
  } else {
    d.listenPort = t.spec;
  }
  return d;
}

/** The local port a tunnel listens on, for local and SOCKS tunnels. */
export function localPort(t: Tunnel): string | null {
  if (t.kind === 'remote') return null;
  if (t.kind === 'kubectl') return t.spec.split(':')[0] || null;
  const listen = t.kind === 'dynamic' ? t.spec : FORWARD.exec(t.spec)?.[1];
  return /(\d+)$/.exec(listen ?? '')?.[1] ?? null;
}

/** "localhost:5432 → db.internal:5432 via bastion" */
export function describeTunnel(t: Tunnel): string {
  if (t.kind === 'dynamic') return `SOCKS proxy on localhost:${t.spec} via ${t.host}`;
  if (t.kind === 'kubectl') {
    const [local, remote] = t.spec.split(':');
    return `localhost:${local} → ${t.resource}:${remote ?? local} in ${t.namespace}`;
  }
  const m = FORWARD.exec(t.spec);
  if (!m) return `${t.spec} via ${t.host}`;
  const listen = m[1].includes(':') ? m[1] : `localhost:${m[1]}`;
  return t.kind === 'local'
    ? `${listen} → ${m[2]}:${m[3]} via ${t.host}`
    : `${t.host}:${m[1]} → ${m[2]}:${m[3]} here`;
}

/** The variables a kubectl forward needs from the shell it's made in. */
export function kubectlEnv(env: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const k of ['KUBECONFIG', 'AWS_PROFILE', 'AWS_REGION', 'AWS_DEFAULT_REGION', 'PATH']) if (env[k]) out[k] = env[k];
  return out;
}

/** Whether a tunnel is a kubectl port-forward (rather than ssh). */
export const isKubectl = (t: Tunnel) => t.kind === 'kubectl';

class SshTunnels {
  list = $state.raw<TunnelStatus[]>([]);
  loaded = $state(false);
  #listening = false;

  async load() {
    if (!this.#listening) {
      this.#listening = true;
      Events.On('ssh:tunnels', (e) => (this.list = (e.data as TunnelStatus[] | null) ?? []));
    }
    try {
      this.list = (await Service.Tunnels()) ?? [];
    } finally {
      this.loaded = true;
    }
  }

  /** How many are on; just ssh tunnels or just kubectl forwards when asked. */
  running(kind?: 'ssh' | 'kubectl'): number {
    return this.list.filter(
      (t) => (t.state === 'up' || t.state === 'connecting' || t.state === 'retrying') && (!kind || (kind === 'kubectl') === (t.kind === 'kubectl')),
    ).length;
  }

  async save(id: string, d: TunnelDraft) {
    this.list =
      (await Service.SaveTunnel({
        id,
        name: d.name.trim(),
        host: d.host.trim(),
        port: d.port.trim(),
        kind: d.kind,
        spec: tunnelSpec(d),
        autoStart: d.autoStart,
        context: d.context.trim(),
        namespace: d.namespace.trim(),
        resource: d.resource.trim(),
        env: d.env,
      })) ?? [];
  }

  async start(id: string) {
    this.list = (await Service.StartTunnel(id)) ?? [];
  }

  async stop(id: string) {
    this.list = (await Service.StopTunnel(id)) ?? [];
  }

  async remove(id: string) {
    this.list = (await Service.DeleteTunnel(id)) ?? [];
  }
}

export const sshTunnels = new SshTunnels();

// ── Files ────────────────────────────────────────────────────────────────

/** scp a set of local files or folders to a folder on the host ("" = home). */
export function uploadCommand(h: Host, locals: string[], remoteDir: string): string {
  const files = locals.map(quotePath).join(' ');
  return `scp -r ${portFlag(h, '-P')}${files} ${shellQuote(`${h.alias}:${remoteDir.trim()}`)}`;
}

/** scp a remote file or folder into a local folder. */
export function downloadCommand(h: Host, remotePath: string, localDir: string): string {
  return `scp -r ${portFlag(h, '-P')}${shellQuote(`${h.alias}:${remotePath.trim()}`)} ${quotePath(localDir)}`;
}

export function sftpCommand(h: Host): string {
  return `sftp ${portFlag(h, '-P')}${shellQuote(h.alias)}`;
}
