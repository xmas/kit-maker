// /api/auth/link: the emailed sign-in link.
//
// GET shows a one-button page instead of signing in straight away: mail
// scanners open links to check them, and that mustn't use up the sign-in.
// POST (the button) signs in and goes to the page the person asked for.
import { allowed, sha256, signIn } from '../../_lib/auth.js'

const page = (body) =>
  new Response(
    `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sign in · Kit Maker</title>
<style>body{font:15px system-ui,sans-serif;background:#F6F7F9;color:#0f172a;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
main{background:#fff;border-radius:12px;padding:32px;max-width:360px;box-shadow:0 1px 3px rgba(15,23,42,.1);text-align:center}
button{background:#1D63ED;color:#fff;border:0;border-radius:8px;padding:10px 18px;font:inherit;font-weight:600;cursor:pointer;width:100%}
p{color:#475569}a{color:#1D63ED}</style></head><body><main>${body}</main></body></html>`,
    {
      headers: {
        'Content-Type': 'text/html; charset=utf-8',
        'Cache-Control': 'no-store',
        // same-origin, not no-referrer: no-referrer makes the form post send Origin: null.
        'Referrer-Policy': 'same-origin',
        'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'",
      },
    },
  )

const expired = () => page('<h1>This link has expired</h1><p>Sign-in links work once, for 15 minutes.</p><p><a href="/login">Send a new one</a></p>')

async function find(env, token) {
  if (!/^[A-Za-z0-9_-]{20,100}$/.test(token || '')) return null
  return env.DB.prepare('SELECT id, email, next FROM login_tokens WHERE id = ? AND expires_at > ?').bind(await sha256(token), new Date().toISOString()).first()
}

export async function onRequestGet({ request, env }) {
  const token = new URL(request.url).searchParams.get('token')
  const t = await find(env, token)
  if (!t) return expired()
  const esc = t.email.replace(/[&<>"']/g, '')
  return page(
    `<h1>Sign in to Kit Maker</h1><p>as <strong>${esc}</strong></p><form method="post" action="/api/auth/link"><input type="hidden" name="token" value="${token}"><button type="submit">Sign in</button></form>`,
  )
}

export async function onRequestPost({ request, env }) {
  const origin = request.headers.get('Origin')
  if (origin && origin !== new URL(request.url).origin) return new Response('cross-site request refused', { status: 403 })
  const form = await request.formData().catch(() => null)
  const t = await find(env, form?.get('token'))
  if (!t) return expired()
  if (!allowed(t.email, env)) return page('<h1>Not allowed</h1><p>This email address can’t sign in to this Kit Maker.</p>')
  await env.DB.prepare('DELETE FROM login_tokens WHERE id = ?').bind(t.id).run()
  return new Response(null, { status: 303, headers: { Location: t.next, 'Set-Cookie': await signIn(env, t.email) } })
}
