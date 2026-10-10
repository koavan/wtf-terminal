import { Dialogs } from '@wailsio/runtime';
import { SvelteMap } from 'svelte/reactivity';
import { Service as Files } from '../../bindings/github.com/dayanraj/wtf/internal/files';
import { Service, prettyPath } from './backend';
import { leaves } from './layout';
import { redaction } from './redaction.svelte';
import { tabs, tabTitle, type Pane } from './tabs.svelte';
import { terminalFor } from './terminals';
import { formatDuration, toast } from './toast.svelte';
import { exportName, markdownTranscript } from './transcript';

// Recording a terminal (asciicast, written by the backend as output
// arrives — see internal/terminal/record.go) and saving one: its output
// as text, or its commands as a Markdown transcript.

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** The tab a pane is in, for names. */
function titleOf(pane: Pane): string {
  const tab = tabs.list.find((t) => leaves(t.root).some((p) => p.id === pane.id));
  return tab ? tabTitle(tab, prettyPath) : prettyPath(pane.cwd) || 'terminal';
}

async function pickFile(title: string, name: string, ext: string, kind: string): Promise<string> {
  const path = await Dialogs.SaveFile({
    Title: title,
    Filename: `${name}.${ext}`,
    CanCreateDirectories: true,
    Filters: [{ DisplayName: kind, Pattern: `*.${ext}` }],
  });
  return typeof path === 'string' ? path : '';
}

/** A file saved from a terminal in this window, for the Share & Record tab. */
export interface SavedFile {
  path: string;
  kind: 'recording' | 'text' | 'markdown';
  at: number;
}

/** How many saved files the tab lists. */
const MAX_SAVED = 20;

class Recordings {
  /** Panes being recorded: where to, and since when (ms). */
  active = new SvelteMap<number, { path: string; since: number }>();
  /** Files saved in this window, newest first. */
  saved = $state<SavedFile[]>([]);

  remember(path: string, kind: SavedFile['kind']) {
    this.saved = [{ path, kind, at: Date.now() }, ...this.saved.filter((f) => f.path !== path)].slice(0, MAX_SAVED);
  }

  /** Whether a pane is being recorded. */
  of(paneId: number) {
    return this.active.get(paneId);
  }

  /** Asks the backend (a pane that took over a session from another window). */
  async sync(pane: Pane) {
    if (!pane.sessionId) return;
    const st = await Service.Recording(pane.sessionId).catch(() => null);
    if (st?.active) this.active.set(pane.id, { path: st.path, since: Date.now() - st.seconds * 1000 });
    else this.active.delete(pane.id);
  }

  async toggle(pane: Pane | undefined = tabs.activePane) {
    if (!pane?.sessionId) return;
    if (this.active.has(pane.id)) return this.stop(pane);
    const title = titleOf(pane);
    const path = await pickFile('Record Terminal', exportName(title, new Date()), 'cast', 'asciinema recording');
    if (!path) return;
    try {
      await Service.StartRecording(pane.sessionId, path, title);
      this.active.set(pane.id, { path, since: Date.now() });
      toast.show(
        redaction.on
          ? `Recording to ${prettyPath(path)} — output is recorded as is; Hide Secrets doesn't apply to it`
          : `Recording to ${prettyPath(path)}`,
        5000,
      );
    } catch (e) {
      toast.show(`Couldn't start recording: ${message(e)}`);
    }
  }

  async stop(pane: Pane) {
    const rec = this.active.get(pane.id);
    this.active.delete(pane.id);
    if (!pane.sessionId || !rec) return;
    try {
      const st = await Service.StopRecording(pane.sessionId);
      this.remember(rec.path, 'recording');
      toast.show(`Saved recording to ${prettyPath(rec.path)} (${formatDuration(st.seconds * 1000)})`, 5000);
    } catch (e) {
      toast.show(`The recording ended with an error: ${message(e)}`);
    }
  }

  /** The pane's shell is gone; the backend has closed its recording. */
  ended(paneId: number) {
    const rec = this.active.get(paneId);
    if (!rec) return;
    this.active.delete(paneId);
    this.remember(rec.path, 'recording');
    toast.show(`Saved recording to ${prettyPath(rec.path)}`, 5000);
  }
}

export const recordings = new Recordings();

/** Saves a pane's scrollback as a text file. */
export async function saveOutputAsText(pane: Pane | undefined = tabs.activePane) {
  const term = pane && terminalFor(pane.id);
  if (!pane || !term) return;
  const path = await pickFile('Save Output as Text', exportName(titleOf(pane), new Date()), 'txt', 'Text');
  if (!path) return;
  try {
    await Files.SaveText(path, redaction.text(term.plainText()) + '\n');
    recordings.remember(path, 'text');
    toast.show(`Saved output to ${prettyPath(path)}`);
  } catch (e) {
    toast.show(`Couldn't save: ${message(e)}`);
  }
}

/** Saves a pane's commands and their output as a Markdown transcript. */
export async function saveTranscript(pane: Pane | undefined = tabs.activePane) {
  const term = pane && terminalFor(pane.id);
  if (!pane || !term) return;
  const blocks = term.transcript();
  if (!blocks?.length) {
    toast.show(blocks ? 'No commands to save yet' : 'A transcript needs shell integration (bash or zsh) — Save Output as Text works anywhere');
    return;
  }
  const title = titleOf(pane);
  const path = await pickFile('Save Commands as Markdown', exportName(title, new Date()), 'md', 'Markdown');
  if (!path) return;
  try {
    const md = markdownTranscript(blocks, { title, cwd: prettyPath(pane.cwd), exportedAt: new Date() });
    await Files.SaveText(path, redaction.text(md));
    recordings.remember(path, 'markdown');
    toast.show(`Saved ${blocks.length} ${blocks.length === 1 ? 'command' : 'commands'} to ${prettyPath(path)}`);
  } catch (e) {
    toast.show(`Couldn't save: ${message(e)}`);
  }
}
