// GET /api/session: who is signed in, and whether AI is set up. The page
// also uses it to find out that this deployment has a backend at all.
import { currentUser, configured } from '../_lib/auth.js'
import { json } from '../_lib/http.js'

export async function onRequestGet({ request, env }) {
  const user = await currentUser(request, env)
  return json({ user, ai: Boolean(user && env.ANTHROPIC_API_KEY), configured: configured(env) })
}
