// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { renderManual, plainText, type ManualContext } from './manual';

vi.mock('./backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
vi.mock('./window', () => ({ thisWindow: { name: 'main' }, Windows: {}, closeThisWindow: vi.fn(), moveTabToNewWindow: vi.fn() }));

const ctx = (isMac = true): ManualContext => ({
  isMac,
  label: (id) => ({ 'sidebar.ssh': 'Show SSH Hosts', 'tab.close': 'Close Tab' })[id] ?? null,
  shortcut: (id) => ({ 'sidebar.ssh': isMac ? '⌘⇧S' : 'Ctrl+Shift+Alt+S' })[id] ?? null,
});

describe('renderManual', () => {
  it('escapes everything that is text', () => {
    const r = renderManual('# A <b>title</b>\n\nRun `rm -rf <dir>` & <script>alert(1)</script>', ctx());
    expect(r.html).toContain('<h1>A &lt;b&gt;title&lt;/b&gt;</h1>');
    expect(r.html).toContain('<code>rm -rf &lt;dir&gt;</code> &amp; &lt;script&gt;');
    expect(r.html).not.toContain('<script>');
  });

  it('renders the subset the pages use', () => {
    const src = [
      '## Lists',
      '- one **bold**',
      '- two *italic*',
      '  continued',
      '',
      '1. first',
      '2. second',
      '',
      '| A | B |',
      '|---|---|',
      '| `x` | y |',
      '',
      '> A tip',
      '',
      '```',
      'echo <hi>',
      '```',
    ].join('\n');
    const html = renderManual(src, ctx()).html;
    expect(html).toContain('<ul><li>one <b>bold</b></li><li>two <i>italic</i> continued</li></ul>');
    expect(html).toContain('<ol><li>first</li><li>second</li></ol>');
    expect(html).toContain('<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td><code>x</code></td><td>y</td></tr></tbody></table>');
    expect(html).toContain('<aside>A tip</aside>');
    expect(html).toContain('<pre><code>echo &lt;hi&gt;</code></pre>');
  });

  it('fills in commands and shortcuts for this platform', () => {
    const src = 'Open {{cmd:sidebar.ssh}}, press {{key:sidebar.ssh}}, or {{cmd:tab.close}}. {{mac:⌘-click|other:Ctrl-click}} {{cmd:nope}}';
    const mac = renderManual(src, ctx(true));
    expect(mac.html).toContain('<b>Show SSH Hosts</b> <kbd>⌘⇧S</kbd>, press <kbd>⌘⇧S</kbd>, or <b>Close Tab</b> (command palette). ⌘-click');
    expect(mac.missing).toEqual(['nope']);
    expect(renderManual(src, ctx(false)).html).toContain('<kbd>Ctrl+Shift+Alt+S</kbd>, or <b>Close Tab</b> (command palette). Ctrl-click');
  });

  it('links to pages and out of the app, and nothing else', () => {
    const r = renderManual('See [tabs](#tabs), [docs](https://example.com/x?a=1&b=2) and [bad](javascript:alert(1)).', ctx());
    expect(r.links).toEqual(['tabs']);
    expect(r.html).toContain('<a href="#" data-topic="tabs">tabs</a>');
    expect(r.html).toContain('data-href="https://example.com/x?a=1&amp;b=2"');
    expect(r.html).not.toContain('javascript');
  });

  it('gives plain text for search', () => {
    expect(plainText('# SSH **Keys**\n{{cmd:x}} use `ssh-add` [agent](#a)')).toBe(' ssh keys use ssh-add agent');
  });
});

describe('the manual pages', async () => {
  const { TOPICS, SIDEBAR_TOPIC } = await import('../manual');
  const { commands, shortcutLabel } = await import('./commands');
  const { SIDEBAR_TABS } = await import('./sidebar.svelte');
  const byId = new Map(commands.map((c) => [c.id, c]));
  const real: ManualContext = {
    isMac: true,
    label: (id) => byId.get(id)?.label ?? null,
    shortcut: (id) => (byId.has(id) ? (shortcutLabel(byId.get(id)!) ?? null) : null),
  };
  const ids = new Set(TOPICS.map((t) => t.id));

  it('have unique ids and a title of their own', () => {
    expect(ids.size).toBe(TOPICS.length);
    for (const t of TOPICS) expect(t.source.trim().startsWith('# '), t.id).toBe(true);
  });

  it.each(TOPICS.map((t) => [t.id, t]))('%s names only real commands and pages', (_, t) => {
    const r = renderManual(t.source, real);
    expect(r.missing, `unknown commands in ${t.id}`).toEqual([]);
    for (const link of r.links) expect(ids.has(link), `${t.id} links to #${link}`).toBe(true);
    // No manual placeholder left unfilled (a page may show {{snippet}} placeholders as text).
    expect(r.html).not.toMatch(/\{\{(cmd|key|mac):/);
  });

  it('cover every side-pane tab', () => {
    for (const tab of SIDEBAR_TABS) expect(ids.has(SIDEBAR_TOPIC[tab]), tab).toBe(true);
  });
});
