// POST /api/auth/passkey/login-options: starts a passkey sign-in. The
// browser offers whichever of this site's passkeys the person has.
import { generateAuthenticationOptions } from '@simplewebauthn/server'
import { challengeCookie, relyingParty } from '../../../_lib/auth.js'
import { error, guard, json } from '../../../_lib/http.js'

export async function onRequestPost({ request, env }) {
  const [, refused] = await guard(request, env, { open: true })
  if (refused) return refused
  if (!env.JWT_SECRET) return error('Sign-in isn’t set up: set JWT_SECRET for this Pages project.', 503)
  const options = await generateAuthenticationOptions({ rpID: relyingParty(request).rpID, userVerification: 'preferred' })
  return json(options, 200, { 'Set-Cookie': await challengeCookie(env, { challenge: options.challenge, kind: 'login' }) })
}
