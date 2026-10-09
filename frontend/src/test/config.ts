import type { Config } from '../../bindings/github.com/dayanraj/wtf/internal/config';

/** The Go defaults (internal/config.Defaults), for tests that don't load a config. */
export function testConfig(over: Partial<Config> = {}): Config {
  return {
    theme: 'auto',
    fontFamily: 'JetBrains Mono',
    fontSize: 13,
    lineHeight: 1.22,
    cursorStyle: 'bar',
    cursorBlink: true,
    scrollback: 10000,
    optionAsMeta: false,
    copyOnSelect: false,
    shell: '',
    shellIntegration: true,
    notifyAfter: 10,
    unfocusedSplitDim: 0.35,
    sidebarAutoRefresh: true,
    sidebarRefreshInterval: 3,
    contextBadges: true,
    ...over,
  } as Config;
}
