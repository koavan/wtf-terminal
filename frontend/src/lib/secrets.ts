// Finds secrets in text, for Hide Secrets (screen sharing): access keys,
// tokens, JWTs, passwords in URLs and in key=value settings, private keys.
// Best effort, and on the side of hiding too much: hiding a harmless value
// costs little while sharing a screen; showing a key costs a lot.

export interface SecretSpan {
  start: number;
  end: number;
  /** What it looks like, e.g. "AWS access key". */
  kind: string;
}

/** What copied text shows instead of a secret. */
export const REDACTED = '[REDACTED]';

/** A pattern, and which group is the secret (0: all of it). */
interface Rule {
  kind: string;
  re: RegExp;
  group?: number;
  /** Extra check on the secret found. */
  accept?: (value: string) => boolean;
}

/** Upper case, lower case and digits mixed: a key or a hash, not a word. */
const random = (v: string) => /[A-Z]/.test(v) && /[a-z]/.test(v) && /\d/.test(v);
/** Encoded data rather than a name: random-looking, or mixed case with base64's = padding. */
const base64ish = (v: string) => random(v) || (/=$/.test(v) && /[A-Z]/.test(v) && /[a-z]/.test(v));

const RULES: Rule[] = [
  { kind: 'private key', re: /-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|$)/g },
  { kind: 'AWS access key', re: /\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b/g },
  {
    kind: 'AWS secret key',
    re: /\b(?:aws_?secret_?access_?key|secret_?access_?key)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})(?![A-Za-z0-9/+=])/gi,
    group: 1,
  },
  { kind: 'AWS session token', re: /\b(?:aws_?session_?token|session_?token)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{80,})/gi, group: 1 },
  { kind: 'JWT', re: /\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*/g },
  { kind: 'GitHub token', re: /\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b/g },
  { kind: 'GitLab token', re: /\bglpat-[A-Za-z0-9_-]{20,}/g },
  { kind: 'Slack token', re: /\bxox[abposr]-[A-Za-z0-9-]{10,}/g },
  { kind: 'Slack webhook', re: /https:\/\/hooks\.slack\.com\/services\/[A-Za-z0-9/]+/g },
  { kind: 'Stripe key', re: /\b[sr]k_(?:live|test)_[A-Za-z0-9]{20,}/g },
  { kind: 'Google API key', re: /\bAIza[0-9A-Za-z_-]{35}/g },
  { kind: 'API key', re: /\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}/g },
  { kind: 'npm token', re: /\bnpm_[A-Za-z0-9]{36}\b/g },
  // https://user:password@host
  { kind: 'password in URL', re: /\b[a-z][a-z0-9+.-]*:\/\/[^\s:/@]+:([^\s@/]+)@/gi, group: 1 },
  { kind: 'bearer token', re: /\b(?:bearer|token)\s+([A-Za-z0-9._~+/-]{20,}=*)/gi, group: 1 },
  // password=…, "client_secret": "…", API_KEY: …, export GITHUB_TOKEN=…
  {
    kind: 'secret setting',
    re: /\b[\w.-]*(?:passw(?:or)?d|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credentials?)["']?\s*[:=]\s*(["']?)([^\s"'&,;]{6,})\1/gi,
    group: 2,
    accept: (v) => !/^(?:true|false|null|none|nil|undefined|\*+|<[^>]*>|\$\{?\w+\}?)$/i.test(v) && !/^\d+$/.test(v),
  },
  // Base64 values in YAML, as in a secret's `data:` (kubectl get secret -o yaml).
  { kind: 'encoded value', re: /^\s+[\w.-]+:\s+([A-Za-z0-9+/]{12,}={0,2})\s*$/gm, group: 1, accept: base64ish },
];

/** The secrets in a text, in order and never overlapping. */
export function findSecrets(text: string): SecretSpan[] {
  if (!text) return [];
  const out: SecretSpan[] = [];
  for (const rule of RULES) {
    rule.re.lastIndex = 0;
    for (let m = rule.re.exec(text); m; m = rule.re.exec(text)) {
      if (m[0] === '') {
        rule.re.lastIndex++;
        continue;
      }
      const value = m[rule.group ?? 0];
      if (!value || (rule.accept && !rule.accept(value))) continue;
      const start = m.index + (rule.group ? m[0].lastIndexOf(value) : 0);
      const end = start + value.length;
      if (out.some((s) => start < s.end && end > s.start)) continue;
      out.push({ start, end, kind: rule.kind });
    }
  }
  return out.sort((a, b) => a.start - b.start);
}

/** The text with its secrets replaced, for copying while secrets are hidden. */
export function redact(text: string): string {
  const spans = findSecrets(text);
  if (!spans.length) return text;
  let out = '';
  let at = 0;
  for (const s of spans) {
    out += text.slice(at, s.start) + REDACTED;
    at = s.end;
  }
  return out + text.slice(at);
}
