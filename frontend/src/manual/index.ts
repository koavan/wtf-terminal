// The in-app manual's pages, in reading order (see lib/manual.ts for the
// Markdown they use). The id is what [links](#id) and the side pane's ?
// button point at.

import aws from './aws.md?raw';
import broadcast from './broadcast.md?raw';
import contextBadges from './context-badges.md?raw';
import files from './files.md?raw';
import find from './find.md?raw';
import gettingStarted from './getting-started.md?raw';
import history from './history.md?raw';
import kubeContexts from './kube-contexts.md?raw';
import kubernetes from './kubernetes.md?raw';
import logs from './logs.md?raw';
import network from './network.md?raw';
import pinsPage from './pins.md?raw';
import panes from './panes.md?raw';
import privacy from './privacy.md?raw';
import recording from './recording.md?raw';
import secrets from './secrets.md?raw';
import share from './share.md?raw';
import generate from './generate.md?raw';
import processes from './processes.md?raw';
import sessions from './sessions.md?raw';
import settings from './settings.md?raw';
import shellIntegration from './shell-integration.md?raw';
import shortcuts from './shortcuts.md?raw';
import snippetsPage from './snippets.md?raw';
import sideP from './side-pane.md?raw';
import sshFiles from './ssh-files.md?raw';
import sshHostKeys from './ssh-host-keys.md?raw';
import sshHosts from './ssh-hosts.md?raw';
import sshKeys from './ssh-keys.md?raw';
import gitIdentitiesPage from './git-identities.md?raw';
import sshTunnels from './ssh-tunnels.md?raw';
import tabs from './tabs.md?raw';
import workspaces from './workspaces.md?raw';
import troubleshooting from './troubleshooting.md?raw';

export interface Topic {
  id: string;
  title: string;
  group: string;
  source: string;
}

const t = (group: string, id: string, title: string, source: string): Topic => ({ id, title, group, source });

export const TOPICS: Topic[] = [
  t('Basics', 'getting-started', 'Welcome to wtf', gettingStarted),
  t('Basics', 'tabs', 'Tabs and tab groups', tabs),
  t('Basics', 'panes', 'Split panes', panes),
  t('Basics', 'shell-integration', 'Commands, prompts and output', shellIntegration),
  t('Basics', 'logs', 'Logs in the output', logs),
  t('Basics', 'secrets', 'Hide Secrets (screen sharing)', secrets),
  t('Basics', 'recording', 'Recording and saving a terminal', recording),
  t('Basics', 'find', 'Find', find),
  t('Basics', 'context-badges', 'Address bar and context badges', contextBadges),
  t('Basics', 'broadcast', 'Broadcast input', broadcast),
  t('Basics', 'snippets', 'Snippets and runbooks', snippetsPage),
  t('Basics', 'workspaces', 'Workspaces', workspaces),
  t('Side pane', 'side-pane', 'The side pane', sideP),
  t('Side pane', 'files', 'Files', files),
  t('Side pane', 'network', 'Network', network),
  t('Side pane', 'processes', 'Processes', processes),
  t('Side pane', 'history', 'History', history),
  t('Side pane', 'pins', 'Pinned output', pinsPage),
  t('Side pane', 'share', 'Share & Record', share),
  t('Side pane', 'generate', 'Generate: SSH keys and passwords', generate),
  t('SSH', 'ssh-hosts', 'SSH hosts', sshHosts),
  t('SSH', 'ssh-keys', 'SSH keys and ssh-agent', sshKeys),
  t('SSH', 'git-identities', 'Git identities (more than one GitHub user)', gitIdentitiesPage),
  t('SSH', 'ssh-host-keys', 'Host keys (known_hosts)', sshHostKeys),
  t('SSH', 'ssh-tunnels', 'SSH tunnels', sshTunnels),
  t('SSH', 'ssh-files', 'Copying files over SSH', sshFiles),
  t('Cloud', 'aws', 'AWS profiles', aws),
  t('Cloud', 'kubernetes', 'Kubernetes: resources', kubernetes),
  t('Cloud', 'kube-contexts', 'Kubernetes: contexts', kubeContexts),
  t('Reference', 'settings', 'Settings and themes', settings),
  t('Reference', 'sessions', 'Sessions and windows', sessions),
  t('Reference', 'shortcuts', 'Keyboard shortcuts', shortcuts),
  t('Reference', 'privacy', 'Privacy and safety', privacy),
  t('Reference', 'troubleshooting', 'Troubleshooting', troubleshooting),
];

/** The page for each side-pane tab, for its ? button. */
export const SIDEBAR_TOPIC: Record<string, string> = {
  files: 'files',
  ssh: 'ssh-hosts',
  aws: 'aws',
  kube: 'kubernetes',
  snippets: 'snippets',
  workspaces: 'workspaces',
  network: 'network',
  processes: 'processes',
  history: 'history',
  pins: 'pins',
  share: 'share',
  generate: 'generate',
};
