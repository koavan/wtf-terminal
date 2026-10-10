import { Service as NotifyService } from '../../bindings/github.com/dayanraj/wtf/internal/notify';
import { prettyPath } from './backend';
import type { Pane } from './layout';
import { redaction } from './redaction.svelte';
import { settings } from './settings.svelte';
import { formatDuration } from './toast.svelte';

/** Commands that ran at least `notify-after` seconds notify when they finish unseen. */
export function notifyCommandDone(pane: Pane, exitCode: number, durationMs: number, onScreen: boolean) {
  const after = settings.config.notifyAfter;
  if (after <= 0 || durationMs < after * 1000) return;
  if (onScreen && document.hasFocus()) return;
  const took = formatDuration(durationMs);
  const title = exitCode === 0 ? `✓ Finished in ${took}` : `✗ Failed (exit ${exitCode}) after ${took}`;
  NotifyService.Notify(title, prettyPath(pane.cwd), redaction.text(pane.lastCommand?.text || pane.process)).catch(() => {});
}
