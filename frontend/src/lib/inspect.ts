import type { MenuEntry } from './overlay.svelte';

// The token and certificate inspector: what a selection, or the text under
// the mouse, can be opened as — a JWT's claims, base64 decoded, a PEM
// certificate, a TLS server's certificate chain. Pure functions; the
// dialogs (JwtViewer, DecodedViewer, CertViewer) show the results.
// Nothing is verified or judged: dates, names and decoded bytes only.

/** A JWT anywhere in text (as Hide Secrets finds them). */
export const JWT_RE = /\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*/;

export interface JwtInfo {
  header: Record<string, unknown>;
  claims: Record<string, unknown>;
  /** The registered time claims that are numbers, as dates. */
  times: { claim: 'exp' | 'nbf' | 'iat' | 'auth_time'; date: Date }[];
  alg: string;
  /** Has a signature part (not checked: wtf has no key). */
  signed: boolean;
}

/** The bytes of base64 or base64url text, or null when it isn't. */
export function base64Bytes(text: string): Uint8Array | null {
  let s = text.replace(/\s+/g, '');
  if (s.length < 2 || !/^[A-Za-z0-9+/_-]+={0,2}$/.test(s)) return null;
  s = s.replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '');
  if (s.length % 4 === 1) return null;
  s += '='.repeat((4 - (s.length % 4)) % 4);
  try {
    const bin = atob(s);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  } catch {
    return null;
  }
}

/** UTF-8 text, or null for binary data. */
export function utf8(bytes: Uint8Array): string | null {
  try {
    const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    // Control characters other than whitespace mean binary.
    return /[\x00-\x08\x0e-\x1f\x7f]/.test(text) ? null : text;
  } catch {
    return null;
  }
}

