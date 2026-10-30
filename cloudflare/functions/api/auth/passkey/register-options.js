// POST /api/auth/passkey/register-options: starts adding a passkey for the
// signed-in person.
import { generateRegistrationOptions } from '@simplewebauthn/server'
import { challengeCookie, relyingParty } from '../../../_lib/auth.js'
import { error, guard, json } from '../../../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [user, refused] = await guard(request, env)
  if (refused) return refused
  if (user.id === 'guest') return error('Guests can’t add passkeys. Sign in with your email first.', 403)
  const { results } = await env.DB.prepare('SELECT id, transports FROM passkeys WHERE email = ?').bind(user.email).all()
  // The WebAuthn user handle: stable per email, and not the email itself.
  const userID = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode('kitmaker:' + user.email)))
  const options = await generateRegistrationOptions({
    rpName: 'Kit Maker',
    rpID: relyingParty(request).rpID,
    userName: user.email,
    userID,
    attestationType: 'none',
    excludeCredentials: results.map((r) => ({ id: r.id, transports: JSON.parse(r.transports || '[]') })),
    authenticatorSelection: { residentKey: 'required', userVerification: 'preferred' },
  })
  return json(options, 200, { 'Set-Cookie': await challengeCookie(env, { challenge: options.challenge, kind: 'register', email: user.email }) })
}
