// The Cloudflare edition's entry. There is no server: the Kit Maker engine
// (internal/lite, compiled to kitmaker.wasm) answers every app route in the
// page. This file loads it with the starter content and the stored
// documents, sends Inertia's requests (axios), the components' fetch()
// calls and download links to it, stores what each request changed, and
// mounts the same Vue app as the server edition.
import '../styles.css'
import axios, { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import type { Page } from '@inertiajs/core'
import { startApp } from '../inertia'
import { connect, ConflictError, netFetch, type Backend, type Doc } from './backend'

interface EngineResponse {
  status: number
  headers: Record<string, string>
  body: Uint8Array
  writes: string
  user: string | null
}
interface Engine {
  init(starter: string, mode: string, user: string | null, version: string): string | null
  load(docs: string): Promise<void>
  saved(revs: string): Promise<void>
  setUser(user: string | null): void
  handle(req: { method: string; url: string; headers: Record<string, string>; body?: string }): Promise<EngineResponse>
}
declare global {
  interface Window {
    Go: new () => { importObject: WebAssembly.Imports; run(instance: WebAssembly.Instance): Promise<void> }
    kitmakerEngine: Engine
    kitmakerHost: unknown
  }
}

const decoder = new TextDecoder()
let engine: Engine
let backend: Backend
let lastSync = 0

/** Paths the engine answers; everything else goes to the network. */
function isAppPath(path: string) {
  return /^\/(blueprints|blocks|login|logout)(\/|$)/.test(path) || path === '/'
}

async function load(docs: Doc[]) {
  if (docs.length) await engine.load(JSON.stringify(docs))
}

/**
 * sync loads stored documents: other people's changes before a page is
 * shown (shared), or this browser's documents once (local).
 */
async function sync(force = false) {
  if (!backend.user) return
  if (!force && Date.now() - lastSync < 3000) return
  lastSync = Date.now()
  await load(await backend.changes())
}

async function persist(writes: string) {
  const docs = JSON.parse(writes) as Doc[]
  if (!docs.length) return
  const revs = await backend.write(docs)
  await engine.saved(JSON.stringify(revs))
}

/**
 * dispatch runs one request through the engine, stores its changes and
 * follows redirects the way a browser does (303 → GET).
 */
async function dispatch(method: string, url: string, headers: Record<string, string>, body?: string): Promise<{ res: EngineResponse; url: string }> {
  if (method.toUpperCase() === 'GET' && headers['X-Inertia']) await sync().catch(() => {})
  for (let hops = 0; hops < 5; hops++) {
    const res = await engine.handle({ method, url, headers, body })
    try {
      await persist(res.writes)
    } catch (e) {
      if (!(e instanceof ConflictError)) throw e
      // Someone else saved first: take their version and show the page again.
      await load(e.docs)
      await sync(true).catch(() => {})
      const back = new URL(url, location.origin).pathname
      return dispatch('GET', isAppPath(back) && method === 'GET' ? url : location.pathname, headers)
    }
    const to = res.headers['location']
    if (res.status >= 300 && res.status < 400 && to) {
      method = 'GET'
      url = to
      body = undefined
      if (headers['X-Inertia']) await sync().catch(() => {})
      continue
    }
    return { res, url }
  }
  throw new Error('too many redirects')
}

const adapter: AxiosAdapter = async (config: InternalAxiosRequestConfig) => {
  const url = new URL(config.url ?? '/', location.origin)
  const headers: Record<string, string> = {}
  for (const [k, v] of Object.entries(AxiosHeaders.from(config.headers).toJSON())) {
    if (typeof v === 'string') headers[k] = v
  }
  const body = typeof config.data === 'string' ? config.data : config.data ? JSON.stringify(config.data) : undefined
  if (body && !headers['Content-Type']) headers['Content-Type'] = 'application/json'
  const { res } = await dispatch((config.method ?? 'get').toUpperCase(), url.pathname + url.search, headers, body)
  const response = { data: decoder.decode(res.body), status: res.status, statusText: String(res.status), headers: res.headers, config, request: {} }
  if (res.status >= 200 && res.status < 300) return response
  throw new AxiosError(`Request failed with status code ${res.status}`, AxiosError.ERR_BAD_RESPONSE, config, {}, response)
}

function patchFetch() {
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = new Request(input, init)
    const url = new URL(req.url)
    if (url.origin !== location.origin || !isAppPath(url.pathname)) return netFetch(input, init)
    const headers: Record<string, string> = {}
    req.headers.forEach((v, k) => (headers[k] = v))
    const body = req.method === 'GET' || req.method === 'HEAD' ? undefined : await req.text()
    const { res } = await dispatch(req.method, url.pathname + url.search, headers, body)
    return new Response(res.status === 204 ? null : (res.body as BodyInit), { status: res.status, headers: res.headers })
  }
}

