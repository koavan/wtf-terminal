import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { findSecrets, redact } from './secrets';

// testdata/secrets/cases.json is shared with the native macOS app's Swift
// port of these rules (macos/Tests): both must find the same secrets.
// Regenerate it after changing the rules: node testdata/secrets/generate.ts
const cases: { text: string; found: { kind: string; start: number; end: number }[]; redacted: string }[] = JSON.parse(
  readFileSync(new URL('../../../testdata/secrets/cases.json', import.meta.url), 'utf8'),
);

describe('shared secret cases', () => {
  it.each(cases.map((c) => [c.text, c] as const))('%j', (_, c) => {
    expect(findSecrets(c.text)).toEqual(c.found);
    expect(redact(c.text)).toBe(c.redacted);
  });
});
