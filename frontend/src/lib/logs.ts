// Finds the parts of a line of terminal output worth a color or a click:
// log levels, timestamps, URLs, IP addresses, file:line references and
// Kubernetes resources (pod/web-1, deployment.apps/api). Pure functions —
// lib/logview applies them to what's on screen.

export type LogLevel = 'error' | 'warn' | 'info' | 'debug';

export type LogToken =
  | { kind: 'level'; start: number; end: number; text: string; level: LogLevel }
  | { kind: 'timestamp'; start: number; end: number; text: string }
  /** A time of day with no date: colored, but there's nothing to convert. */
  | { kind: 'time'; start: number; end: number; text: string }
  | { kind: 'url'; start: number; end: number; text: string }
  | { kind: 'ip'; start: number; end: number; text: string; address: string; port?: number; cidr?: boolean }
  | { kind: 'path'; start: number; end: number; text: string; path: string; line: number; col?: number }
  | { kind: 'k8s'; start: number; end: number; text: string; resource: string; name: string };

export type TokenKind = LogToken['kind'];

/** Longer lines aren't scanned (minified files, base64 blobs). */
export const MAX_SCAN = 4000;

const MONTH = '(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)';
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

const URL_RE = /\b(?:https?|ftp|file):\/\/[^\s<>"'`]+/g;

const TIMESTAMP_RES = [
  // 2026-10-09T12:34:56.789Z, 2026-10-09 12:34:56,123 +02:00
  /(?<![\w.])\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2}(?:[.,]\d{1,9})?)?(?: ?(?:Z|[+-]\d{2}:?\d{2})(?![\w:]))?/g,
  // 09/Oct/2026:13:55:36 -0700 (web server access logs)
  new RegExp(`(?<![\\w/])\\d{2}/${MONTH}/\\d{4}:\\d{2}:\\d{2}:\\d{2}(?: [+-]\\d{4})?`, 'g'),
  // Oct  9 12:34:56 (syslog)
  new RegExp(`(?<![\\w])${MONTH} [ \\d]\\d \\d{2}:\\d{2}:\\d{2}(?![\\w:])`, 'g'),
];

const TIME_RE = /(?<![\w:.])\d{2}:\d{2}:\d{2}(?:[.,]\d{1,9})?(?![\w:])/g;

const OCTET = '(?:25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)';
const IPV4_RE = new RegExp(`(?<![\\w.])((?:${OCTET}\\.){3}${OCTET})(?:(/\\d{1,2})|:(\\d{1,5}))?(?![\\w]|\\.\\d)`, 'g');
const HEX = '[0-9a-fA-F]{1,4}';
// Only the forms that can't be a time or a MAC address: compressed (::) or all eight groups.
const IPV6_RE = new RegExp(
  `(?<![\\w:])((?:${HEX}(?::${HEX})*)?::(?:${HEX}(?::${HEX})*)?|(?:${HEX}:){7}${HEX})(/\\d{1,3})?(?![\\w:])`,
  'g',
);

// foo.go:12, ./src/app.ts:3:14, /var/app/main.py:88 — a file name with an extension, then a line.
const PATH_RE = /(?<![\w/.~@+-])((?:~|\.{1,2})?\/?(?:[\w.@+-]+\/)*[\w@+-][\w.@+-]*\.[A-Za-z]\w{0,9}):(\d+)(?::(\d+))?(?![\w.]|:\d)/g;
// Python tracebacks: File "app/main.py", line 12
const PY_PATH_RE = /File "([^"]+)", line (\d+)/g;

/** Kinds as kubectl prints them in output ("pod/web-1", "deployment.apps/api"). */
const K8S_KINDS = [
  'pods?', 'deployments?', 'deploy', 'statefulsets?', 'sts', 'daemonsets?', 'replicasets?', 'jobs?', 'cronjobs?',
  'services?', 'svc', 'configmaps?', 'secrets?', 'ingress(?:es)?', 'nodes?', 'namespaces?',
  'persistentvolumeclaims?', 'pvc', 'persistentvolumes?', 'serviceaccounts?', 'horizontalpodautoscalers?', 'hpa',
  'networkpolic(?:y|ies)', 'endpoints', 'endpointslices?', 'roles?', 'rolebindings?', 'clusterroles?',
  'clusterrolebindings?', 'poddisruptionbudgets?', 'pdb', 'storageclass(?:es)?', 'customresourcedefinitions?', 'crd',
];
const K8S_RE = new RegExp(
  `(?<![\\w./-])((?:${K8S_KINDS.join('|')})(?:\\.[a-z0-9-]+(?:\\.[a-z0-9-]+)*)?)/([a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?)(?![\\w/-])`,
  'g',
);

