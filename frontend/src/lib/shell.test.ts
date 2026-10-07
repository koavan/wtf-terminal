import { describe, expect, it } from 'vitest';
import { baseName, parentOf, relativeTo, shellQuote } from './shell';

describe('shellQuote', () => {
  it('leaves plain paths alone', () => {
    expect(shellQuote('src/main.go')).toBe('src/main.go');
    expect(shellQuote('/usr/local/bin')).toBe('/usr/local/bin');
  });

  it('quotes anything the shell would interpret', () => {
    expect(shellQuote('My Docs/a b.txt')).toBe(`'My Docs/a b.txt'`);
    expect(shellQuote('$HOME')).toBe(`'$HOME'`);
    expect(shellQuote('a;rm -rf x')).toBe(`'a;rm -rf x'`);
    expect(shellQuote('*.go')).toBe(`'*.go'`);
    expect(shellQuote('~/x')).toBe(`'~/x'`);
  });

  it("escapes single quotes and leading dashes", () => {
    expect(shellQuote("it's")).toBe(`'it'\\''s'`);
    expect(shellQuote('-rf')).toBe(`'-rf'`);
  });
});

describe('paths', () => {
  it('makes paths relative to a folder they are in', () => {
    expect(relativeTo('/w/proj', '/w/proj/src/a.go')).toBe('src/a.go');
    expect(relativeTo('/w/proj', '/w/proj')).toBe('.');
    expect(relativeTo('/w/proj', '/w/project/a.go')).toBe('/w/project/a.go');
    expect(relativeTo('/w/proj', '/etc/hosts')).toBe('/etc/hosts');
    expect(relativeTo('', '/etc/hosts')).toBe('/etc/hosts');
  });

  it('splits paths', () => {
    expect(parentOf('/a/b/c')).toBe('/a/b');
    expect(parentOf('/a')).toBe('/');
    expect(baseName('/a/b/c.txt')).toBe('c.txt');
    expect(baseName('/')).toBe('/');
  });
});
