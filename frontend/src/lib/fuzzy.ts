export interface Match {
  score: number;
  /** Indices of the matched characters in the text, for highlighting. */
  indices: number[];
}

const isBoundary = (text: string, i: number) => i === 0 || /[\s\-_/.:]/.test(text[i - 1]);

/**
 * Subsequence match, case-insensitive. Rewards matches at word starts and
 * runs of consecutive characters, so "spr" ranks "Split Right" above
 * "Select Previous Run". Returns null when `query` doesn't match.
 */
export function fuzzyMatch(query: string, text: string): Match | null {
  const q = query.toLowerCase().replace(/\s+/g, '');
  if (!q) return { score: 0, indices: [] };
  const t = text.toLowerCase();

  const indices: number[] = [];
  let score = 0;
  let ti = 0;
  let prev = -2;
  for (const ch of q) {
    // Prefer the next occurrence that starts a word; fall back to any.
    let found = -1;
    for (let j = ti; j < t.length; j++) {
      if (t[j] !== ch) continue;
      if (found < 0) found = j;
      if (j === prev + 1) {
        found = j;
        break;
      }
      if (isBoundary(text, j)) {
        found = j;
        break;
      }
    }
    if (found < 0) return null;

    score += 1;
    if (found === prev + 1) score += 7;
    if (isBoundary(text, found)) score += 5;
    if (found === 0) score += 4;
    score -= Math.min(found - ti, 6) * 0.25; // gaps cost a little
    indices.push(found);
    prev = found;
    ti = found + 1;
  }
  // Shorter texts are tighter matches.
  score -= text.length * 0.02;
  return { score, indices };
}
