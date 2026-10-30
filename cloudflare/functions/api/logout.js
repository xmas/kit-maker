// POST /api/logout: clears the session cookie.
import { clearCookie } from '../_lib/auth.js'
import { guard, json } from '../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  return json({ ok: true }, 200, { 'Set-Cookie': clearCookie() })
}
