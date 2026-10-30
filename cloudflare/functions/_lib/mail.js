// Sending sign-in emails through Resend (RESEND_API_KEY, MAIL_FROM such as
// "Kit Maker <login@ezdata.xyz>"); its free plan covers 3,000 a month.
// Cloudflare's own Email Service would need the Workers Paid plan to send
// to any address. Under wrangler pages dev with no key, the message is
// printed to the console instead.
import { isLocal, mailConfigured } from './auth.js'

function escapeHTML(s) {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])
}

export async function sendSignIn(request, env, { email, code, link }) {
  const subject = `${code} is your Kit Maker sign-in code`
  const text = `Your Kit Maker sign-in code is ${code}\n\nOr open this link to sign in:\n${link}\n\nBoth work once and expire in 15 minutes. If you didn't ask to sign in, ignore this email.`
  const html = `<p>Your Kit Maker sign-in code is</p><p style="font-size:24px;font-weight:600;letter-spacing:4px">${code}</p><p>Or <a href="${escapeHTML(link)}">sign in with this link</a>.</p><p style="color:#64748b">Both work once and expire in 15 minutes. If you didn't ask to sign in, ignore this email.</p>`

  if (!mailConfigured(env)) {
    if (!isLocal(request)) throw new Error('Sending email isn’t set up: set RESEND_API_KEY and MAIL_FROM for this Pages project.')
    console.log(`[kitmaker] sign-in email for ${email}: code ${code}, link ${link}`)
    return
  }
  const res = await fetch('https://api.resend.com/emails', {
    method: 'POST',
    headers: { Authorization: `Bearer ${env.RESEND_API_KEY}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ from: env.MAIL_FROM, to: [email], subject, text, html }),
  })
  if (!res.ok) {
    console.log(`[kitmaker] Resend refused the sign-in email: ${res.status} ${await res.text()}`)
    throw new Error('The sign-in email couldn’t be sent. Try again in a moment.')
  }
}
