// GET /api/session: who is signed in, whether AI is set up, and how many
// passkeys they have. The page also uses it to find out that this
// deployment has a backend at all.
import { currentUser, missingSettings } from '../_lib/auth.js'
import { json } from '../_lib/http.js'

export async function onRequestGet({ request, env }) {
  const user = await currentUser(request, env)
  let passkeys = 0
  if (user) passkeys = (await env.DB.prepare('SELECT COUNT(*) AS n FROM passkeys WHERE email = ?').bind(user.email).first()).n
  const missing = missingSettings(env, request)
  return json({ user, ai: Boolean(user && env.ANTHROPIC_API_KEY), passkeys, configured: missing.length === 0, missing })
}
