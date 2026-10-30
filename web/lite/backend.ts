// Where the Cloudflare edition keeps documents. With the Pages Functions
// deployed (cloudflare/functions), they live in D1 and everyone signed in
// shares them; without them, in this browser's IndexedDB.

export interface Doc {
  kind: string
  id: string
  parent?: string
  rev: number
  body: unknown
}

export interface User {
  id: string
  email: string
  displayName: string
}

export class ConflictError extends Error {
  constructor(
    message: string,
    public docs: Doc[],
  ) {
    super(message)
  }
}

export interface Backend {
  mode: 'shared' | 'local'
  user: User | null
  ai: boolean
  /** Documents changed since the last call (all of them the first time), revisions excluded. */
  changes(): Promise<Doc[]>
  revisions(blueprintId: string): Promise<Doc[]>
  /** Stores documents; returns their new revs by "kind/id". */
  write(docs: Doc[]): Promise<Record<string, number>>
  login(email: string, password: string): Promise<User>
  logout(): Promise<void>
  /** Publishing (shared only): the state as internal/lite.PublishState JSON. */
  versions(blueprintId: string): Promise<string>
  publish(request: string): Promise<string>
}

async function json<T>(res: Response): Promise<T> {
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((body as { error?: string }).error ?? `request failed (${res.status})`)
  return body as T
}

// The real fetch: web/lite/boot.ts routes app paths to the engine.
const netFetch = window.fetch.bind(window)

export { netFetch }

class Shared implements Backend {
  mode = 'shared' as const
  private since = ''
  constructor(
    public user: User | null,
    public ai: boolean,
  ) {}

  async changes() {
    const out = await json<{ docs: Doc[]; now: string }>(await netFetch(`/api/docs?since=${encodeURIComponent(this.since)}`))
    this.since = out.now
    return out.docs
  }
  async revisions(blueprintId: string) {
    return (await json<{ docs: Doc[] }>(await netFetch(`/api/revisions?blueprint=${encodeURIComponent(blueprintId)}`))).docs
  }
  async write(docs: Doc[]) {
    const res = await netFetch('/api/docs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ docs }) })
    if (res.status === 409) {
      const body = (await res.json()) as { error: string; docs: Doc[] }
      throw new ConflictError(body.error, body.docs)
    }
    return (await json<{ revs: Record<string, number> }>(res)).revs
  }
  // Sign-in happens on the login page (an emailed code or a passkey, see
  // components/auth/PasswordlessSignIn.vue), which reloads the app.
  async login(): Promise<User> {
    throw new Error('Sign in with an emailed code or a passkey.')
  }
  async logout() {
    await netFetch('/api/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
    this.user = null
  }
  async versions(blueprintId: string) {
    const res = await netFetch(`/api/versions?blueprint=${encodeURIComponent(blueprintId)}`)
    if (!res.ok) throw new Error(`couldn't read versions (${res.status})`)
    return await res.text()
  }
  async publish(request: string) {
    const res = await netFetch('/api/publish', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: request })
    await json(res)
    return this.versions((JSON.parse(request) as { blueprintId: string }).blueprintId)
  }
}

// Local keeps documents in IndexedDB, keyed "kind/id".
class Local implements Backend {
  mode = 'local' as const
  user: User = { id: 'local', email: 'you@this-browser', displayName: 'You' }
  ai = false
  private loaded = false
  private db = new Promise<IDBDatabase>((resolve, reject) => {
    const req = indexedDB.open('kitmaker', 1)
    req.onupgradeneeded = () => req.result.createObjectStore('docs')
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })

  private async tx<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T> | void): Promise<T | undefined> {
    const db = await this.db
    return new Promise((resolve, reject) => {
      const t = db.transaction('docs', mode)
      const req = fn(t.objectStore('docs'))
      t.oncomplete = () => resolve(req ? req.result : undefined)
      t.onerror = () => reject(t.error)
    })
  }
  private async all() {
    return ((await this.tx('readonly', (s) => s.getAll())) ?? []) as Doc[]
  }
  async changes() {
    if (this.loaded) return [] // nobody else writes here
    this.loaded = true
    return (await this.all()).filter((d) => d.kind !== 'revision')
  }
  async revisions(blueprintId: string) {
    return (await this.all()).filter((d) => d.kind === 'revision' && d.parent === blueprintId)
  }
  async write(docs: Doc[]) {
    const revs: Record<string, number> = {}
    await this.tx('readwrite', (s) => {
      for (const d of docs) {
        const key = `${d.kind}/${d.id}`
        revs[key] = d.rev + 1
        s.put({ ...d, rev: d.rev + 1 }, key)
      }
    })
    return revs
  }
  async login(): Promise<User> {
    return this.user
  }
  async logout() {}
  async versions(): Promise<string> {
    throw new Error('no backend')
  }
  async publish(): Promise<string> {
    throw new Error('Publishing needs this site’s backend.')
  }
}

/** connect finds the backend: the Pages Functions when deployed, else this browser. */
export async function connect(): Promise<Backend> {
  try {
    const res = await netFetch('/api/session', { headers: { Accept: 'application/json' } })
    if (res.ok && res.headers.get('content-type')?.includes('application/json')) {
      const s = (await res.json()) as { user: User | null; ai: boolean }
      return new Shared(s.user, s.ai)
    }
  } catch {
    // no backend
  }
  return new Local()
}
