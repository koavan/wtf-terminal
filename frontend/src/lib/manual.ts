// Renders the in-app manual's pages (src/manual/*.md) to HTML.
//
// A small Markdown subset — just what the pages use — so there's no
// dependency to keep up: headings (#, ##, ###), paragraphs, - and 1. lists,
// > tips, | tables |, ``` code blocks, `code`, **bold**, *italics*, and
// links: [text](#topic) to another page, [text](https://…) out of the app.
//
// All text is HTML-escaped first; the only markup in the output is the
// renderer's own. Shortcuts come from the command registry, so the manual
// can't drift from the app:
//   {{cmd:sidebar.ssh}}  → the command's name and its shortcut
//   {{key:sidebar.ssh}}  → just the shortcut
//   {{mac:⌘-click|other:Ctrl-click}} → text for this platform

export interface ManualContext {
  isMac: boolean;
  /** A command's label, or null if there's no such command. */
  label(id: string): string | null;
  /** A command's shortcut on this platform, or null if it has none. */
  shortcut(id: string): string | null;
}

export interface Rendered {
  html: string;
  /** Command ids the page mentions that don't exist (tests keep this empty). */
  missing: string[];
  /** Topic ids the page links to. */
  links: string[];
}

const escape = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** Inline markup of one already-split piece of text. */
function inline(text: string, ctx: ManualContext, out: Rendered): string {
  // Code spans first, so nothing inside them is touched.
  const codes: string[] = [];
  let s = text.replace(/`([^`]+)`/g, (_, c: string) => {
    codes.push(`<code>${escape(c)}</code>`);
    return `\u0000${codes.length - 1}\u0000`;
  });
  s = escape(s);
  s = s.replace(/\{\{(cmd|key):([\w.]+)\}\}/g, (_, kind: string, id: string) => {
    const label = ctx.label(id);
    if (label === null) {
      out.missing.push(id);
      return `<b>${escape(id)}</b>`;
    }
    const key = ctx.shortcut(id);
    const kbd = key ? `<kbd>${escape(key)}</kbd>` : '';
    if (kind === 'key') return kbd || `<b>${escape(label)}</b> in the command palette`;
    return key ? `<b>${escape(label)}</b> ${kbd}` : `<b>${escape(label)}</b> (command palette)`;
  });
  s = s.replace(/\{\{mac:([^|}]*)\|other:([^}]*)\}\}/g, (_, mac: string, other: string) => (ctx.isMac ? mac : other));
  s = s.replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>');
  s = s.replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, '$1<i>$2</i>');
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, label: string, href: string) => {
    if (href.startsWith('#')) {
      out.links.push(href.slice(1));
      return `<a href="#" data-topic="${href.slice(1)}">${label}</a>`;
    }
    if (/^https:\/\//.test(href)) return `<a href="#" data-href="${href}">${label}</a>`;
    return label;
  });
  return s.replace(/\u0000(\d+)\u0000/g, (_, i: string) => codes[Number(i)]);
}

export function renderManual(src: string, ctx: ManualContext): Rendered {
  const out: Rendered = { html: '', missing: [], links: [] };
  const lines = src.replace(/\r\n/g, '\n').split('\n');
  const html: string[] = [];
  let para: string[] = [];

  const flush = () => {
    if (para.length) html.push(`<p>${inline(para.join(' '), ctx, out)}</p>`);
    para = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const t = line.trim();

    if (t.startsWith('```')) {
      flush();
      const code: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith('```'); i++) code.push(lines[i]);
      html.push(`<pre><code>${escape(code.join('\n'))}</code></pre>`);
      continue;
    }
    if (!t) {
      flush();
      continue;
    }
    const h = /^(#{1,3})\s+(.*)$/.exec(t);
    if (h) {
      flush();
      const level = h[1].length;
      html.push(`<h${level}>${inline(h[2], ctx, out)}</h${level}>`);
      continue;
    }
    if (t.startsWith('|')) {
      flush();
      const rows: string[][] = [];
      for (; i < lines.length && lines[i].trim().startsWith('|'); i++) {
        const cells = lines[i].trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
        if (!cells.every((c) => /^:?-+:?$/.test(c))) rows.push(cells);
      }
      i--;
      const [head, ...body] = rows;
      html.push(
        '<table><thead><tr>' +
          head.map((c) => `<th>${inline(c, ctx, out)}</th>`).join('') +
          '</tr></thead><tbody>' +
          body.map((r) => '<tr>' + r.map((c) => `<td>${inline(c, ctx, out)}</td>`).join('') + '</tr>').join('') +
          '</tbody></table>',
      );
      continue;
    }
    const list = /^(-|\d+\.)\s+/.exec(t);
    if (list) {
      flush();
      const ordered = list[1] !== '-';
      const items: string[] = [];
      for (; i < lines.length; i++) {
        const l = lines[i];
        const m = /^\s*(-|\d+\.)\s+(.*)$/.exec(l);
        if (m && (m[1] === '-') !== ordered) items.push(m[2]);
        else if (l.trim() && /^\s{2,}/.test(l) && items.length) items[items.length - 1] += ' ' + l.trim();
        else break;
      }
      i--;
      const tag = ordered ? 'ol' : 'ul';
      html.push(`<${tag}>${items.map((it) => `<li>${inline(it, ctx, out)}</li>`).join('')}</${tag}>`);
      continue;
    }
    if (t.startsWith('>')) {
      flush();
      const tip: string[] = [];
      for (; i < lines.length && lines[i].trim().startsWith('>'); i++) tip.push(lines[i].trim().replace(/^>\s?/, ''));
      i--;
      html.push(`<aside>${inline(tip.join(' '), ctx, out)}</aside>`);
      continue;
    }
    para.push(t);
  }
  flush();
  out.html = html.join('\n');
  return out;
}

/** A page's words without markup, for searching. */
export function plainText(src: string): string {
  return src
    .replace(/\{\{(cmd|key):[\w.]+\}\}/g, ' ')
    .replace(/\{\{mac:([^|}]*)\|other:([^}]*)\}\}/g, '$1 $2')
    .replace(/[#*`>|]/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\s+/g, ' ')
    .toLowerCase();
}
