import type { ITheme } from '@xterm/xterm';
import type { Theme } from './settings.svelte';

type RGB = [number, number, number];

export function parseHex(color: string): RGB | null {
  const m = /^#([0-9a-f]{6})([0-9a-f]{2})?$/i.exec(color.trim());
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

const toHex = ([r, g, b]: RGB) => '#' + [r, g, b].map((v) => Math.round(v).toString(16).padStart(2, '0')).join('');
const rgba = ([r, g, b]: RGB, a: number) => `rgba(${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)}, ${a})`;
const mix = (a: RGB, b: RGB, t: number): RGB => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];

/** WCAG relative luminance, 0–1. */
export function luminance([r, g, b]: RGB): number {
  const lin = (v: number) => {
    const s = v / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** How far a color is from grey, 0–1. Unlike HSL saturation, pale pastels score low. */
function chroma([r, g, b]: RGB): number {
  return (Math.max(r, g, b) - Math.min(r, g, b)) / 255;
}

function contrast(a: RGB, b: RGB): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/**
 * The UI accent for a theme: its cursor color when that's colorful and
 * readable, otherwise its blue, then magenta.
 */
export function accentFor(t: Theme): string {
  const bg = parseHex(t.background) ?? [0, 0, 0];
  for (const c of [t.cursor, t.palette[12], t.palette[4], t.palette[13], t.palette[5]]) {
    const rgb = c ? parseHex(c) : null;
    if (rgb && chroma(rgb) > 0.25 && contrast(rgb, bg) >= 2.5) return toHex(rgb);
  }
  return t.dark ? '#8d9cff' : '#4c5bd4';
}

export function xtermTheme(t: Theme): ITheme {
  const p = t.palette;
  const ink = t.dark ? '255, 255, 255' : '0, 0, 0';
  return {
    background: t.background,
    foreground: t.foreground,
    cursor: t.cursor,
    cursorAccent: t.cursorText,
    selectionBackground: t.selectionBackground,
    selectionForeground: t.selectionForeground || undefined,
    scrollbarSliderBackground: `rgba(${ink}, 0.10)`,
    scrollbarSliderHoverBackground: `rgba(${ink}, 0.18)`,
    scrollbarSliderActiveBackground: `rgba(${ink}, 0.26)`,
    black: p[0], red: p[1], green: p[2], yellow: p[3],
    blue: p[4], magenta: p[5], cyan: p[6], white: p[7],
    brightBlack: p[8], brightRed: p[9], brightGreen: p[10], brightYellow: p[11],
    brightBlue: p[12], brightMagenta: p[13], brightCyan: p[14], brightWhite: p[15],
  };
}

/**
 * Re-skins the window around a theme: the card takes the terminal's
 * background, the chrome a deeper (or lighter) shade of it, and the
 * accent comes from the theme's own colors. Everything else follows the
 * dark/light token set in app.css.
 */
export function applyChrome(t: Theme) {
  const root = document.documentElement;
  root.dataset.tone = t.dark ? 'dark' : 'light';

  const bg = parseHex(t.background);
  const accent = accentFor(t);
  const accentRgb = parseHex(accent)!;
  const vars: Record<string, string> = {
    '--surface': t.background,
    '--accent': accent,
    '--accent-soft': rgba(accentRgb, t.dark ? 0.16 : 0.12),
    '--on-accent': luminance(accentRgb) > 0.45 ? '#111216' : '#ffffff',
  };
  if (bg) {
    const chrome = t.dark ? mix(bg, [0, 0, 0], 0.28) : mix(bg, [214, 216, 222], 0.45);
    vars['--chrome'] = rgba(chrome, t.dark ? 0.6 : 0.55);
    vars['--chrome-solid'] = toHex(chrome);
    vars['--popover-solid'] = toHex(t.dark ? mix(bg, [255, 255, 255], 0.06) : mix(bg, [255, 255, 255], 0.5));
    vars['--popover'] = rgba(t.dark ? mix(bg, [255, 255, 255], 0.06) : mix(bg, [255, 255, 255], 0.5), 0.84);
  }
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v);
}
