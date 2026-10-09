import { Service as Windows } from '../../bindings/github.com/dayanraj/wtf/internal/windows';
import { homeDir, prettyPath } from './backend';
import { leaves, type LayoutNode } from './layout';
import { fieldsIn, fill, fillPlain, type Field, type Workspace, type WorkspaceDef } from './snippets.svelte';
import { GROUP_COLORS, tabs, type GroupColor } from './tabs.svelte';

// Workspaces: named sets of tabs — folders, split panes, tab groups and
// startup commands — opened in one go (see internal/snippets/workspaces.go
// for the file format). A workspace is first turned into a plan, with its
// placeholders filled in; the plan is what opens, here or in a new window.

export interface PlannedPane {
  cwd: string;
  command: string;
}

export interface PlannedTab {
  title: string;
  group: string;
  color: string;
  /** For several panes: side by side (row) or stacked (column). */
  layout: 'row' | 'column';
  panes: PlannedPane[];
}

export interface Plan {
  name: string;
  tabs: PlannedTab[];
}

/** A tab's panes: its own folder and command, or its pane list. */
const panesOf = (t: Workspace['tabs'][number]) => (t.panes.length ? t.panes : [{ cwd: t.cwd, command: t.command }]);

/** The values a workspace asks for, from every title, group, folder and command. */
export function workspaceFields(w: Workspace): Field[] {
  const texts: string[] = [];
  for (const t of w.tabs) {
    texts.push(t.title, t.group);
    for (const p of panesOf(t)) texts.push(p.cwd, p.command);
  }
  return fieldsIn(texts, w.params);
}

/** ~ and ~/… to the home folder; other folders as given. */
function expandHome(path: string): string {
  const home = homeDir();
  if (!path || !home) return path;
  if (path === '~') return home;
  return path.startsWith('~/') ? home + path.slice(1) : path;
}

/**
 * The tabs a workspace opens, with its placeholders filled in: commands
 * shell-quoted, folders and names as typed. Throws, naming the first
 * field left empty.
 */
export function planWorkspace(w: Workspace, values: Record<string, string>): Plan {
  const fields = workspaceFields(w);
  return {
    name: w.name,
    tabs: w.tabs.map((t) => ({
      title: fillPlain(t.title, values),
      group: fillPlain(t.group, values),
      color: t.color,
      layout: t.layout === 'column' ? 'column' : 'row',
      panes: panesOf(t).map((p) => ({
        cwd: expandHome(fillPlain(p.cwd, values)),
        command: p.command ? fill(p.command, values, fields) : '',
      })),
    })),
  };
}

/** Each split takes its share of the panes it holds, so N panes get 1/N each. */
function evenOut(node: LayoutNode) {
  if (node.kind !== 'split') return;
  node.ratio = leaves(node.a).length / leaves(node).length;
  evenOut(node.a);
  evenOut(node.b);
}

/**
 * Opens a plan's tabs in this window: folders, startup commands (typed
 * into each new shell, as if by you), splits, names and groups. Tabs
 * already open stay as they are.
 */
export function openPlan(plan: Plan) {
  const groups = new Map<string, number>();
  let first: number | null = null;
  for (const t of plan.tabs) {
    const [head, ...rest] = t.panes;
    const id = tabs.open({ cwd: head?.cwd ?? '', command: head?.command || undefined });
    first ??= id;
    for (const p of rest) tabs.split(t.layout, { cwd: p.cwd || head?.cwd || '', command: p.command || undefined });
    const tab = tabs.get(id);
    if (tab) {
      evenOut(tab.root);
      // Start with the first pane focused, as the workspace lists them.
      tab.focusedPaneId = leaves(tab.root)[0].id;
    }
    if (t.title) tabs.rename(id, t.title);
    if (t.group) {
      const existing = groups.get(t.group);
      if (existing !== undefined && tabs.group(existing)) {
        tabs.addToGroup(id, existing);
      } else {
        const g = tabs.createGroup(id);
        if (g) {
          g.name = t.group;
          if (GROUP_COLORS.includes(t.color as GroupColor)) g.color = t.color as GroupColor;
          groups.set(t.group, g.id);
        }
      }
    }
  }
  if (first !== null) tabs.activate(first);
}

/** A plan handed to a new window, which opens it instead of a fresh tab. */
export interface WorkspacePayload {
  kind: 'workspace';
  plan: Plan;
}

/** Opens a workspace here, or in a new window when it says so (or `newWindow`). */
export async function openWorkspace(w: Workspace, values: Record<string, string>, newWindow = w.window === 'new') {
  const plan = planWorkspace(w, values);
  if (newWindow) {
    const payload: WorkspacePayload = { kind: 'workspace', plan };
    await Windows.NewWindow(JSON.stringify(payload), -1, -1);
  } else {
    openPlan(plan);
  }
}

/**
 * This window's tabs as a workspace, for Save Window as Workspace: names,
 * groups, splits, folders — and the command running in a pane (ssh host,
 * kubectl … -w) as its startup command.
 */
export function captureWindow(): WorkspaceDef {
  return {
    name: '',
    description: '',
    window: '',
    params: [],
    tabs: tabs.list.map((t) => {
      const group = tabs.group(t.groupId);
      const panes = leaves(t.root).map((p) => ({ cwd: prettyPath(p.cwd), command: p.running ? p.command : '' }));
      return {
        title: t.customTitle ?? '',
        group: group?.name ?? '',
        color: group ? group.color : '',
        cwd: '',
        command: '',
        layout: t.root.kind === 'split' ? t.root.dir : '',
        panes,
      };
    }),
  };
}
