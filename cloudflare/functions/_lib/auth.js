// Sign-in for the Cloudflare edition, in the style of a small Pages site:
// the people allowed in are ALLOWED_EMAILS, they share ADMIN_PASSWORD, and a
// signed cookie (JWT_SECRET, HMAC-SHA256) keeps them signed in for 12 hours.

const COOKIE = 'kitmaker_session'
const MAX_AGE = 12 * 60 * 60 // the server edition's absolute session limit
const enc = new TextEncoder()

export function allowedEmails(env) {
  return (env.ALLOWED_EMAILS || '')
    .split(',')
    .map((e) => e.trim().toLowerCase())
    .filter(Boolean)
}

export function configured(env) {
  return Boolean(env.ADMIN_PASSWORD && env.JWT_SECRET && allowedEmails(env).length)
}

function b64url(bytes) {
  let s = ''
  for (const b of new Uint8Array(bytes)) s += String.fromCharCode(b)
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function fromB64url(s) {
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(bin, (c) => c.charCodeAt(0))
}

async function key(secret) {
  return crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign', 'verify'])
}

export async function createToken(email, secret) {
  const payload = b64url(enc.encode(JSON.stringify({ email, exp: Math.floor(Date.now() / 1000) + MAX_AGE })))
  const sig = await crypto.subtle.sign('HMAC', await key(secret), enc.encode(payload))
  return `${payload}.${b64url(sig)}`
}

async function verifyToken(token, secret) {
  const [payload, sig] = (token || '').split('.')
  if (!payload || !sig) return null
  const ok = await crypto.subtle.verify('HMAC', await key(secret), fromB64url(sig), enc.encode(payload))
  if (!ok) return null
  const data = JSON.parse(new TextDecoder().decode(fromB64url(payload)))
  return data.exp > Date.now() / 1000 ? data : null
}

export function sessionCookie(token) {
  return `${COOKIE}=${token}; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=${MAX_AGE}`
}

export function clearCookie() {
  return `${COOKIE}=; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=0`
}

/** userFor turns an email into the user the app shows. */
export function userFor(email) {
  return { id: email, email, displayName: email.split('@')[0] }
}

/** currentUser returns the signed-in user, or null. */
export async function currentUser(request, env) {
  if (!configured(env)) return null
  const cookie = request.headers.get('Cookie') || ''
  const token = cookie
    .split(';')
    .map((c) => c.trim())
    .find((c) => c.startsWith(COOKIE + '='))
    ?.slice(COOKIE.length + 1)
  const data = await verifyToken(token, env.JWT_SECRET).catch(() => null)
  if (!data || !allowedEmails(env).includes(data.email)) return null
  return userFor(data.email)
}

/** sameSecret compares two strings in constant time. */
export async function sameSecret(a, b) {
  const [x, y] = await Promise.all([crypto.subtle.digest('SHA-256', enc.encode(a)), crypto.subtle.digest('SHA-256', enc.encode(b))])
  const p = new Uint8Array(x)
  const q = new Uint8Array(y)
  let diff = 0
  for (let i = 0; i < p.length; i++) diff |= p[i] ^ q[i]
  return diff === 0
}
