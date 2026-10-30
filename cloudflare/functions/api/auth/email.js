// POST /api/auth/email {email, next}: emails a sign-in code and link to
// anyone allowed (any address unless ALLOWED_EMAILS narrows it). At most 5
// emails per address per 15 minutes and 20 per IP per hour.
import { allowed, missingSettings, normalizeEmail, randomToken, safeNext, sha256, validEmail } from '../../_lib/auth.js'
import { error, guard, json, readJSON } from '../../_lib/http.js'
import { sendSignIn } from '../../_lib/mail.js'

const TTL = 15 * 60 * 1000

async function count(env, kind, key, sinceMs) {
  const since = new Date(Date.now() - sinceMs).toISOString()
  const r = await env.DB.prepare('SELECT COUNT(*) AS n FROM auth_events WHERE kind = ? AND key = ? AND at > ?').bind(kind, key, since).first()
  return r.n
}

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  const missing = missingSettings(env, request)
  if (missing.length) return error(`Sign-in isn’t set up: set ${missing.join(', ')} for this Pages project, then deploy again.`, 503)
  const body = (await readJSON(request)) || {}
  const email = normalizeEmail(body.email)
  if (!validEmail(email)) return error('Enter your email address.', 400)
  if (!allowed(email, env)) return error('This email address can’t sign in to this Kit Maker.', 403)

  const ip = request.headers.get('CF-Connecting-IP') || 'unknown'
  if ((await count(env, 'send-email', email, TTL)) >= 5 || (await count(env, 'send-ip', ip, 60 * 60 * 1000)) >= 20) {
    return error('Too many sign-in emails. Wait a few minutes and try again.', 429)
  }

  const token = randomToken()
  const id = await sha256(token)
  const code = String(crypto.getRandomValues(new Uint32Array(1))[0] % 1_000_000).padStart(6, '0')
  const now = new Date()
  await env.DB.batch([
    env.DB.prepare('DELETE FROM login_tokens WHERE expires_at < ?').bind(now.toISOString()),
    env.DB.prepare('DELETE FROM auth_events WHERE at < ?').bind(new Date(now - 60 * 60 * 1000).toISOString()),
    env.DB.prepare('INSERT INTO login_tokens (id, email, code_hash, next, expires_at) VALUES (?, ?, ?, ?, ?)').bind(
      id,
      email,
      await sha256(code + id),
      safeNext(body.next),
      new Date(now.getTime() + TTL).toISOString(),
    ),
    env.DB.prepare('INSERT INTO auth_events (kind, key, at) VALUES (?, ?, ?), (?, ?, ?)').bind('send-email', email, now.toISOString(), 'send-ip', ip, now.toISOString()),
  ])
  const link = `${new URL(request.url).origin}/api/auth/link?token=${token}`
  try {
    await sendSignIn(request, env, { email, code, link })
  } catch (e) {
    return error(e.message, 502)
  }
  return json({ sent: true })
}
