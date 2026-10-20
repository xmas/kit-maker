// Fuzzy search over several fields. Every word of the query has to match
// one of the fields; a word matches as a substring (best at the start of a
// word) or, failing that, as its letters in order within a short stretch
// ("ghtok" finds "github-token"). Fields carry weights: a hit in the name
// counts for more than one deep in a block's content.

export interface Field {
  text: string
  weight: number
}

function wordScore(word: string, text: string): number {
  const at = text.indexOf(word)
  if (at >= 0) {
    const boundary = at === 0 || /[^a-z0-9]/.test(text[at - 1] ?? '')
    return boundary ? 3 : 2
  }
  // Letters in order, scored by how tight the stretch is.
  let best = 0
  for (let start = text.indexOf(word[0] ?? ''); start >= 0; start = text.indexOf(word[0] ?? '', start + 1)) {
    let i = 0
    let j = start
    for (; j < text.length && i < word.length; j++) if (text[j] === word[i]) i++
    if (i < word.length) break
    const span = j - start
    if (span <= word.length * 3) best = Math.max(best, word.length / span)
  }
  return best
}

// fuzzyScore is 0 when the query doesn't match, higher for better matches.
export function fuzzyScore(query: string, fields: Field[]): number {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return 1
  const texts = fields.map((f) => ({ text: f.text.toLowerCase(), weight: f.weight }))
  let total = 0
  for (const w of words) {
    let best = 0
    for (const f of texts) best = Math.max(best, wordScore(w, f.text) * f.weight)
    if (best === 0) return 0
    total += best
  }
  return total
}

// fuzzyFilter keeps the items that match, best first (stable for ties).
export function fuzzyFilter<T>(query: string, items: T[], fields: (item: T) => Field[]): T[] {
  if (!query.trim()) return items
  return items
    .map((item, i) => ({ item, i, s: fuzzyScore(query, fields(item)) }))
    .filter((x) => x.s > 0)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .map((x) => x.item)
}