function jsonObject(part: string): Record<string, unknown> | null {
  const bytes = base64Bytes(part);
  const text = bytes && utf8(bytes);
  if (!text) return null;
  try {
    const v: unknown = JSON.parse(text);
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

export function decodeJwt(token: string): JwtInfo | null {
  const parts = token.trim().split('.');
  if (parts.length !== 3) return null;
  const header = jsonObject(parts[0]);
  const claims = jsonObject(parts[1]);
  if (!header || !claims) return null;
  const times: JwtInfo['times'] = [];
  for (const claim of ['exp', 'nbf', 'iat', 'auth_time'] as const) {
    const v = claims[claim];
    if (typeof v === 'number' && Number.isFinite(v)) times.push({ claim, date: new Date(v * 1000) });
  }
  return { header, claims, times, alg: typeof header.alg === 'string' ? header.alg : '', signed: parts[2].length > 0 };
}

export type DecodedKind = 'certificate' | 'jwt' | 'json' | 'text' | 'binary';

export interface Decoded {
  kind: DecodedKind;
  size: number;
  /** The text, pretty-printed for JSON; a hex dump of the start for binary data. */
  text: string;
}

const HEX_PREVIEW = 256;

export function decodeBase64(input: string): Decoded | null {
  const bytes = base64Bytes(input);
  if (!bytes || !bytes.length) return null;
  const text = utf8(bytes);
  if (text === null) return { kind: 'binary', size: bytes.length, text: hexDump(bytes.subarray(0, HEX_PREVIEW)) };
  const trimmed = text.trim();
  if (/-----BEGIN (?:TRUSTED )?CERTIFICATE-----/.test(trimmed)) return { kind: 'certificate', size: bytes.length, text };
  if (JWT_RE.test(trimmed) && decodeJwt(trimmed.match(JWT_RE)![0])) return { kind: 'jwt', size: bytes.length, text: trimmed };
  if (/^[[{]/.test(trimmed)) {
    try {
      return { kind: 'json', size: bytes.length, text: JSON.stringify(JSON.parse(trimmed), null, 2) };
    } catch {
      // Not JSON after all.
    }
  }
  return { kind: 'text', size: bytes.length, text };
}

/** "00000000  48 65 6c 6c 6f  |Hello|", 16 bytes a row. */
export function hexDump(bytes: Uint8Array): string {
  const rows: string[] = [];
  for (let i = 0; i < bytes.length; i += 16) {
    const row = bytes.subarray(i, i + 16);
    const hex = Array.from(row, (b) => b.toString(16).padStart(2, '0')).join(' ');
    const ascii = Array.from(row, (b) => (b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : '.')).join('');
    rows.push(`${i.toString(16).padStart(8, '0')}  ${hex.padEnd(47)}  |${ascii}|`);
  }
  return rows.join('\n');
}

/** Selected text that is base64 worth decoding: long enough, and not a plain word. */
export function looksBase64(text: string): boolean {
  const s = text.replace(/\s+/g, '');
  if (s.length < 8 || !/^[A-Za-z0-9+/_-]+={0,2}$/.test(s)) return false;
  // A word or a number decodes to garbage; real data mixes cases or pads.
  if (!(/[A-Z]/.test(s) && /[a-z]/.test(s)) && !/=$/.test(s)) return false;
  return decodeBase64(s) !== null;
}

/**
 * The base64 value of a YAML `key: value` line (kubectl get secret -o yaml)
 * when `index` is on it.
 */
export function yamlBase64At(line: string, index: number): string | null {
  const m = /^(\s*[\w.-]+:\s+)([A-Za-z0-9+/]+={0,2})\s*$/.exec(line);
  if (!m) return null;
  const start = m[1].length;
  if (index < start || index >= start + m[2].length) return null;
  return looksBase64(m[2]) ? m[2] : null;
}

export const hasPemCertificate = (text: string) => /-----BEGIN (?:TRUSTED )?CERTIFICATE-----/.test(text);

/**
 * A selection that names a TLS server: https://host[:port]/…, host:port, or a
 * host name with a dot. Returns what to connect to, or null.
 */
export function tlsTarget(text: string): string | null {
  const s = text.trim();
  if (!s || s.length > 300 || /\s/.test(s)) return null;
  const url = /^(?:https|wss|ldaps):\/\/[^/\s?#]+/i.exec(s);
  if (url) return url[0];
  // Top-level domains are letters (or IDNA's xn--…), which leaves out JWTs and versions.
  const m = /^((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:[a-z]{2,63}|xn--[a-z0-9-]+)|\d{1,3}(?:\.\d{1,3}){3})(?::(\d{1,5}))?\/?$/i.exec(s);
  if (!m) return null;
  // A file name (main.go, app.ts) isn't a host; a dotted name with a port or a known TLD is.
  if (!m[2] && !/^\d/.test(m[1]) && /\.(?:go|ts|js|py|rb|rs|md|txt|json|ya?ml|toml|lock|sh|log|conf|cfg|ini|pem|crt|key)$/i.test(m[1])) return null;
  return m[2] ? `${m[1]}:${m[2]}` : m[1];
}

export interface InspectActions {
  inspectJwt(token: string): void;
  decodeBase64(text: string): void;
  inspectCertText(text: string): void;
  inspectTls(target: string): void;
}

/**
 * What a selection can be inspected as, for the terminal's right-click menu.
 * Without `secrets` (Hide Secrets is on), tokens and encoded values aren't
 * offered: decoding them would show what is being hidden.
 */
export function selectionEntries(selection: string, a: InspectActions, secrets = true): MenuEntry[] {
  const out: MenuEntry[] = [];
  if (!selection.trim()) return out;
  if (hasPemCertificate(selection)) out.push({ label: 'Inspect Certificate…', action: () => a.inspectCertText(selection) });
  const jwt = secrets ? JWT_RE.exec(selection)?.[0] : undefined;
  if (jwt && decodeJwt(jwt)) out.push({ label: 'Inspect JWT…', action: () => a.inspectJwt(jwt) });
  else if (secrets && !out.length && looksBase64(selection)) out.push({ label: 'Decode Base64…', action: () => a.decodeBase64(selection) });
  const target = out.length || JWT_RE.test(selection) ? null : tlsTarget(selection);
  if (target) out.push({ label: `Inspect TLS Certificate of ${target.replace(/^\w+:\/\//, '')}…`, action: () => a.inspectTls(target) });
  return out;
}

/** Certificate files, for the file tree's menu. */
export const isCertFile = (path: string) => /\.(?:pem|crt|cer|cert|der|ca-bundle)$/i.test(path);
