// POST /api/auth/passkey/login {response, next}: finishes a passkey sign-in.
import { verifyAuthenticationResponse } from '@simplewebauthn/server'
import { allowed, clearChallenge, fromB64url, readChallenge, relyingParty, safeNext, signIn, userFor } from '../../../_lib/auth.js'
import { error, guard, json, readJSON } from '../../../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  const body = (await readJSON(request)) || {}
  const ch = await readChallenge(request, env)
  if (!ch || ch.kind !== 'login') return error('The passkey request expired. Try again.', 400)
  const id = String(body.response?.id || '')
  const pk = await env.DB.prepare('SELECT id, email, public_key, counter, transports FROM passkeys WHERE id = ?').bind(id).first()
  if (!pk) return error('This passkey isn’t registered here. Sign in with an emailed code, then add it again.', 401)
  const { rpID, origin } = relyingParty(request)
  let result
  try {
    result = await verifyAuthenticationResponse({
      response: body.response,
      expectedChallenge: ch.challenge,
      expectedOrigin: origin,
      expectedRPID: rpID,
      credential: { id: pk.id, publicKey: fromB64url(pk.public_key), counter: pk.counter, transports: JSON.parse(pk.transports || '[]') },
    })
  } catch (e) {
    return error(`The passkey couldn’t be checked: ${e.message}`, 401)
  }
  if (!result.verified) return error('The passkey couldn’t be checked.', 401)
  if (!allowed(pk.email, env)) return error('This email address can’t sign in to this Kit Maker.', 403)
  await env.DB.prepare('UPDATE passkeys SET counter = ?, last_used_at = ? WHERE id = ?').bind(result.authenticationInfo.newCounter, new Date().toISOString(), pk.id).run()
  return json({ user: userFor(pk.email), next: safeNext(body.next) }, 200, [
    ['Set-Cookie', await signIn(env, pk.email)],
    ['Set-Cookie', clearChallenge()],
  ])
}
