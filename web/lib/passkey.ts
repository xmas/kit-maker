// Passkeys for the Cloudflare edition (cloudflare/functions/api/auth/passkey).
// The WebAuthn library loads only when someone uses a passkey.

async function post<T>(path: string, body: unknown = {}): Promise<T> {
  const res = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const out = (await res.json().catch(() => ({}))) as T & { error?: string }
  if (!res.ok) throw new Error(out.error ?? `request failed (${res.status})`)
  return out
}

export function passkeysSupported() {
  return typeof window !== 'undefined' && 'PublicKeyCredential' in window
}

/** cancelled is the browser's answer when someone closes the passkey prompt. */
export function cancelled(e: unknown) {
  return e instanceof Error && (e.name === 'NotAllowedError' || e.name === 'AbortError')
}

/** signInWithPasskey signs in and returns where to go next. */
export async function signInWithPasskey(next: string): Promise<string> {
  const { startAuthentication } = await import('@simplewebauthn/browser')
  const optionsJSON = await post<Parameters<typeof startAuthentication>[0]['optionsJSON']>('/api/auth/passkey/login-options')
  const response = await startAuthentication({ optionsJSON })
  return (await post<{ next: string }>('/api/auth/passkey/login', { response, next })).next
}

/** addPasskey registers a passkey for the signed-in person. */
export async function addPasskey(): Promise<number> {
  const { startRegistration } = await import('@simplewebauthn/browser')
  const optionsJSON = await post<Parameters<typeof startRegistration>[0]['optionsJSON']>('/api/auth/passkey/register-options')
  const response = await startRegistration({ optionsJSON })
  return (await post<{ passkeys: number }>('/api/auth/passkey/register', { response })).passkeys
}

/** emailCode sends a sign-in code and link to email. */
export async function emailCode(email: string, next: string) {
  await post('/api/auth/email', { email, next })
}

/** signInWithCode signs in with an emailed code and returns where to go next. */
export async function signInWithCode(email: string, code: string): Promise<string> {
  return (await post<{ next: string }>('/api/auth/code', { email, code })).next
}
