// POST /api/auth/guest {passphrase, next}: signs in as Guest with the
// shared GUEST_PASSPHRASE (only when that secret is set). Ten tries per IP
// per 15 minutes.
import { GUEST, guestEnabled, guestSession, safeNext, sameSecret } from '../../_lib/auth.js'
import { error, guard, json, readJSON } from '../../_lib/http.js'

const WINDOW = 15 * 60 * 1000

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  if (!guestEnabled(env) || !env.JWT_SECRET) return error('Guest sign-in isn’t turned on here.', 404)
  const ip = request.headers.get('CF-Connecting-IP') || 'unknown'
  const since = new Date(Date.now() - WINDOW).toISOString()
  const { n } = await env.DB.prepare("SELECT COUNT(*) AS n FROM auth_events WHERE kind = 'guest-fail' AND key = ? AND at > ?").bind(ip, since).first()
  if (n >= 10) return error('Too many tries. Wait 15 minutes and try again.', 429)

  const body = (await readJSON(request)) || {}
  if (!(await sameSecret(String(body.passphrase || '').trim(), String(env.GUEST_PASSPHRASE).trim()))) {
    await env.DB.prepare("INSERT INTO auth_events (kind, key, at) VALUES ('guest-fail', ?, ?)").bind(ip, new Date().toISOString()).run()
    return error('That passphrase isn’t right.', 401)
  }
  return json({ user: GUEST, next: safeNext(body.next) }, 200, { 'Set-Cookie': await guestSession(env) })
}
