// Publishing through GitHub Actions: the Function starts the publish-kit
// workflow in PUBLISH_GITHUB_REPO with a fine-grained PUBLISH_GITHUB_TOKEN
// (Actions: read and write on that repo), and the workflow pushes to GHCR.

/** A pending publish older than this is shown as failed (the workflow never reported; it times out at 45 minutes). */
export const STALE_MS = 50 * 60 * 1000

export function publishConfig(env) {
  const repo = String(env.PUBLISH_GITHUB_REPO || '').trim()
  const token = String(env.PUBLISH_GITHUB_TOKEN || '').trim()
  const missing = []
  if (!repo) missing.push('PUBLISH_GITHUB_REPO')
  if (!token) missing.push('PUBLISH_GITHUB_TOKEN')
  const owner = repo.split('/')[0]?.toLowerCase()
  return {
    missing,
    repo,
    token,
    ref: String(env.PUBLISH_GITHUB_REF || 'main').trim(),
    // GHCR names are lowercase; the workflow's token can push only under the repo owner.
    registry: String(env.PUBLISH_REGISTRY || (owner ? `ghcr.io/${owner}` : '')).trim().replace(/\/+$/, '').toLowerCase(),
  }
}

export function repositoryFor(registry, slug) {
  return `${registry}/sbx-kit-${slug}`
}

/** rows turns blueprint_versions rows into what the engine reads, newest first. */
export function versionsJSON(results) {
  const now = Date.now()
  let yamlSent = false
  return results.map((r) => {
    let status = r.status
    let error = r.error
    if (status === 'pending' && now - Date.parse(r.created_at) > STALE_MS) {
      status = 'failed'
      error = 'The publish workflow never reported back. Check the Actions tab on GitHub.'
    }
    const v = {
      version: r.version,
      repository: r.repository,
      digest: r.digest || '',
      status,
      error: error || '',
      runUrl: r.run_url || '',
      publishedAt: r.finished_at || r.created_at,
      startedAt: r.created_at,
      publishedBy: r.published_by || '',
    }
    // The newest published version's YAML lets the engine see "nothing changed".
    if (status === 'published' && !yamlSent) {
      v.yaml = r.yaml
      yamlSent = true
    }
    return v
  })
}
