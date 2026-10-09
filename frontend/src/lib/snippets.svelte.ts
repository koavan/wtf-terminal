import {
  Service, type Edit, type List, type Param, type Snippet as RawSnippet, type Target, type WPane, type WTab,
  type Workspace as RawWorkspace,
} from '../../bindings/github.com/dayanraj/wtf/internal/snippets';
import { shellIdle } from './fileActions';
import { overlay } from './overlay.svelte';
import { shellQuote } from './shell';
import { tabs, type Pane } from './tabs.svelte';
import { activeTerminal } from './terminals';
import { toast } from './toast.svelte';

export type { Def, Edit, Preview, Target, WPane, WTab, WorkspaceDef } from '../../bindings/github.com/dayanraj/wtf/internal/snippets';

// Snippets and runbooks: commands with {{placeholders}} from the user's and
// the project's snippets files (see internal/snippets). This fills them in.
//
//   {{name}}           asks for a value; shell-quoted when it needs to be
//   {{name=default}}   with a default: a literal, or $kube_namespace,
//                      $kube_context, $aws_profile, $aws_region, $cwd
//
// Only {{identifier}} forms are placeholders: a Go template such as
// {{.metadata.name}} or {{end}} in a command is left alone.

/** A workspace, with Go's nil slices made empty. */
export type Workspace = Omit<RawWorkspace, 'tabs' | 'params'> & {
  tabs: (Omit<WTab, 'panes'> & { panes: WPane[] })[];
  params: Param[];
};

export type Snippet = Omit<RawSnippet, 'steps' | 'params' | 'tags'> & {
  steps: { title: string; command: string }[];
  params: Param[];
  tags: string[];
};

const PLACEHOLDER = /\{\{([A-Za-z_][\w-]*)(?:=([^}]*))?\}\}/g;

/** Words Go templates (kubectl -o go-template, Helm) use bare: never placeholders. */
const TEMPLATE_WORDS = new Set(['end', 'else', 'break', 'continue', 'nil', 'true', 'false']);

/** One value to ask for. */
export interface Field {
  name: string;
  description: string;
  /** As written: a literal or a $variable. */
  default: string;
  choices: string[];
  raw: boolean;
}

/** The shell's context, for $variable defaults. */
export interface ShellFacts {
  kube_context: string;
  kube_namespace: string;
  aws_profile: string;
  aws_region: string;
  cwd: string;
}

export const isRunbook = (s: Snippet) => s.steps.length > 0;

/** A snippet's commands: its one command, or each step's. */
export const commandsOf = (s: Snippet) => (isRunbook(s) ? s.steps.map((st) => st.command) : [s.command]);

/** The values a snippet asks for, in order of first use, with what its file says about them. */
export function fieldsOf(s: Snippet): Field[] {
  return fieldsIn(commandsOf(s), s.params);
}

/** The placeholders in some texts, in order of first use, with their declared settings. */
export function fieldsIn(texts: string[], params: Param[]): Field[] {
  const out: Field[] = [];
  for (const cmd of texts) {
    for (const m of cmd.matchAll(PLACEHOLDER)) {
      const [, name, inline] = m;
      if (TEMPLATE_WORDS.has(name) && inline === undefined) continue;
      const existing = out.find((f) => f.name === name);
      if (existing) {
        if (!existing.default && inline !== undefined) existing.default = inline;
        continue;
      }
      const p = params.find((x) => x.name === name);
      out.push({
        name,
        description: p?.description ?? '',
        default: inline ?? p?.default ?? '',
        choices: p?.choices ?? [],
        raw: p?.raw ?? false,
      });
    }
  }
  return out;
}

/** A default with its $variable worked out ("" when the shell doesn't say). */
export function resolveDefault(def: string, facts: ShellFacts): string {
  const m = /^\$(\w+)$/.exec(def.trim());
  if (!m) return def;
  return m[1] in facts ? facts[m[1] as keyof ShellFacts] : def;
}

/** What the focused shell is pointed at. */
export function shellFacts(pane: Pane | undefined = tabs.activePane): ShellFacts {
  const env = pane?.env ?? {};
  return {
    kube_context: pane?.infra?.kubeContext ?? '',
    kube_namespace: pane?.infra?.kubeNamespace || (pane?.infra?.kubeContext ? 'default' : ''),
    aws_profile: pane?.infra?.awsProfile && pane.infra.awsProfile !== 'env credentials' ? pane.infra.awsProfile : (env.AWS_PROFILE ?? ''),
    aws_region: pane?.infra?.awsRegion || env.AWS_REGION || env.AWS_DEFAULT_REGION || '',
    cwd: pane?.cwd ?? '',
  };
}

