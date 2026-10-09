import { SvelteMap } from 'svelte/reactivity';
import {
  Service, type AccessKey, type Draft, type Identity, type List, type Profile as RawProfile, type Setting,
} from '../../bindings/github.com/dayanraj/wtf/internal/awsprofiles';
import { shellQuote } from './shell';
import { tabs } from './tabs.svelte';
import { terminalFor } from './terminals';

export type { AccessKey, Identity, Setting } from '../../bindings/github.com/dayanraj/wtf/internal/awsprofiles';

// The AWS tab: named profiles from ~/.aws/credentials and ~/.aws/config
// (see internal/awsprofiles). Secrets are never sent here; every change is
// planned by the backend first and confirmed by the user before it's made.

/** A profile, with Go's nil slices made empty. */
export type Profile = RawProfile & {
  settings: Setting[];
  credentialsOther: Setting[];
  locked: string[];
  sourceFor: string[];
};

export type ProfileKind = 'keys' | 'role' | 'sso' | 'process' | 'none';

export const KIND_LABEL: Record<string, string> = {
  keys: 'Access keys',
  role: 'Assume role',
  sso: 'SSO',
  process: 'Credential process',
  none: 'Settings only',
};

/** Config settings with their own fields in the editor, by kind. */
export const KIND_FIELDS: Record<ProfileKind, { key: string; label: string; placeholder: string }[]> = {
  keys: [],
  role: [
    { key: 'role_arn', label: 'Role ARN', placeholder: 'arn:aws:iam::123456789012:role/Admin' },
    { key: 'source_profile', label: 'Source profile', placeholder: 'the profile whose keys assume the role' },
    { key: 'mfa_serial', label: 'MFA device ARN', placeholder: 'optional' },
    { key: 'external_id', label: 'External ID', placeholder: 'optional' },
  ],
  sso: [
    { key: 'sso_session', label: 'SSO session', placeholder: 'a [sso-session] name in ~/.aws/config' },
    { key: 'sso_account_id', label: 'Account ID', placeholder: '123456789012' },
    { key: 'sso_role_name', label: 'Role name', placeholder: 'AdministratorAccess' },
    { key: 'sso_start_url', label: 'Start URL (without a session)', placeholder: 'optional' },
    { key: 'sso_region', label: 'SSO region (without a session)', placeholder: 'optional' },
  ],
  process: [{ key: 'credential_process', label: 'Command', placeholder: '/usr/local/bin/get-creds --profile x' }],
  none: [],
};

const COMMON_KEYS = ['region', 'output'];

export const REGIONS = [
  'us-east-1', 'us-east-2', 'us-west-1', 'us-west-2', 'ca-central-1', 'sa-east-1',
  'eu-west-1', 'eu-west-2', 'eu-west-3', 'eu-central-1', 'eu-central-2', 'eu-north-1', 'eu-south-1',
  'ap-south-1', 'ap-southeast-1', 'ap-southeast-2', 'ap-northeast-1', 'ap-northeast-2', 'ap-east-1',
  'me-central-1', 'af-south-1',
];

/** What the editor holds. */
export interface ProfileDraft {
  name: string;
  kind: ProfileKind;
  accessKeyId: string;
  /** Empty keeps the current secret. */
  secret: string;
  sessionToken: string;
  removeSessionToken: boolean;
  region: string;
  output: string;
  /** The kind's own fields, by config key. */
  fields: Record<string, string>;
  /** Any other config settings, one "key = value" per line. */
  more: string;
}

const get = (list: Setting[], key: string) => list.find((s) => s.key.toLowerCase() === key)?.value ?? '';

export function draftOf(p: Profile | null): ProfileDraft {
  const d: ProfileDraft = {
    name: '', kind: 'keys', accessKeyId: '', secret: '', sessionToken: '', removeSessionToken: false,
    region: '', output: '', fields: {}, more: '',
  };
  if (!p) return d;
  d.name = p.name;
  d.kind = (p.kind as ProfileKind) || 'none';
  d.accessKeyId = p.accessKeyId;
  d.region = get(p.settings, 'region');
  d.output = get(p.settings, 'output');
  for (const f of KIND_FIELDS[d.kind] ?? []) d.fields[f.key] = get(p.settings, f.key);
  // Settings without a field of this kind stay as lines, so nothing is lost.
  const own = new Set([...COMMON_KEYS, ...(KIND_FIELDS[d.kind] ?? []).map((f) => f.key)]);
  d.more = p.settings
    .filter((s) => !own.has(s.key.toLowerCase()))
    .map((s) => `${s.key} = ${s.value}`)
    .join('\n');
  return d;
}

