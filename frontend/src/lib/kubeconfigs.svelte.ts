import { SvelteMap } from 'svelte/reactivity';
import {
  Service, type ConfigList, type Connection, type KubeContext as RawContext, type Op,
} from '../../bindings/github.com/dayanraj/wtf/internal/kube';
import { shellQuote } from './shell';
import { tabs } from './tabs.svelte';
import { onCommandFinished, terminalFor } from './terminals';

export type { Connection, Op } from '../../bindings/github.com/dayanraj/wtf/internal/kube';

// The Kubernetes tab's Contexts view: the contexts of the focused shell's
// kubeconfig files (see internal/kube/configs.go). Listing reads files
// only; Test Connection is the one thing that reaches a cluster, and only
// when asked. Every edit is planned, confirmed, and backed up.

export type KubeContext = RawContext & { execArgs: string[]; execEnv: string[]; problems: string[] };

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** How a context logs in, in a few words. */
export function authLabel(c: KubeContext): string {
  switch (c.auth) {
    case 'exec': {
      const cmd = c.execCommand.slice(c.execCommand.lastIndexOf('/') + 1);
      return cmd === 'aws' && c.execArgs.includes('get-token') ? 'aws eks get-token' : cmd;
    }
    case 'client-cert':
      return 'client certificate';
    case 'token':
      return 'token';
    case 'auth-provider':
      return 'auth provider';
    case 'basic':
      return 'username and password';
  }
  return 'no credentials';
}

/** The cluster, briefly: an EKS cluster's name, else the server's host. */
export function clusterLabel(c: KubeContext): string {
  if (c.eksCluster) return `${c.eksCluster} (EKS ${c.eksRegion})`;
  try {
    return new URL(c.server).host || c.cluster;
  } catch {
    return c.cluster;
  }
}

/** `aws eks update-kubeconfig` for the Add EKS Cluster dialog. */
export function eksCommand(cluster: string, region: string, profile: string, alias: string, kubeconfig = ''): string {
  const parts = ['aws eks update-kubeconfig', '--name', shellQuote(cluster), '--region', shellQuote(region)];
  if (profile) parts.push('--profile', shellQuote(profile));
  if (alias) parts.push('--alias', shellQuote(alias));
  // A shell with its own KUBECONFIG: write to its file, not ~/.kube/config.
  if (kubeconfig) parts.push('--kubeconfig', shellQuote(kubeconfig));
  return parts.join(' ');
}

export interface Check {
  busy: boolean;
  result?: Connection;
  error?: string;
}

class KubeConfigs {
  contexts = $state.raw<KubeContext[]>([]);
  files = $state.raw<string[]>([]);
  /** Other kubeconfigs in ~/.kube, which the shell doesn't read. */
  otherFiles = $state.raw<string[]>([]);
  problems = $state.raw<string[]>([]);
  loaded = $state(false);
  versions: Record<string, string> = {};
  checks = new SvelteMap<string, Check>();

  /** The focused shell's variables: its KUBECONFIG and PATH decide everything. */
  env(): Record<string, string> {
    return tabs.activePane?.env ?? {};
  }

  apply(l: ConfigList) {
    this.contexts = (l.contexts ?? []).map((c) => ({ ...c, execArgs: c.execArgs ?? [], execEnv: c.execEnv ?? [], problems: c.problems ?? [] }));
    this.files = l.files ?? [];
    this.otherFiles = l.otherFiles ?? [];
    this.problems = l.problems ?? [];
    this.versions = Object.fromEntries(Object.entries(l.versions ?? {}).map(([k, v]) => [k, v ?? '']));
    this.loaded = true;
  }

  #watching = false;

  async load() {
    if (!this.#watching) {
      this.#watching = true;
      // Commands that change kubeconfigs, run in a terminal.
      onCommandFinished((command) => {
        if (this.loaded && /\b(aws\s+eks\s+update-kubeconfig|kubectl\s+config\s|eksctl\s+utils\s+write-kubeconfig)/.test(command)) this.load();
      });
    }
    try {
      this.apply(await Service.Contexts(this.env()));
    } catch (e) {
      this.problems = [message(e)];
      this.loaded = true;
    }
  }

  async test(c: KubeContext) {
    const key = contextKey(c);
    this.checks.set(key, { busy: true });
    try {
      this.checks.set(key, { busy: false, result: await Service.TestConnection(this.env(), c.name, c.file) });
    } catch (e) {
      this.checks.set(key, { busy: false, error: message(e) });
    }
  }

  plan(op: Op): Promise<string[]> {
    return Service.PlanEdit(this.env(), op).then((p) => p ?? []);
  }

  async applyEdit(op: Op) {
    this.apply(await Service.ApplyEdit(this.env(), op, this.versions));
  }
}

export const kubeConfigs = new KubeConfigs();

/** Contexts in different files may share a name. */
export function contextKey(c: KubeContext): string {
  return `${c.file}\n${c.name}`;
}

const encoder = new TextEncoder();

/**
 * Points the focused shell at a context — that shell only — by typing the
 * export at its prompt; when a program is running, opens a new tab instead.
 * A context from another kubeconfig in ~/.kube makes that shell read only
 * that file.
 */
export async function useContext(c: KubeContext, newTab = false): Promise<'here' | 'tab'> {
  const env = kubeConfigs.env();
  const command = await Service.ShellConfig(env, c.name, c.file, '');
  const pane = tabs.activePane;
  const term = pane && terminalFor(pane.id);
  if (!newTab && pane && term && pane.isShell && !pane.running) {
    term.sendInput(encoder.encode(command + '\r'));
    term.focus();
    return 'here';
  }
  tabs.open({ command });
  return 'tab';
}
