// POST /api/auth/passkey/register {response}: saves the new passkey.
import { verifyRegistrationResponse } from '@simplewebauthn/server'
import { b64url, clearChallenge, readChallenge, relyingParty } from '../../../_lib/auth.js'
import { error, guard, json, readJSON } from '../../../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [user, refused] = await guard(request, env)
  if (refused) return refused
  if (user.id === 'guest') return error('Guests can’t add passkeys. Sign in with your email first.', 403)
  const body = (await readJSON(request)) || {}
  const ch = await readChallenge(request, env)
  if (!ch || ch.kind !== 'register' || ch.email !== user.email) return error('The passkey request expired. Try again.', 400)
  const { rpID, origin } = relyingParty(request)
  let result
  try {
    result = await verifyRegistrationResponse({ response: body.response, expectedChallenge: ch.challenge, expectedOrigin: origin, expectedRPID: rpID })
  } catch (e) {
    return error(`The passkey couldn’t be added: ${e.message}`, 400)
  }
  if (!result.verified) return error('The passkey couldn’t be added.', 400)
  const c = result.registrationInfo.credential
  await env.DB.prepare('INSERT INTO passkeys (id, email, public_key, counter, transports, created_at) VALUES (?, ?, ?, ?, ?, ?)')
    .bind(c.id, user.email, b64url(c.publicKey), c.counter, JSON.stringify(c.transports || []), new Date().toISOString())
    .run()
  const { n } = await env.DB.prepare('SELECT COUNT(*) AS n FROM passkeys WHERE email = ?').bind(user.email).first()
  return json({ added: true, passkeys: n }, 200, { 'Set-Cookie': clearChallenge() })
}