/**
 * A command with its placeholders filled in. Values are shell-quoted
 * unless the field is raw. Throws, naming the first field left empty.
 */
export function fill(command: string, values: Record<string, string>, fields: Field[]): string {
  return command.replace(PLACEHOLDER, (whole: string, name: string, inline?: string) => {
    if (TEMPLATE_WORDS.has(name) && inline === undefined) return whole;
    const v = (values[name] ?? '').trim();
    if (!v) throw new Error(`Fill in ${name}`);
    return fields.find((f) => f.name === name)?.raw ? v : shellQuote(v);
  });
}

/**
 * Text (a folder, a tab's title) with its placeholders filled in as typed,
 * no quoting. Throws, naming the first field left empty.
 */
export function fillPlain(text: string, values: Record<string, string>): string {
  return text.replace(PLACEHOLDER, (whole: string, name: string, inline?: string) => {
    if (TEMPLATE_WORDS.has(name) && inline === undefined) return whole;
    const v = (values[name] ?? '').trim();
    if (!v) throw new Error(`Fill in ${name}`);
    return v;
  });
}

/** Snippets for the palette: what's in the user's and the focused shell's project files. */
class Snippets {
  list = $state.raw<Snippet[]>([]);
  workspaces = $state.raw<Workspace[]>([]);
  problems = $state.raw<string[]>([]);
  userFile = $state('');
  projectFile = $state('');
  /** Files snippets can be saved to from the focused shell's folder. */
  targets = $state.raw<Target[]>([]);
  /** The files that were read, in list order. */
  files = $state.raw<string[]>([]);
  versions: Record<string, string> = {};
  loaded = $state(false);

  apply(l: List) {
    this.workspaces = (l.workspaces ?? []).map((w) => ({
      ...w,
      params: (w.params ?? []).map((p) => ({ ...p, choices: p.choices ?? [] })),
      tabs: (w.tabs ?? []).map((t) => ({ ...t, panes: t.panes ?? [] })),
    }));
    this.list = (l.snippets ?? []).map((s) => ({ ...s, steps: s.steps ?? [], params: (s.params ?? []).map((p) => ({ ...p, choices: p.choices ?? [] })), tags: s.tags ?? [] }));
    this.problems = l.problems ?? [];
    this.userFile = l.userFile;
    this.projectFile = l.projectFile;
    this.targets = l.targets ?? [];
    this.files = l.files ?? [];
    this.versions = Object.fromEntries(Object.entries(l.versions ?? {}).map(([k, v]) => [k, v ?? '']));
    this.loaded = true;
  }

  cwd(): string {
    return tabs.activePane?.cwd ?? '';
  }

  plan(e: Edit) {
    return Service.PlanEdit(e, this.cwd());
  }

  async save(e: Edit) {
    this.apply(await Service.ApplyEdit(e, this.cwd()));
  }

  async remove(s: { source: string; index: number }, kind: 'snippet' | 'workspace' = 'snippet') {
    this.apply(await Service.Delete(kind, s.source, s.index, this.versions[s.source] ?? '', this.cwd()));
  }

  /** Reads the files afresh (they're small, and edited by hand). */
  async load() {
    try {
      this.apply(await Service.List(tabs.activePane?.cwd ?? ''));
    } catch (e) {
      this.problems = [e instanceof Error ? e.message : String(e)];
    }
  }
}

export const snippets = new Snippets();

/** Opens a snippet: straight to the prompt when there's nothing to fill in, else its form. */
export function openSnippet(s: Snippet) {
  if (!isRunbook(s) && fieldsOf(s).length === 0) runSnippetCommand(s.command, 'insert');
  else overlay.show({ kind: 'snippet', snippet: s });
}

export type RunHow = 'insert' | 'run' | 'tab';

/**
 * Puts a command at the focused shell's prompt (to check and press Enter),
 * runs it there, or runs it in a new tab. False when the shell is busy.
 */
export function runSnippetCommand(command: string, how: RunHow): boolean {
  if (how === 'tab') {
    tabs.open({ command });
    return true;
  }
  const term = activeTerminal();
  if (!term || !shellIdle()) {
    toast.show('The shell is busy — finish the running program first, or use Run in New Tab');
    return false;
  }
  // Ctrl-U clears anything half-typed at the prompt first.
  term.type(`\x15${command}${how === 'run' ? '\r' : ''}`);
  term.focus();
  return true;
}