const LEVEL_WORDS: Record<string, LogLevel> = {
  FATAL: 'error', PANIC: 'error', CRITICAL: 'error', CRIT: 'error', EMERG: 'error', ALERT: 'error',
  ERROR: 'error', ERR: 'error', SEVERE: 'error',
  WARNING: 'warn', WARN: 'warn',
  INFO: 'info', NOTICE: 'info',
  DEBUG: 'debug', TRACE: 'debug',
};
const LEVEL_ALT = Object.keys(LEVEL_WORDS).join('|');
// Upper case on its own (ERROR, [WARN]); any case after level= / "level": or in [brackets].
const LEVEL_RES = [
  new RegExp(`(?<![\\w-])(${LEVEL_ALT})(?![\\w-])`, 'g'),
  new RegExp(`(?:\\b(?:level|lvl|severity)["']?\\s*[=:]\\s*["']?|\\[)(${LEVEL_ALT})(?![\\w-])`, 'gi'),
];

interface Hit {
  start: number;
  end: number;
}

/** Trailing punctuation that ends a sentence rather than the URL — unless it closes a bracket the URL opened. */
function trimUrl(url: string): string {
  for (;;) {
    const last = url.at(-1);
    if (!last || !/[.,;:!?'")\]}>]/.test(last)) return url;
    const open = { ')': '(', ']': '[', '}': '{' }[last as ')' | ']' | '}'];
    if (open && url.split(open).length > url.split(last).length - 1) return url;
    url = url.slice(0, -1);
  }
}

/** The canonical kind for commands: "deploy" and "deployments.apps" are a deployment. */
export function k8sKind(resource: string): string {
  const base = resource.split('.')[0];
  const short: Record<string, string> = { deploy: 'deployment', sts: 'statefulset', svc: 'service', pvc: 'persistentvolumeclaim', hpa: 'horizontalpodautoscaler', pdb: 'poddisruptionbudget', crd: 'customresourcedefinition' };
  if (short[base]) return short[base];
  if (base === 'endpoints') return base;
  if (base.endsWith('ies')) return base.slice(0, -3) + 'y';
  if (base.endsWith('sses') || base.endsWith('xes')) return base.slice(0, -2);
  if (base.endsWith('s') && !base.endsWith('ss')) return base.slice(0, -1);
  return base;
}

/** Everything worth a color or a click in one line, in order and never overlapping. */
export function scanLine(text: string): LogToken[] {
  if (!text || text.length > MAX_SCAN) return [];
  const taken: Hit[] = [];
  const out: LogToken[] = [];
  const free = (start: number, end: number) => taken.every((h) => end <= h.start || start >= h.end);
  const add = (t: LogToken) => {
    if (t.end <= t.start || !free(t.start, t.end)) return;
    taken.push(t);
    out.push(t);
  };
  const each = (re: RegExp, fn: (m: RegExpExecArray) => void) => {
    re.lastIndex = 0;
    for (let m = re.exec(text); m; m = re.exec(text)) fn(m);
  };

  // First come first served: a URL's host isn't also an IP, a timestamp's time isn't a time.
  each(URL_RE, (m) => {
    const url = trimUrl(m[0]);
    add({ kind: 'url', start: m.index, end: m.index + url.length, text: url });
  });
  for (const re of TIMESTAMP_RES) each(re, (m) => add({ kind: 'timestamp', start: m.index, end: m.index + m[0].length, text: m[0] }));
  each(PY_PATH_RE, (m) => {
    const start = m.index + m[0].indexOf('"') + 1;
    add({ kind: 'path', start, end: start + m[1].length, text: m[1], path: m[1], line: Number(m[2]) });
  });
  each(PATH_RE, (m) =>
    add({
      kind: 'path', start: m.index, end: m.index + m[0].length, text: m[0], path: m[1], line: Number(m[2]),
      ...(m[3] ? { col: Number(m[3]) } : {}),
    }),
  );
  each(K8S_RE, (m) =>
    add({ kind: 'k8s', start: m.index, end: m.index + m[0].length, text: m[0], resource: m[1], name: m[2] }),
  );
  each(IPV4_RE, (m) => {
    const port = m[3] ? Number(m[3]) : undefined;
    if (port !== undefined && (port < 1 || port > 65535)) return;
    add({
      kind: 'ip', start: m.index, end: m.index + m[0].length, text: m[0], address: m[1],
      ...(port ? { port } : {}), ...(m[2] ? { cidr: true } : {}),
    });
  });
  each(IPV6_RE, (m) => {
    if (m[1].length < 3) return; // a lone "::"
    add({ kind: 'ip', start: m.index, end: m.index + m[0].length, text: m[0], address: m[1], ...(m[2] ? { cidr: true } : {}) });
  });
  each(TIME_RE, (m) => add({ kind: 'time', start: m.index, end: m.index + m[0].length, text: m[0] }));
  for (const re of LEVEL_RES) {
    each(re, (m) => {
      const start = m.index + m[0].length - m[1].length;
      add({ kind: 'level', start, end: start + m[1].length, text: m[1], level: LEVEL_WORDS[m[1].toUpperCase()] });
    });
  }
  return out.sort((a, b) => a.start - b.start);
}

/** What a timestamp says, and whether it said its time zone. */
export interface ParsedTime {
  date: Date;
  /** False when the text has no zone and was read as local time. */
  zoned: boolean;
}

/** Reads the timestamps scanLine finds; null for anything else. `now` fills in syslog's missing year. */
export function parseTimestamp(text: string, now = new Date()): ParsedTime | null {
  let m = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})(?::(\d{2})(?:[.,](\d{1,9}))?)? ?(Z|[+-]\d{2}:?\d{2})?$/.exec(text);
  if (m) {
    const [, y, mo, d, h, mi, s = '0', frac = '0', zone] = m;
    const ms = Math.round(Number(`0.${frac}`) * 1000);
    return build(+y, +mo - 1, +d, +h, +mi, +s, ms, zone);
  }
  m = /^(\d{2})\/(\w{3})\/(\d{4}):(\d{2}):(\d{2}):(\d{2})(?: ([+-]\d{4}))?$/.exec(text);
  if (m) {
    const month = MONTHS.indexOf(m[2]);
    return month < 0 ? null : build(+m[3], month, +m[1], +m[4], +m[5], +m[6], 0, m[7]);
  }
  m = /^(\w{3}) [ \d]?(\d{1,2}) (\d{2}):(\d{2}):(\d{2})$/.exec(text);
  if (m) {
    const month = MONTHS.indexOf(m[1]);
    if (month < 0) return null;
    const t = build(now.getFullYear(), month, +m[2], +m[3], +m[4], +m[5], 0, undefined);
    // December's log read in January was last year.
    if (t && t.date.getTime() - now.getTime() > 31 * 86400_000) t.date.setFullYear(now.getFullYear() - 1);
    return t;
  }
  return null;
}

