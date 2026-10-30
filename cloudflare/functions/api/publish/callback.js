// POST /api/publish/callback {id, token, status, digest, error, runUrl}:
// the publish-kit workflow's report. No session: the one-use token from the
// publish request is the proof.
import { sha256 } from '../../_lib/auth.js'
import { error, guard, json, readJSON } from '../../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  const b = (await readJSON(request)) || {}
  const [blueprintId, v] = String(b.id || '').split(':')
  const version = Number(v)
  const row = await env.DB.prepare("SELECT token_hash FROM blueprint_versions WHERE blueprint_id = ? AND version = ? AND status = 'pending'").bind(blueprintId, version).first()
  if (!row || !row.token_hash || row.token_hash !== (await sha256(String(b.token || '')))) return error('unknown or finished publish', 404)
  const ok = b.status === 'published' && /^sha256:[0-9a-f]{64}$/.test(String(b.digest || ''))
  const runUrl = /^https:\/\/github\.com\/[^\s"']+$/.test(String(b.runUrl || '')) ? b.runUrl : null
  await env.DB.prepare('UPDATE blueprint_versions SET status = ?, digest = ?, error = ?, run_url = ?, token_hash = NULL, finished_at = ? WHERE blueprint_id = ? AND version = ?')
    .bind(ok ? 'published' : 'failed', ok ? b.digest : null, ok ? null : String(b.error || 'The build failed; see the workflow run.').slice(0, 500), runUrl, new Date().toISOString(), blueprintId, version)
    .run()
  return json({ ok: true })
}
