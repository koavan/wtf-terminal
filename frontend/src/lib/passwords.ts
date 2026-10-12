import { Clipboard } from '@wailsio/runtime';

// Random passwords for the Generate tab. Made here from the system's
// cryptographic random source (crypto.getRandomValues), never sent to the
// backend, never stored or logged.

export interface PasswordOptions {
  length: number;
  lower: boolean;
  upper: boolean;
  digits: boolean;
  symbols: boolean;
  /** Leave out characters easy to misread: I l 1 O 0 o. */
  avoidAmbiguous: boolean;
}

export const MIN_LENGTH = 4;
export const MAX_LENGTH = 128;

export const DEFAULT_OPTIONS: PasswordOptions = {
  length: 24,
  lower: true,
  upper: true,
  digits: true,
  symbols: true,
  avoidAmbiguous: false,
};

const SETS = {
  lower: 'abcdefghijklmnopqrstuvwxyz',
  upper: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
  digits: '0123456789',
  // No quotes, backslash, backtick or space: pastes into shells, URLs and config files without escaping.
  symbols: '!#$%&()*+,-./:;<=>?@[]^_{|}~',
} as const;

const AMBIGUOUS = /[Il1O0o|]/g;

/** The characters each chosen class contributes. */
export function classes(o: PasswordOptions): string[] {
  return (['lower', 'upper', 'digits', 'symbols'] as const)
    .filter((k) => o[k])
    .map((k) => (o.avoidAmbiguous ? SETS[k].replace(AMBIGUOUS, '') : SETS[k]));
}

/** A uniform random integer in [0, n), without modulo bias. */
export function randomInt(n: number): number {
  const limit = Math.floor(0x1_0000_0000 / n) * n;
  const buf = new Uint32Array(1);
  for (;;) {
    crypto.getRandomValues(buf);
    if (buf[0] < limit) return buf[0] % n;
  }
}

/**
 * A password with at least one character of each chosen class, the rest
 * drawn from all of them, then shuffled. '' when no class is chosen.
 */
export function generatePassword(o: PasswordOptions, rand: (n: number) => number = randomInt): string {
  const sets = classes(o);
  if (!sets.length) return '';
  const length = Math.min(MAX_LENGTH, Math.max(MIN_LENGTH, Math.round(o.length)));
  const pool = sets.join('');
  const chars = sets.map((s) => s[rand(s.length)]);
  while (chars.length < length) chars.push(pool[rand(pool.length)]);
  for (let i = chars.length - 1; i > 0; i--) {
    const j = rand(i + 1);
    [chars[i], chars[j]] = [chars[j], chars[i]];
  }
  return chars.join('');
}

/** Roughly how many bits of randomness: length × log2(characters to choose from). */
export function entropyBits(o: PasswordOptions): number {
  const pool = classes(o).join('').length;
  return pool ? Math.floor(Math.min(MAX_LENGTH, Math.max(MIN_LENGTH, o.length)) * Math.log2(pool)) : 0;
}

export const CLIPBOARD_CLEAR_MS = 30_000;

/**
 * Copies a secret and empties the clipboard after 30 seconds, unless
 * something else was copied meanwhile.
 */
export async function copySecret(text: string) {
  await Clipboard.SetText(text);
  setTimeout(async () => {
    try {
      if ((await Clipboard.Text()) === text) await Clipboard.SetText('');
    } catch {
      // the clipboard is busy or gone; nothing to clean
    }
  }, CLIPBOARD_CLEAR_MS);
}