function build(y: number, mo: number, d: number, h: number, mi: number, s: number, ms: number, zone: string | undefined): ParsedTime | null {
  if (mo < 0 || mo > 11 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 60) return null;
  if (!zone) return { date: new Date(y, mo, d, h, mi, s, ms), zoned: false };
  let offset = 0;
  if (zone !== 'Z') {
    const z = /^([+-])(\d{2}):?(\d{2})$/.exec(zone)!;
    offset = (z[1] === '-' ? -1 : 1) * (Number(z[2]) * 60 + Number(z[3]));
  }
  return { date: new Date(Date.UTC(y, mo, d, h, mi, s, ms) - offset * 60_000), zoned: true };
}

/** "3 min ago", "in 2 h", "just now". */
export function relativeTime(date: Date, now = new Date()): string {
  const diff = (now.getTime() - date.getTime()) / 1000;
  const abs = Math.abs(diff);
  const unit =
    abs < 45 ? null
    : abs < 3600 ? `${Math.round(abs / 60)} min`
    : abs < 86400 ? `${Math.round(abs / 3600)} h`
    : `${Math.round(abs / 86400)} d`;
  if (!unit) return 'just now';
  return diff >= 0 ? `${unit} ago` : `in ${unit}`;
}

/** The first JSON object or array in a line (after a log prefix, say), pretty-printed; null if there's none. */
export function findJson(text: string): string | null {
  let tries = 0;
  for (let i = 0; i < text.length && tries < 20; i++) {
    const c = text[i];
    if (c !== '{' && c !== '[') continue;
    tries++;
    const end = text.lastIndexOf(c === '{' ? '}' : ']');
    if (end <= i) continue;
    try {
      const value: unknown = JSON.parse(text.slice(i, end + 1));
      if (value && typeof value === 'object') return JSON.stringify(value, null, 2);
    } catch {
      // Not JSON from here; try the next bracket.
    }
  }
  return null;
}
