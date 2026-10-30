// POST /api/publish {blueprintId, slug, kind, name, version, yaml, files}: starts
// publishing one blueprint version. The engine compiled the descriptor and
// picked the number; this records it as pending and starts the publish-kit
// workflow on GitHub, which builds it with the kit spec's BuildKit frontend,
// pushes <registry>/sbx-kit-<slug>:vN and :latest, and reports back to
// /api/publish/callback with a one-use token.
import { randomToken, sha256 } from '../_lib/auth.js'
import { error, guard, json, readJSON } from '../_lib/http.js'
import { publishConfig, repositoryFor, STALE_MS } from '../_lib/publish.js'

async function gzipBase64(text) {
  const stream = new Blob([text]).stream().pipeThrough(new CompressionStream('gzip'))
  const bytes = new Uint8Array(await new Response(stream).arrayBuffer())
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s)
}

export async function onRequestPost({ request, env }) {
  const [user, refused] = await guard(request, env)
  if (refused) return refused
  if (user.id === 'guest') return error('Guests can’t publish. Sign in with your email to publish.', 403)
  const cfg = publishConfig(env)
  if (cfg.missing.length) return error(`Publishing isn’t set up: set ${cfg.missing.join(', ')} for this Pages project.`, 503)

  const b = (await readJSON(request)) || {}
  const id = String(b.blueprintId || '')
  const slug = String(b.slug || '')
  const version = Number(b.version)
  const yaml = String(b.yaml || '')
  if (!/^[A-Za-z0-9_-]{1,120}$/.test(id) || !/^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$/.test(slug) || !Number.isInteger(version) || version < 1) {
    return error('a publish request is malformed', 400)
  }
  if (!yaml.startsWith('# syntax=docker/sandbox-kit:3')) return error('the descriptor must start with # syntax=docker/sandbox-kit:3', 400)
  // The Dockerfile's build files, written beside the descriptor by the workflow.
  const files = b.files && typeof b.files === 'object' ? b.files : {}
  for (const [name, content] of Object.entries(files)) {
    const parts = name.split('/')
    if (typeof content !== 'string' || !name || name.startsWith('/') || name.includes('\\') || parts.some((p) => p === '' || p === '.' || p === '..') || name === `${slug}.yaml`) {
      return error(`build file ${JSON.stringify(name)} must be a relative path inside the build folder`, 400)
    }
  }

  // One publish at a time per blueprint, and the number must be the next one.
  const { results } = await env.DB.prepare('SELECT version, status, created_at FROM blueprint_versions WHERE blueprint_id = ? ORDER BY version DESC').bind(id).all()
  const live = results.filter((r) => r.status === 'published' || (r.status === 'pending' && Date.now() - Date.parse(r.created_at) < STALE_MS))
  if (live.some((r) => r.status === 'pending')) return error(`v${live.find((r) => r.status === 'pending').version} is still publishing.`, 409)
  const next = (live[0]?.version ?? 0) + 1
  if (version !== next) return error(`The next version is v${next}; reload and publish again.`, 409)

  const token = randomToken()
  const repository = repositoryFor(cfg.registry, slug)
  const now = new Date().toISOString()
  await env.DB.batch([
    env.DB.prepare("DELETE FROM blueprint_versions WHERE blueprint_id = ? AND version >= ? AND status != 'published'").bind(id, version),
    env.DB.prepare(
      "INSERT INTO blueprint_versions (blueprint_id, version, slug, kind, name, repository, yaml, status, token_hash, published_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)",
    ).bind(id, version, slug, b.kind === 'mixin' ? 'mixin' : 'workload', String(b.name || slug).slice(0, 200), repository, yaml, await sha256(token), user.email, now),
  ])

  const payload = await gzipBase64(
    JSON.stringify({ id: `${id}:${version}`, slug, version, yaml, files, repository, callback: `${new URL(request.url).origin}/api/publish/callback`, token }),
  )
  if (payload.length > 60_000) {
    await env.DB.prepare("UPDATE blueprint_versions SET status = 'failed', error = ? WHERE blueprint_id = ? AND version = ?").bind('The descriptor and its build files are too large to send to GitHub Actions.', id, version).run()
    return error('The descriptor and its build files are too large to send to GitHub Actions (60 KB compressed at most).', 413)
  }
  const res = await fetch(`https://api.github.com/repos/${cfg.repo}/actions/workflows/publish-kit.yml/dispatches`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${cfg.token}`,
      Accept: 'application/vnd.github+json',
      'X-GitHub-Api-Version': '2022-11-28',
      'User-Agent': 'kit-maker',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ ref: cfg.ref, inputs: { request: payload } }),
  })
  if (!res.ok) {
    const detail = (await res.text()).slice(0, 300)
    console.log(`[kitmaker] GitHub refused the publish workflow: ${res.status} ${detail}`)
    const why =
      res.status === 404
        ? `GitHub couldn’t find .github/workflows/publish-kit.yml on ${cfg.ref} in ${cfg.repo}, or the token can’t see that repo.`
        : res.status === 401 || res.status === 403
          ? 'GitHub refused PUBLISH_GITHUB_TOKEN: it needs Actions read and write on that repo.'
          : `GitHub refused to start the publish workflow (${res.status}).`
    await env.DB.prepare("UPDATE blueprint_versions SET status = 'failed', error = ?, finished_at = ? WHERE blueprint_id = ? AND version = ?").bind(why, now, id, version).run()
    return error(why, 502)
  }
  return json({ started: true, version, repository, runsUrl: `https://github.com/${cfg.repo}/actions/workflows/publish-kit.yml` })
}
