import { describe, expect, it } from 'vitest';
import { Terminal as Headless } from '@xterm/headless';
import type { IDecorationOptions, Terminal } from '@xterm/xterm';
import { SecretMask, coverColor } from './secretmask';
import type { Theme } from './settings.svelte';

const KEY = 'AKIAABCDEFGHIJKLMNOP';

/** A headless terminal that records decorations, like the browser one draws them. */
function setup(cols = 40, rows = 6) {
  const term = new Headless({ cols, rows, allowProposedApi: true, scrollback: 1000 });
  const decorations: (IDecorationOptions & { disposed: boolean })[] = [];
  (term as unknown as Terminal).registerDecoration = (opts) => {
    const d = { ...opts, disposed: false };
    decorations.push(d);
    return { dispose: () => (d.disposed = true), onRender: () => ({ dispose() {} }) } as never;
  };
  // The covers come in a microtask after the output is read (before the app draws a frame).
  const write = (s: string) => new Promise<void>((resolve) => term.write(s, () => setTimeout(resolve)));
  const covers = () => decorations.filter((d) => !d.disposed).map((d): number[] => [d.marker.line, d.x ?? 0, d.width ?? 0]);
  const mask = new SecretMask(term as unknown as Terminal, '#777777');
  return { term, write, covers, decorations, mask };
}

describe('SecretMask', () => {
  it('covers nothing while off', async () => {
    const { write, covers } = setup();
    await write(`id ${KEY}`);
    expect(covers()).toEqual([]);
  });

  it('covers secrets on screen as soon as output is read, above everything else', async () => {
    const { write, covers, decorations, mask } = setup();
    mask.on = true;
    await write(`id ${KEY}\r\npassword: hunter2222`);
    expect(covers()).toEqual([
      [0, 3, 20],
      [1, 10, 10],
    ]);
    expect(decorations[0]).toMatchObject({ layer: 'top', backgroundColor: '#777777', foregroundColor: '#777777' });
    mask.dispose();
  });

  it('covers a secret that wraps, row by row', async () => {
    const { write, covers, mask } = setup(12);
    mask.on = true;
    await write(`key ${KEY}`);
    expect(covers()).toEqual([
      [0, 4, 8],
      [1, 0, 12],
    ]);
    mask.dispose();
  });

  it('covers every line of a private key, even when its start is above the screen', async () => {
    const { term, write, covers, mask } = setup(40, 4);
    mask.on = true;
    await write('-----BEGIN OPENSSH PRIVATE KEY-----\r\n' + 'b3BlbnNzaC1rZXktdjEAAAAA\r\n'.repeat(5) + '-----END OPENSSH PRIVATE KEY-----\r\nafter');
    const top = term.buffer.active.viewportY;
    const onScreen = covers().map(([line]) => line - top).filter((row) => row >= 0);
    // The key's last lines and its END line are covered; "after" isn't.
    expect(onScreen).toEqual([0, 1, 2]);
    mask.dispose();
  });

  it('uncovers a line rewritten without the secret, and everything when turned off', async () => {
    const { write, covers, mask } = setup();
    mask.on = true;
    await write(`id ${KEY}`);
    expect(covers()).toHaveLength(1);
    await write('\r\x1b[2Kid gone');
    expect(covers()).toEqual([]);
    await write(`\r\nagain ${KEY}`);
    expect(covers()).toHaveLength(1);
    mask.on = false;
    expect(covers()).toEqual([]);
    mask.dispose();
  });

  it('keeps a cover on its text as more output scrolls it', async () => {
    const { write, covers, mask } = setup(40, 3);
    mask.on = true;
    await write(`id ${KEY}\r\n`);
    const before = covers();
    await write('one\r\n');
    expect(covers()).toEqual(before);
    mask.dispose();
  });
});

describe('coverColor', () => {
  it('is a solid color between text and background', () => {
    expect(coverColor({ foreground: '#ffffff', background: '#000000' } as Theme)).toBe('#666666');
  });
});
