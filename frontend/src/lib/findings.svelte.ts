import { Service, type Finding, type Report as RawReport, type Source } from '../../bindings/github.com/dayanraj/wtf/internal/findings';
import { onCommandFinished } from './terminals';

export type { Finding, Scanner, Source } from '../../bindings/github.com/dayanraj/wtf/internal/findings';

/** The backend's report with Go's nil slices made empty. */
export type Report = Omit<RawReport, 'sources' | 'findings'> & { sources: Source[]; findings: Finding[] };

// The Findings tab: security scanners' SARIF reports for the focused
// shell's repository (see internal/findings). Read when the tab opens, when
// the shell moves to another repository, on refresh, and when a scan
// finishes in a terminal — never polled.

/** Most severe first: scores (critical…low) and SARIF levels (error…note) interleaved by weight. */
export const SEVERITIES = ['critical', 'high', 'error', 'medium', 'warning', 'low', 'note', 'none'] as const;
export type Severity = (typeof SEVERITIES)[number];

export const SEVERITY_LABEL: Record<string, string> = {
  critical: 'Critical', high: 'High', error: 'Error', medium: 'Medium', warning: 'Warning', low: 'Low', note: 'Note', none: 'None',
};

/** For color only: how the scanner itself ranked it. */
export function severityTone(s: string): 'bad' | 'warn' | 'dim' {
  if (s === 'critical' || s === 'high' || s === 'error') return 'bad';
  if (s === 'medium' || s === 'warning') return 'warn';
  return 'dim';
}

export interface RuleGroup {
  /** Tool and rule id: rules of different scanners can share an id. */
  key: string;
  tool: string;
  ruleId: string;
  title: string;
  description: string;
  helpUri: string;
  items: Finding[];
}

export interface SeverityGroup {
  severity: string;
  count: number;
  rules: RuleGroup[];
}

const match = (f: Finding, words: string[]) => {
  if (!words.length) return true;
  const text = `${f.tool} ${f.ruleId} ${f.rule} ${f.message} ${f.path}`.toLowerCase();
  return words.every((w) => text.includes(w));
};

/** Findings by severity (most severe first), then by rule (most found first); `filter` matches every word. */
export function group(findings: Finding[], filter = ''): SeverityGroup[] {
  const words = filter.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const bySeverity = new Map<string, Map<string, RuleGroup>>();
  for (const f of findings) {
    if (!match(f, words)) continue;
    let rules = bySeverity.get(f.severity);
    if (!rules) bySeverity.set(f.severity, (rules = new Map()));
    const key = `${f.tool}\n${f.ruleId}`;
    let g = rules.get(key);
    if (!g) {
      rules.set(key, (g = { key, tool: f.tool, ruleId: f.ruleId, title: f.rule || f.ruleId, description: f.description, helpUri: f.helpUri, items: [] }));
    }
    g.items.push(f);
  }
  const rank = (s: string) => {
    const i = (SEVERITIES as readonly string[]).indexOf(s);
    return i < 0 ? SEVERITIES.length : i;
  };
  return [...bySeverity.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([severity, rules]) => {
      const list = [...rules.values()].sort((a, b) => b.items.length - a.items.length || a.title.localeCompare(b.title));
      for (const r of list) r.items.sort((a, b) => a.path.localeCompare(b.path) || a.line - b.line);
      return { severity, count: list.reduce((n, r) => n + r.items.length, 0), rules: list };
    });
}

/** A finding's place for display: relative to the root when inside it, with its line. */
export function where(f: Finding, root: string): string {
  if (!f.path) return '';
  const p = root && f.path.startsWith(root + '/') ? f.path.slice(root.length + 1) : f.path;
  return f.line ? `${p}:${f.line}` : p;
}

/** Scans that write findings: reloading after them shows the new results. */
const SCANS = /(^|[\s(;&|])(trivy|semgrep|checkov|gitleaks|grype)\s/;

class Findings {
  report = $state.raw<Report | null>(null);
  loading = $state(false);
  error = $state('');
  #folder = '';
  #seq = 0;
  #watching = false;

  /** Reads the reports for a folder's repository. */
  async load(folder: string) {
    this.#watch();
    this.#folder = folder;
    const seq = ++this.#seq;
    this.loading = true;
    try {
      const r = await Service.Load(folder);
      if (seq !== this.#seq) return;
      this.report = { ...r, sources: r.sources ?? [], findings: r.findings ?? [] };
      this.error = '';
    } catch (e) {
      if (seq !== this.#seq) return;
      this.report = null;
      this.error = e instanceof Error ? e.message : String(e);
    } finally {
      if (seq === this.#seq) this.loading = false;
    }
  }

  /** Whether a folder is outside what's loaded, so the tab should read again. */
  stale(folder: string): boolean {
    const root = this.report?.root;
    if (!root) return folder !== this.#folder;
    return folder !== root && !folder.startsWith(root + '/');
  }

  reload() {
    if (this.#folder) return this.load(this.#folder);
  }

  #watch() {
    if (this.#watching) return;
    this.#watching = true;
    onCommandFinished((command) => {
      if (this.#folder && SCANS.test(command)) this.reload();
    });
  }
}

export const findings = new Findings();
