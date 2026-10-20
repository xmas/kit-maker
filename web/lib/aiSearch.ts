// Ask the server which blocks fit a plain-language need. The answer says
// whether the AI ranked them or, without AI, keyword matching did.
export interface AISearchResult {
  source: 'ai' | 'keyword'
  message?: string
  matches: { id: string; reason: string }[]
}

export async function aiSearch(q: string, type?: string): Promise<AISearchResult> {
  const res = await fetch('/blocks/search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ q, type: type ?? '' }),
  })
  if (!res.ok) throw new Error(`search failed (${res.status})`)
  return res.json()
}
