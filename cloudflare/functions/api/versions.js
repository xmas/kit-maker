// GET /api/versions?blueprint=<id>: whether publishing is set up, the
// blueprint's versions (newest first) and the newest published workload
// (what a mixin runs on).
import { guard, json } from '../_lib/http.js'
import { publishConfig, versionsJSON } from '../_lib/publish.js'

export async function onRequestGet({ request, env }) {
  const [user, refused] = await guard(request, env)
  if (refused) return refused
  const cfg = publishConfig(env)
  const id = new URL(request.url).searchParams.get('blueprint') || ''
  const { results } = await env.DB.prepare('SELECT * FROM blueprint_versions WHERE blueprint_id = ? ORDER BY version DESC').bind(id).all()
  const w = await env.DB.prepare("SELECT name, slug, repository FROM blueprint_versions WHERE kind = 'workload' AND status = 'published' ORDER BY finished_at DESC LIMIT 1").first()
  let setup = ''
  if (cfg.missing.length) setup = `Publishing isn’t set up: set ${cfg.missing.join(', ')} for this Pages project (see cloudflare/README.md).`
  else if (user.id === 'guest') setup = 'Guests can’t publish. Sign in with your email to publish.'
  return json({
    configured: !setup,
    setup,
    registry: cfg.registry,
    allowedSource: cfg.registry ? cfg.registry + '/' : '',
    versions: versionsJSON(results),
    workload: w ? { name: w.name, slug: w.slug, image: w.repository + ':latest' } : null,
  })
}
