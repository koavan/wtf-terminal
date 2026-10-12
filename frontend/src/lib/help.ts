import { SIDEBAR_TOPIC } from '../manual';
import { overlay } from './overlay.svelte';
import { sidebar } from './sidebar.svelte';

/** The manual page for what's on screen: the open side-pane tab's, else the start. */
export function helpTopic(): string {
  if (!sidebar.open) return 'getting-started';
  if (sidebar.tab === 'ssh') {
    return { hosts: 'ssh-hosts', keys: 'ssh-keys', git: 'git-identities', tunnels: 'ssh-tunnels' }[sidebar.sshSection];
  }
  if (sidebar.tab === 'kube' && sidebar.kubeView === 'contexts') return 'kube-contexts';
  return SIDEBAR_TOPIC[sidebar.tab] ?? 'getting-started';
}

/** Opens the manual, at a page or at the one for what's on screen. */
export function openManual(topic = helpTopic()) {
  overlay.show({ kind: 'manual', topic });
}