/** The backend draft for an editor draft. Throws on a malformed "more" line. */
export function toDraft(d: ProfileDraft, original: string, versions: Record<string, string>): Draft {
  const settings: Setting[] = [];
  if (d.region.trim()) settings.push({ key: 'region', value: d.region.trim() });
  if (d.output.trim()) settings.push({ key: 'output', value: d.output.trim() });
  for (const f of KIND_FIELDS[d.kind]) {
    const v = (d.fields[f.key] ?? '').trim();
    if (v) settings.push({ key: f.key, value: v });
  }
  for (const line of d.more.split('\n')) {
    if (!line.trim()) continue;
    const m = /^\s*([^=\s]+)\s*=\s*(.*)$/.exec(line);
    if (!m || !m[2].trim()) throw new Error(`“${line.trim()}” should look like “key = value”`);
    settings.push({ key: m[1], value: m[2].trim() });
  }
  return {
    original,
    name: d.name.trim(),
    versions,
    useKeys: d.kind === 'keys',
    accessKeyId: d.kind === 'keys' ? d.accessKeyId.trim() : '',
    secretAccessKey: d.kind === 'keys' ? d.secret.trim() : '',
    sessionToken: d.kind === 'keys' ? d.sessionToken.trim() : '',
    removeSessionToken: d.kind === 'keys' && d.removeSessionToken,
    settings,
  };
}

/** "AKIA…WXYZ" */
export function shortKey(id: string): string {
  return id.length > 8 ? `${id.slice(0, 4)}…${id.slice(-4)}` : id;
}

/** Whole days since an ISO date. */
export function daysSince(iso: string, now = Date.now()): number {
  return Math.floor((now - Date.parse(iso)) / 86_400_000);
}

export function ago(iso: string, now = Date.now()): string {
  const d = daysSince(iso, now);
  return d <= 0 ? 'today' : d === 1 ? 'yesterday' : `${d} days ago`;
}

/** What a check of a profile found (sts, iam). */
export interface Check {
  busy?: boolean;
  identity?: Identity;
  identityError?: string;
  keys?: AccessKey[];
  keysError?: string;
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

class AwsProfiles {
  profiles = $state.raw<Profile[]>([]);
  problems = $state.raw<string[]>([]);
  credentialsFile = $state('');
  configFile = $state('');
  loaded = $state(false);
  loading = $state(false);
  versions: Record<string, string> = {};
  checks = new SvelteMap<string, Check>();

  apply(l: List) {
    this.profiles = (l.profiles ?? []).map((p) => ({
      ...p,
      settings: p.settings ?? [],
      credentialsOther: p.credentialsOther ?? [],
      locked: p.locked ?? [],
      sourceFor: p.sourceFor ?? [],
    }));
    this.problems = l.problems ?? [];
    this.credentialsFile = l.credentialsFile;
    this.configFile = l.configFile;
    this.versions = Object.fromEntries(Object.entries(l.versions ?? {}).map(([k, v]) => [k, v ?? '']));
  }

  async load() {
    this.loading = true;
    try {
      this.apply(await Service.List());
    } catch (e) {
      this.problems = [message(e)];
    } finally {
      this.loading = false;
      this.loaded = true;
    }
  }

  async verify(name: string) {
    const prev = this.checks.get(name) ?? {};
    this.checks.set(name, { ...prev, busy: true, identityError: undefined });
    try {
      this.checks.set(name, { ...prev, busy: false, identity: await Service.Verify(name), identityError: undefined });
    } catch (e) {
      this.checks.set(name, { ...prev, busy: false, identity: undefined, identityError: message(e) });
    }
  }

  async checkKeys(name: string) {
    const prev = this.checks.get(name) ?? {};
    this.checks.set(name, { ...prev, busy: true });
    try {
      this.checks.set(name, { ...prev, busy: false, keys: (await Service.AccessKeys(name)) ?? [], keysError: undefined });
    } catch (e) {
      this.checks.set(name, { ...prev, busy: false, keys: undefined, keysError: message(e) });
    }
  }
}

export const aws = new AwsProfiles();

/** The profile the focused shell uses, as its last prompt reported. */
export function focusedProfile(): string {
  const p = tabs.activePane?.infra?.awsProfile ?? '';
  return p === 'env credentials' ? '' : p;
}

/** The command that points a shell at a profile. */
export function useCommand(name: string): string {
  return `export AWS_PROFILE=${shellQuote(name)}`;
}

const encoder = new TextEncoder();

/**
 * Points the focused shell at a profile when it's sitting at its prompt;
 * otherwise (a program is running, or ssh) opens a new tab that does.
 * Returns where it went.
 */
export function useInShell(name: string): 'here' | 'tab' {
  const pane = tabs.activePane;
  const term = pane && terminalFor(pane.id);
  if (pane && term && pane.isShell && !pane.running) {
    term.sendInput(encoder.encode(useCommand(name) + '\r'));
    term.focus();
    return 'here';
  }
  tabs.open({ command: useCommand(name) });
  return 'tab';
}
