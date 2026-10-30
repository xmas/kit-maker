// POST /api/auth/code {email, code}: signs in with the emailed code. Five
// wrong codes use up the email's pending sign-ins.
import { allowed, normalizeEmail, sha256, signIn, userFor } from '../../_lib/auth.js'
import { error, guard, json, readJSON } from '../../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  const body = (await readJSON(request)) || {}
  const email = normalizeEmail(body.email)
  const code = String(body.code || '').replace(/\D/g, '')
  const now = new Date().toISOString()
  const { results } = await env.DB.prepare('SELECT id, code_hash, next FROM login_tokens WHERE email = ? AND expires_at > ? AND attempts < 5').bind(email, now).all()
  for (const t of results) {
    if (code.length === 6 && (await sha256(code + t.id)) === t.code_hash) {
      if (!allowed(email, env)) return error('This email address can’t sign in to this Kit Maker.', 403)
      await env.DB.prepare('DELETE FROM login_tokens WHERE id = ?').bind(t.id).run()
      return json({ user: userFor(email), next: t.next }, 200, { 'Set-Cookie': await signIn(env, email) })
    }
  }
  await env.DB.batch([
    env.DB.prepare('UPDATE login_tokens SET attempts = attempts + 1 WHERE email = ?').bind(email),
    env.DB.prepare('DELETE FROM login_tokens WHERE attempts >= 5').bind(),
  ])
  return error('That code isn’t right, or it has expired. Check the email or send a new one.', 401)
}
