import { describe, expect, it } from 'vitest';
import type { Theme } from './settings.svelte';
import { accentFor, luminance, parseHex, xtermTheme } from './themes';

const theme = (over: Partial<Theme>): Theme => ({
  name: 't', background: '#1a1b26', foreground: '#c0caf5', cursor: '#c0caf5', cursorText: '#1a1b26',
  selectionBackground: '#33467c', selectionForeground: '', dark: true, builtin: true,
  palette: ['#15161e', '#f7768e', '#9ece6a', '#e0af68', '#7aa2f7', '#bb9af7', '#7dcfff', '#a9b1d6',
    '#414868', '#f7768e', '#9ece6a', '#e0af68', '#7aa2f7', '#bb9af7', '#7dcfff', '#c0caf5'],
  ...over,
});

describe('themes', () => {
  it('parses hex colors, with or without alpha', () => {
    expect(parseHex('#ff8000')).toEqual([255, 128, 0]);
    expect(parseHex('#ff800080')).toEqual([255, 128, 0]);
    expect(parseHex('rgba(1,2,3,0.5)')).toBeNull();
  });

  it('computes luminance', () => {
    expect(luminance([0, 0, 0])).toBe(0);
    expect(luminance([255, 255, 255])).toBeCloseTo(1);
  });

  it('uses a colorful cursor as the accent', () => {
    expect(accentFor(theme({ cursor: '#ff79c6' }))).toBe('#ff79c6');
  });

  it('falls back to the theme blue when the cursor is grey', () => {
    // Tokyo Night's cursor is its (greyish) foreground.
    expect(accentFor(theme({}))).toBe('#7aa2f7');
  });

  it('skips colors that would be unreadable on the background', () => {
    const light = theme({
      background: '#fdf6e3', dark: false, cursor: '#fdf6e3',
      palette: Array(16).fill('#fdf6e3').map((c, i) => (i === 4 ? '#268bd2' : c)),
    });
    expect(accentFor(light)).toBe('#268bd2');
  });

  it('maps the palette onto xterm colors', () => {
    const x = xtermTheme(theme({}));
    expect(x.red).toBe('#f7768e');
    expect(x.brightWhite).toBe('#c0caf5');
    expect(x.selectionForeground).toBeUndefined();
  });
});
