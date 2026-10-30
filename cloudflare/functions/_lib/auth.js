// Sign-in for the Cloudflare edition, without passwords: anyone who proves
// they own an email address (an emailed code or link) may sign in, unless
// ALLOWED_EMAILS narrows it, and can then add a passkey for next time. A
// signed cookie (JWT_SECRET, HMAC-SHA256) keeps them signed in for 12 hours.

const COOKIE = 'kitmaker_session'
const CHALLENGE = 'kitmaker_webauthn'
const MAX_AGE = 12 * 60 * 60 // the server edition's absolute session limit
const enc = new TextEncoder()

export function normalizeEmail(email) {
  return String(email || '').trim().toLowerCase()
}

export function validEmail(email) {
  return /^[^\s@]{1,64}@[^\s@]+\.[^\s@]{2,}$/.test(email) && email.length <= 254
}

/**
 * allowed reports whether an email may sign in. ALLOWED_EMAILS is optional:
 * empty allows any verified email; otherwise a comma-separated list of
 * addresses and domains (@ezdata.xyz).
 */
export function allowed(email, env) {
  const list = (env.ALLOWED_EMAILS || '')
    .split(',')
    .map((e) => e.trim().toLowerCase())
    .filter(Boolean)
  if (!list.length) return true
  return list.some((rule) => (rule.startsWith('@') || rule.startsWith('*@') ? email.endsWith(rule.slice(rule.indexOf('@'))) : email === rule))
}

/** isLocal is wrangler pages dev, where sign-in emails go to the console. */
export function isLocal(request) {
  const host = new URL(request.url).hostname
  return host === 'localhost' || host === '127.0.0.1'
}

export function mailConfigured(env) {
  return Boolean(env.RESEND_API_KEY && env.MAIL_FROM)
}

/**
 * missingSettings names the sign-in secrets that are unset or saved empty
 * (never their values), e.g. ["JWT_SECRET (empty)", "MAIL_FROM (unset)"].
 */
export function missingSettings(env, request) {
  const need = isLocal(request) ? ['JWT_SECRET'] : ['JWT_SECRET', 'RESEND_API_KEY', 'MAIL_FROM']
  return need.filter((k) => !String(env[k] || '').trim()).map((k) => `${k} (${env[k] === undefined ? 'unset' : 'empty'})`)
}

export function configured(env, request) {
  return missingSettings(env, request).length === 0
}

export function b64url(bytes) {
  let s = ''
  for (const b of new Uint8Array(bytes)) s += String.fromCharCode(b)
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export function fromB64url(s) {
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(bin, (c) => c.charCodeAt(0))
}

export async function sha256(text) {
  return b64url(await crypto.subtle.digest('SHA-256', enc.encode(text)))
}

export function randomToken(bytes = 32) {
  return b64url(crypto.getRandomValues(new Uint8Array(bytes)))
}

async function key(secret) {
  return crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign', 'verify'])
}

/** sign makes a tamper-proof token holding data until maxAge seconds from now. */
async function sign(data, secret, maxAge) {
  const payload = b64url(enc.encode(JSON.stringify({ ...data, exp: Math.floor(Date.now() / 1000) + maxAge })))
  const sig = await crypto.subtle.sign('HMAC', await key(secret), enc.encode(payload))
  return `${payload}.${b64url(sig)}`
}

async function verify(token, secret) {
  const [payload, sig] = (token || '').split('.')
  if (!payload || !sig) return null
  const ok = await crypto.subtle.verify('HMAC', await key(secret), fromB64url(sig), enc.encode(payload))
  if (!ok) return null
  const data = JSON.parse(new TextDecoder().decode(fromB64url(payload)))
  return data.exp > Date.now() / 1000 ? data : null
}

function cookie(request, name) {
  return (request.headers.get('Cookie') || '')
    .split(';')
    .map((c) => c.trim())
    .find((c) => c.startsWith(name + '='))
    ?.slice(name.length + 1)
}

function setCookie(name, value, maxAge, path = '/') {
  return `${name}=${value}; Path=${path}; HttpOnly; Secure; SameSite=Strict; Max-Age=${maxAge}`
}

export function clearCookie() {
  return setCookie(COOKIE, '', 0)
}

/** userFor turns an email into the user the app shows. */
export function userFor(email) {
  return { id: email, email, displayName: email.split('@')[0] }
}

/**
 * signIn records the user and returns the Set-Cookie header for a new
 * session.
 */
export async function signIn(env, email) {
  const now = new Date().toISOString()
  await env.DB.prepare('INSERT INTO users (email, created_at, last_seen_at) VALUES (?1, ?2, ?2) ON CONFLICT (email) DO UPDATE SET last_seen_at = ?2')
    .bind(email, now)
    .run()
  return setCookie(COOKIE, await sign({ email }, env.JWT_SECRET, MAX_AGE), MAX_AGE)
}

/** currentUser returns the signed-in user, or null. */
export async function currentUser(request, env) {
  if (!env.JWT_SECRET) return null
  const data = await verify(cookie(request, COOKIE), env.JWT_SECRET).catch(() => null)
  if (!data || !allowed(data.email, env)) return null
  return userFor(data.email)
}

/** A WebAuthn challenge lives in a short signed cookie between the two steps. */
export async function challengeCookie(env, data) {
  return setCookie(CHALLENGE, await sign(data, env.JWT_SECRET, 300), 300, '/api/auth/passkey')
}

export async function readChallenge(request, env) {
  return verify(cookie(request, CHALLENGE), env.JWT_SECRET).catch(() => null)
}

export function clearChallenge() {
  return setCookie(CHALLENGE, '', 0, '/api/auth/passkey')
}

/** safeNext keeps post-sign-in redirects on this site. */
export function safeNext(next) {
  const n = String(next || '')
  return n.startsWith('/') && !n.startsWith('//') && !n.startsWith('/api/') ? n : '/blueprints'
}

/** The relying party for passkeys is this site's host name. */
export function relyingParty(request) {
  const url = new URL(request.url)
  return { rpID: url.hostname, origin: url.origin }
}
