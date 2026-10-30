// GET /api/revisions?blueprint=<id>: a blueprint's stored history.
import { error, guard } from '../_lib/http.js'

export async function onRequestGet({ request, env }) {
  const [, refused] = await guard(request, env)
  if (refused) return refused
  const id = new URL(request.url).searchParams.get('blueprint') || ''
  if (!/^[A-Za-z0-9_-]{1,120}$/.test(id)) return error('which blueprint?', 400)
  const { results } = await env.DB.prepare("SELECT kind, id, parent, rev, body FROM docs WHERE kind = 'revision' AND parent = ?").bind(id).all()
  const docs = results.map((r) => `{"kind":"revision","id":${JSON.stringify(r.id)},"parent":${JSON.stringify(r.parent)},"rev":${r.rev},"body":${r.body}}`)
  return new Response(`{"docs":[${docs.join(',')}]}`, { headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } })
}
