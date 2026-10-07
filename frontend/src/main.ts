import '@fontsource-variable/jetbrains-mono';
import '@fontsource-variable/inter';
import '@xterm/xterm/css/xterm.css';
import './app.css';

import { mount } from 'svelte';
import App from './App.svelte';
import { loadHome } from './lib/backend';
import { isMac } from './lib/platform';
import { settings } from './lib/settings.svelte';
import { initWindow } from './lib/window';

if (!isMac) document.documentElement.classList.add('no-vibrancy');

// xterm.js measures the cell size when a terminal opens, so the font has to
// be ready first or every tab ends up with fallback-font metrics.
try {
  await Promise.all([
    document.fonts.load('13px "JetBrains Mono Variable"'),
    document.fonts.load('600 13px "JetBrains Mono Variable"'),
    loadHome(),
    settings.load(),
    initWindow(),
  ]);
  await document.fonts.load(`13px "${settings.config.fontFamily.replace(/"/g, '')}"`).catch(() => {});
  mount(App, { target: document.getElementById('app')! });
} catch (err) {
  // A visible error beats an empty window.
  const pre = document.createElement('pre');
  pre.className = 'startup-error';
  pre.textContent = `wtf couldn't start:\n\n${err instanceof Error ? (err.stack ?? err.message) : String(err)}`;
  document.body.append(pre);
  throw err;
}
