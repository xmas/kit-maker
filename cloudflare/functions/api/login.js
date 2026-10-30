// POST /api/login {email, password}: checks the email is allowed and the
// shared password matches, then sets the session cookie. Five failures in
// 15 minutes lock the email out for the rest of that window.
import { allowedEmails, configured, createToken, sameSecret, sessionCookie, userFor } from '../_lib/auth.js'
import { error, guard, json } from '../_lib/http.js'

const WINDOW = 15 * 60 * 1000
const LIMIT = 5

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  if (!configured(env)) return error('Sign-in isn’t set up: set ALLOWED_EMAILS, ADMIN_PASSWORD and JWT_SECRET for this Pages project.', 503)

  let body
  try {
    body = await request.json()
  } catch {
    return error('send JSON', 400)
  }
  const email = String(body.email || '').trim().toLowerCase()
  const password = String(body.password || '')

  const since = new Date(Date.now() - WINDOW).toISOString()
  const { n } = await env.DB.prepare('SELECT COUNT(*) AS n FROM login_failures WHERE email = ? AND at > ?').bind(email, since).first()
  if (n >= LIMIT) return error('Too many failed attempts. Try again in 15 minutes.', 429)

  const ok = allowedEmails(env).includes(email) && (await sameSecret(password, env.ADMIN_PASSWORD))
  if (!ok) {
    await env.DB.batch([
      env.DB.prepare('INSERT INTO login_failures (email, at) VALUES (?, ?)').bind(email, new Date().toISOString()),
      env.DB.prepare('DELETE FROM login_failures WHERE at < ?').bind(since),
    ])
    return error('Wrong email or password.', 401)
  }
  await env.DB.prepare('DELETE FROM login_failures WHERE email = ?').bind(email).run()
  const token = await createToken(email, env.JWT_SECRET)
  return json({ user: userFor(email), ai: Boolean(env.ANTHROPIC_API_KEY) }, 200, { 'Set-Cookie': sessionCookie(token) })
}
