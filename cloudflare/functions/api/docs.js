// /api/docs: the shared document store behind the engine (internal/lite).
//
// GET ?since=<iso> returns blocks and blueprints changed since then (all of
// them without since) and the time to ask from next. Revisions are fetched
// per blueprint from /api/revisions.
//
// POST {docs: [{kind, id, parent, rev, body}]} writes documents in one
// transaction. rev is the version the page last saw (0 for one it never
// stored); if any document has moved on, nothing is written and the answer
// is 409 with the current copies, so the page can reload them.
import { error, guard, json } from '../_lib/http.js'

const KINDS = new Set(['block', 'blueprint', 'revision'])
const ID = /^[A-Za-z0-9_-]{1,120}$/
const MAX_DOCS = 50
const MAX_BODY = 900_000 // bytes of JSON per document, under D1's row limit

function rows(results) {
  // Bodies are stored as JSON text; splice them in rather than re-parse.
  return '[' + results.map((r) => `{"kind":${JSON.stringify(r.kind)},"id":${JSON.stringify(r.id)},"parent":${JSON.stringify(r.parent || '')},"rev":${r.rev},"body":${r.body}}`).join(',') + ']'
}

export async function onRequestGet({ request, env }) {
  const [, refused] = await guard(request, env)
  if (refused) return refused
  const since = new URL(request.url).searchParams.get('since') || ''
  // Ask from a little before now next time, so a write that was committing
  // during this read isn't missed (seeing a document twice is harmless).
  const next = new Date(Date.now() - 5000).toISOString()
  const { results } = await env.DB.prepare("SELECT kind, id, parent, rev, body FROM docs WHERE kind != 'revision' AND updated_at >= ?").bind(since).all()
  return new Response(`{"docs":${rows(results)},"now":${JSON.stringify(next)}}`, {
    headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' },
  })
}

export async function onRequestPost({ request, env }) {
  const [user, refused] = await guard(request, env)
  if (refused) return refused
  let input
  try {
    input = await request.json()
  } catch {
    return error('send JSON', 400)
  }
  const docs = Array.isArray(input.docs) ? input.docs : []
  if (!docs.length || docs.length > MAX_DOCS) return error(`send 1 to ${MAX_DOCS} documents`, 400)
  for (const d of docs) {
    if (!KINDS.has(d.kind) || !ID.test(d.id || '') || (d.parent && !ID.test(d.parent)) || !Number.isInteger(d.rev) || d.rev < 0) {
      return error('a document is malformed', 400)
    }
    if (!d.body || typeof d.body !== 'object' || d.body.id !== d.id) return error(`document ${d.kind}/${d.id} has no matching body`, 400)
    if (d.kind === 'revision' && d.rev !== 0) return error('revisions are never rewritten', 400)
    d.text = JSON.stringify(d.body)
    if (d.text.length > MAX_BODY) return error(`document ${d.kind}/${d.id} is too large`, 413)
  }

  const now = new Date().toISOString()
  const statements = []
  for (const d of docs) {
    statements.push(
      env.DB.prepare('INSERT INTO rev_guard (ok) SELECT 1 WHERE COALESCE((SELECT rev FROM docs WHERE kind = ?1 AND id = ?2), 0) != ?3').bind(d.kind, d.id, d.rev),
      env.DB.prepare(
        `INSERT INTO docs (kind, id, parent, rev, body, updated_at, updated_by) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
         ON CONFLICT (kind, id) DO UPDATE SET parent = excluded.parent, rev = excluded.rev, body = excluded.body,
           updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
      ).bind(d.kind, d.id, d.parent || null, d.rev + 1, d.text, now, user.email),
    )
  }
  try {
    await env.DB.batch(statements)
  } catch (e) {
    if (!/CHECK constraint/i.test(String(e?.message ?? e))) throw e
    // Someone saved first: send the current copies of what they changed.
    const keys = docs.filter((d) => d.kind !== 'revision')
    const current = []
    for (const d of keys) {
      const r = await env.DB.prepare('SELECT kind, id, parent, rev, body FROM docs WHERE kind = ? AND id = ?').bind(d.kind, d.id).first()
      if (r) current.push(r)
    }
    return new Response(`{"error":"Someone else changed this just now. It has been reloaded; make your change again.","docs":${rows(current)}}`, {
      status: 409,
      headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' },
    })
  }
  const revs = {}
  for (const d of docs) revs[`${d.kind}/${d.id}`] = d.rev + 1
  return json({ revs })
}
