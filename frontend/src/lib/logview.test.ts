import { describe, expect, it, vi } from 'vitest';
import { Terminal as Headless } from '@xterm/headless';
import type { IDecorationOptions, ILinkProvider, Terminal } from '@xterm/xterm';
import { LogView, logColors, readLogicalLine, tokenAt, type LogColors } from './logview';
import type { Theme } from './settings.svelte';

const COLORS: LogColors = { error: '#ff0000', warn: '#ffff00', info: '#00ff00', timestamp: '#888888', ip: '#ff00ff', k8s: '#00ffff', url: '#0000ff' };

/** A headless terminal with the decoration and link calls the browser one has, recorded. */
function setup(cols = 40) {
  const term = new Headless({ cols, rows: 6, allowProposedApi: true, scrollback: 1000 });
  const decorations: (IDecorationOptions & { disposed: boolean })[] = [];
  const providers: ILinkProvider[] = [];
  const t = term as unknown as Terminal;
  t.registerDecoration = (opts) => {
    const d = { ...opts, disposed: false };
    decorations.push(d);
    return { dispose: () => (d.disposed = true) } as never;
  };
  t.registerLinkProvider = (p) => {
    providers.push(p);
    return { dispose() {} };
  };
  const write = (s: string) => new Promise<void>((resolve) => term.write(s, resolve));
  const live = () => decorations.filter((d) => !d.disposed).map((d) => [d.marker.line, d.x, d.width, d.foregroundColor]);
  return { term, t, decorations, providers, write, live };
}

function view(t: Terminal, onActivate = vi.fn(), resolveFile = async (p: string) => (p === 'main.go' ? '/src/main.go' : null)) {
  return new LogView(t, { colors: COLORS, onActivate, resolveFile, isLinkClick: (e) => e.metaKey });
}

describe('readLogicalLine', () => {
  it('joins soft-wrapped rows and knows where each character is', async () => {
    const { term, write } = setup(10);
    await write('0123456789ABCDE\r\nnext');
    const line = readLogicalLine(term.buffer.active as never, 1)!;
    expect(line.text.trimEnd()).toBe('0123456789ABCDE');
    expect([line.first, line.last]).toEqual([0, 1]);
    expect([line.rows[12], line.cols[12]]).toEqual([1, 2]);
  });

  it('marks text the program colored', async () => {
    const { term, write } = setup();
    await write('plain \x1b[31mred\x1b[0m');
    const line = readLogicalLine(term.buffer.active as never, 0)!;
    expect(line.plain[0]).toBe(true);
    expect(line.plain[6]).toBe(false);
  });

  it('keeps wide characters in their cells', async () => {
    const { term, write } = setup();
    await write('日本 ERROR');
    const line = readLogicalLine(term.buffer.active as never, 0)!;
    const i = line.text.indexOf('ERROR');
    expect(line.cols[i]).toBe(5);
  });
});

describe('tokenAt', () => {
  it('finds the token under a cell', async () => {
    const { term, write } = setup();
    await write('x pod/web-1 y');
    expect(tokenAt(term.buffer.active as never, 0, 4)?.token).toMatchObject({ kind: 'k8s', name: 'web-1' });
    expect(tokenAt(term.buffer.active as never, 0, 0)).toBeNull();
  });
});

describe('LogView', () => {
  it('colors what it finds on screen, but not what the program colored', async () => {
    const { t, write, live } = setup();
    const v = view(t);
    await write('12:00:01 ERROR from 10.0.0.1\r\n\x1b[33mWARN\x1b[0m pod/a\r\n');
    v.paint();
    expect(live()).toEqual([
      [0, 0, 8, '#888888'],
      [0, 9, 5, '#ff0000'],
      [0, 20, 8, '#ff00ff'],
      [1, 5, 5, '#00ffff'],
    ]);
    v.dispose();
  });

  it('keeps decorations that are still right and replaces changed ones', async () => {
    const { t, write, decorations, live } = setup();
    const v = view(t);
    await write('ERROR one\r\n');
    v.paint();
    const first = decorations.length;
    v.paint();
    expect(decorations.length).toBe(first); // nothing redone
    await write('\x1b[1;1HWARN \r\n');
    v.paint();
    expect(live()).toEqual([[0, 0, 4, '#ffff00']]);
    v.dispose();
  });

  it('splits a token that wraps into one decoration per row', async () => {
    const { t, write, live } = setup(10);
    const v = view(t);
    await write('abcdefg 2026-10-09T10:00:00Z');
    v.paint();
    expect(live()).toEqual([
      [0, 8, 2, '#888888'],
      [1, 0, 10, '#888888'],
      [2, 0, 8, '#888888'],
    ]);
    v.dispose();
  });

  it('leaves full-screen programs alone and can be switched off', async () => {
    const { t, write, live } = setup();
    const v = view(t);
    await write('ERROR\r\n');
    v.paint();
    expect(live()).toHaveLength(1);
    v.highlight = false;
    v.paint();
    expect(live()).toHaveLength(0);
    v.highlight = true;
    await write('\x1b[?1049hERROR in vim');
    v.paint();
    expect(live()).toHaveLength(0);
    v.dispose();
  });

  it('links file:line only for files that exist here, and acts on a ⌘-click', async () => {
    const { t, write, providers } = setup();
    const onActivate = vi.fn();
    const v = view(t, onActivate);
    await write('main.go:12 other.go:3 at 10.1.1.1');
    const links = await new Promise<Parameters<Parameters<ILinkProvider['provideLinks']>[1]>[0]>((resolve) =>
      providers[0].provideLinks(1, resolve),
    );
    expect(links?.map((l) => l.text)).toEqual(['main.go:12', '10.1.1.1']);
    links![0].activate({ metaKey: false } as MouseEvent, '');
    expect(onActivate).not.toHaveBeenCalled();
    links![0].activate({ metaKey: true } as MouseEvent, '');
    expect(onActivate).toHaveBeenCalledWith(expect.objectContaining({ kind: 'path', path: '/src/main.go', line: 12 }), expect.anything());
    v.dispose();
  });
});

describe('logColors', () => {
  it("takes the theme's palette, in the six-digit form decorations need", () => {
    const palette = ['#000000', '#ff5555', '#50fa7b', '#f1fa8c', '#bd93f9', '#ff79c6', '#8be9fd', '#ffffff'];
    const c = logColors({ palette, foreground: '#ffffff', background: '#000000' } as Theme);
    expect(c.error).toBe('#ff5555');
    expect(c.warn).toBe('#f1fa8c');
    expect(c.timestamp).toMatch(/^#[0-9a-f]{6}$/);
  });
});
