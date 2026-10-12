import { describe, expect, it, vi } from 'vitest';

vi.mock('@wailsio/runtime', () => ({ Clipboard: { SetText: vi.fn(), Text: vi.fn() } }));

import { Clipboard } from '@wailsio/runtime';
import { classes, copySecret, DEFAULT_OPTIONS, entropyBits, generatePassword, randomInt, type PasswordOptions } from './passwords';

const opts = (o: Partial<PasswordOptions>): PasswordOptions => ({ ...DEFAULT_OPTIONS, ...o });

describe('generatePassword', () => {
  it('has the length asked for and a character of every chosen class', () => {
    for (let i = 0; i < 200; i++) {
      const p = generatePassword(opts({ length: 8 }));
      expect(p).toHaveLength(8);
      expect(p).toMatch(/[a-z]/);
      expect(p).toMatch(/[A-Z]/);
      expect(p).toMatch(/[0-9]/);
      expect(p).toMatch(/[^A-Za-z0-9]/);
    }
  });

  it('uses only the chosen classes', () => {
    expect(generatePassword(opts({ upper: false, symbols: false, lower: false, length: 30 }))).toMatch(/^[0-9]{30}$/);
    const p = generatePassword(opts({ length: 128, avoidAmbiguous: true }));
    expect(p).not.toMatch(/[Il1O0o|]/);
  });

  it('never uses quotes, backslash, backtick or space', () => {
    expect(classes(DEFAULT_OPTIONS).join('')).not.toMatch(/['"\\` ]/);
  });

  it('is empty with no class, and clamps the length', () => {
    expect(generatePassword(opts({ lower: false, upper: false, digits: false, symbols: false }))).toBe('');
    expect(generatePassword(opts({ length: 1 }))).toHaveLength(4);
    expect(generatePassword(opts({ length: 1000 }))).toHaveLength(128);
  });

  it('draws evenly', () => {
    const counts = new Array(10).fill(0);
    for (let i = 0; i < 20000; i++) counts[randomInt(10)]++;
    for (const c of counts) expect(c).toBeGreaterThan(1700);
  });
});

describe('entropyBits', () => {
  it('is length × log2(pool)', () => {
    expect(entropyBits(opts({ length: 10, lower: false, upper: false, symbols: false }))).toBe(33); // 10 × log2(10)
    expect(entropyBits(opts({ lower: false, upper: false, digits: false, symbols: false }))).toBe(0);
  });
});

describe('copySecret', () => {
  it('clears the clipboard after 30 seconds only if it still holds the secret', async () => {
    vi.useFakeTimers();
    vi.mocked(Clipboard.Text).mockResolvedValueOnce('s3cret').mockResolvedValueOnce('other');
    await copySecret('s3cret');
    await vi.advanceTimersByTimeAsync(30_000);
    expect(Clipboard.SetText).toHaveBeenLastCalledWith('');
    vi.mocked(Clipboard.SetText).mockClear();
    await copySecret('s3cret');
    await vi.advanceTimersByTimeAsync(30_000);
    expect(Clipboard.SetText).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});
