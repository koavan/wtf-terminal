/**
 * Calls `refresh` now and then every `intervalMs` while the window is in
 * front. An interval of 0 means load once and never again on its own.
 * Returns a function that stops it.
 */
export function poll(refresh: () => void, intervalMs: number): () => void {
  refresh();
  if (intervalMs <= 0) return () => {};
  const tick = () => {
    if (document.visibilityState === 'visible' && document.hasFocus()) refresh();
  };
  const timer = setInterval(tick, intervalMs);
  window.addEventListener('focus', tick);
  return () => {
    clearInterval(timer);
    window.removeEventListener('focus', tick);
  };
}

/** The side pane's polling interval from settings: 0 when auto-refresh is off. */
export function sidebarInterval(config: { sidebarAutoRefresh: boolean; sidebarRefreshInterval: number }): number {
  return config.sidebarAutoRefresh ? Math.max(1, config.sidebarRefreshInterval) * 1000 : 0;
}
