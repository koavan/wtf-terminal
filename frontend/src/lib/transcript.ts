import { formatDuration } from './toast.svelte';

// Saving a terminal for later — a postmortem, a ticket, a runbook: its
// output as plain text, or its commands as a Markdown transcript (each
// command with when it ran, how it went and what it printed). See also
// internal/terminal/record.go for live recordings (asciicast).

/** One command, as the transcript needs it. */
export interface TranscriptBlock {
  text: string;
  output: string | null;
  startedAt?: Date;
  finishedAt?: Date;
  exitCode?: number;
  durationMs?: number;
}

export interface TranscriptInfo {
  title: string;
  cwd: string;
  exportedAt: Date;
}

/** A Markdown code fence longer than any run of backticks in the text. */
export function fence(text: string): string {
  const longest = Math.max(0, ...[...text.matchAll(/`+/g)].map((m) => m[0].length));
  return '`'.repeat(Math.max(3, longest + 1));
}

/** Inline code that survives backticks in the text. */
function code(text: string): string {
  const longest = Math.max(0, ...[...text.matchAll(/`+/g)].map((m) => m[0].length));
  const ticks = '`'.repeat(longest + 1);
  const pad = text.startsWith('`') || text.endsWith('`') ? ' ' : '';
  return `${ticks}${pad}${text}${pad}${ticks}`;
}

const clock = (d: Date) => d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
const day = (d: Date) => d.toLocaleDateString([], { year: 'numeric', month: 'short', day: 'numeric' });

export function markdownTranscript(blocks: TranscriptBlock[], info: TranscriptInfo): string {
  const out: string[] = [
    `# ${info.title}`,
    '',
    `Terminal transcript from wtf, saved ${day(info.exportedAt)} ${clock(info.exportedAt)}${info.cwd ? ` · ${info.cwd}` : ''} · ${blocks.length} ${blocks.length === 1 ? 'command' : 'commands'}`,
  ];
  blocks.forEach((b, i) => {
    out.push('', `## ${i + 1}. ${code(b.text || '(no command text)')}`, '');
    const facts: string[] = [];
    if (b.exitCode === undefined) facts.push('still running');
    else facts.push(b.exitCode === 0 ? '✓ succeeded' : `✗ exit ${b.exitCode}`);
    if (b.startedAt) facts.push(`started ${clock(b.startedAt)}`);
    if (b.durationMs !== undefined) facts.push(`took ${formatDuration(b.durationMs)}`);
    out.push(facts.join(' · '));
    if (b.output === null) out.push('', '_Output no longer in the scrollback._');
    else if (b.output) {
      const f = fence(b.output);
      out.push('', `${f}text`, b.output, f);
    } else out.push('', '_No output._');
  });
  return out.join('\n') + '\n';
}

/** "wtf-kubectl-get-pods-2026-10-09-2350": a file name from a title and a time. */
export function exportName(title: string, at: Date): string {
  const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40) || 'terminal';
  const p = (n: number) => String(n).padStart(2, '0');
  return `wtf-${slug}-${at.getFullYear()}-${p(at.getMonth() + 1)}-${p(at.getDate())}-${p(at.getHours())}${p(at.getMinutes())}`;
}
