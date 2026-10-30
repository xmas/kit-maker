import { currentUser } from './auth.js'

/** json answers with body; headers is an object or a list of [name, value] (for several Set-Cookie). */
export function json(body, status = 200, headers = {}) {
  const h = new Headers({ 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
  for (const [k, v] of Array.isArray(headers) ? headers : Object.entries(headers)) h.append(k, v)
  return new Response(JSON.stringify(body), { status, headers: h })
}

/** readJSON parses a JSON body, or returns null. */
export async function readJSON(request) {
  try {
    return await request.json()
  } catch {
    return null
  }
}

export function error(message, status) {
  return json({ error: message }, status)
}

/**
 * guard checks a request before a handler runs: writes must be JSON from
 * this origin (no cross-site form posts), and the person must be signed in
 * unless open is set. It returns [user, errorResponse].
 */
export async function guard(request, env, { open = false } = {}) {
  if (request.method !== 'GET' && request.method !== 'HEAD') {
    const origin = request.headers.get('Origin')
    if (origin && origin !== new URL(request.url).origin) return [null, error('cross-site request refused', 403)]
    if (!(request.headers.get('Content-Type') || '').startsWith('application/json')) return [null, error('send JSON', 415)]
  }
  const user = await currentUser(request, env)
  if (!user && !open) return [null, error('sign in first', 401)]
  return [user, null]
}