/** Download links (the tar.gz, sbxenv.yaml) are built by the engine too. */
function interceptDownloads() {
  document.addEventListener(
    'click',
    async (ev) => {
      const a = (ev.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null
      if (!a || a.origin !== location.origin || !/\/(download|sbxenv\.yaml)$/.test(a.pathname)) return
      ev.preventDefault()
      const { res } = await dispatch('GET', a.pathname, {})
      if (res.status !== 200) {
        alert(decoder.decode(res.body))
        return
      }
      const name = /filename="([^"]+)"/.exec(res.headers['content-disposition'] ?? '')?.[1] ?? 'download'
      const href = URL.createObjectURL(new Blob([res.body as BlobPart], { type: res.headers['content-type'] }))
      const tmp = Object.assign(document.createElement('a'), { href, download: name })
      document.body.append(tmp)
      tmp.click()
      tmp.remove()
      setTimeout(() => URL.revokeObjectURL(href), 10_000)
    },
    true,
  )
}

/** The host is what the engine calls back for: sign-in, history and AI. */
function installHost() {
  window.kitmakerHost = {
    get aiEnabled() {
      return backend.ai && backend.user !== null
    },
    async login(email: string, password: string) {
      const user = await backend.login(email, password)
      lastSync = 0 // load what they may see on the next page
      return JSON.stringify(user)
    },
    async logout() {
      await backend.logout()
    },
    async revisions(id: string) {
      return JSON.stringify(await backend.revisions(id))
    },
    async ai(request: string, onDelta: ((text: string) => void) | null) {
      const res = await netFetch('/api/ai', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: request })
      if (!res.ok) {
        const err = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(err.error ?? `AI request failed (${res.status})`)
      }
      if (!onDelta) return await res.text()
      // A stream of server-sent events: delta …, then done with the response.
      const reader = res.body!.getReader()
      let buf = ''
      let final = '{}'
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        let i
        while ((i = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, i)
          buf = buf.slice(i + 2)
          const event = /^event: (.*)$/m.exec(block)?.[1]
          const data = /^data: (.*)$/m.exec(block)?.[1] ?? '{}'
          if (event === 'delta') onDelta((JSON.parse(data) as { text: string }).text)
          else if (event === 'done') final = data
          else if (event === 'error') throw new Error((JSON.parse(data) as { message: string }).message)
        }
      }
      return final
    },
  }
}

async function loadEngine(version: string) {
  const ready = new Promise<void>((resolve) => ((window as unknown as { kitmakerReady: () => void }).kitmakerReady = resolve))
  const go = new window.Go()
  const [{ instance }, starter] = await Promise.all([
    WebAssembly.instantiateStreaming(netFetch(`/kitmaker.wasm?v=${version}`), go.importObject),
    netFetch(`/starter.json?v=${version}`).then((r) => r.text()),
  ])
  void go.run(instance)
  await ready
  engine = window.kitmakerEngine
  const err = engine.init(starter, backend.mode, backend.user ? JSON.stringify(backend.user) : null, version)
  if (err) throw new Error(err)
}

async function boot() {
  const version = import.meta.env.VITE_BUILD ?? 'dev'
  backend = await connect()
  installHost()
  await loadEngine(version)
  if (backend.user) await sync(true)
  axios.defaults.adapter = adapter
  patchFetch()
  interceptDownloads()

  const { res, url } = await dispatch('GET', location.pathname + location.search, { 'X-Inertia': 'true' })
  const page = JSON.parse(decoder.decode(res.body)) as Page
  if (url !== location.pathname + location.search) history.replaceState(history.state, '', page.url)
  document.getElementById('boot')?.remove()
  await startApp(page)
}

boot().catch((e: Error) => {
  const el = document.getElementById('boot')
  if (el) el.textContent = `Kit Maker couldn't start: ${e.message}`
  console.error(e)
})
