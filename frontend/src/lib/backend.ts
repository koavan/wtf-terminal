import { Service, type Endpoint } from '../../bindings/github.com/dayanraj/wtf/internal/terminal';

export { Service };

let endpointPromise: Promise<Endpoint> | null = null;

/** Where to stream session I/O. Fetched once; the token is fixed per launch. */
export function endpoint(): Promise<Endpoint> {
  endpointPromise ??= Service.Endpoint();
  return endpointPromise;
}

let home = '';

export async function loadHome(): Promise<void> {
  home = await Service.Home();
}

/** "/Users/me/src/wtf" → "~/src/wtf" */
export function prettyPath(path: string): string {
  if (!path) return '';
  if (home && (path === home || path.startsWith(home + '/'))) {
    return '~' + path.slice(home.length);
  }
  return path;
}
