<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Check, Copy, Download, ExternalLink, LoaderCircle, Play, Rocket, TriangleAlert, X } from '@lucide/vue'

// Run a blueprint with sbx. The main path is a published version: Publish
// builds the kit image and pushes it to the deployment's registry (vN and
// latest); sbx runs it by reference, and an sbxenv.yaml following :latest
// with pullPolicy: always picks up every new version. The fallback is a
// local copy: a folder sbx builds itself. The Cloudflare edition publishes
// on GitHub Actions, so a version can be pending (the dialog polls until
// the workflow reports) or failed.
const props = defineProps<{ blueprintId: string; name: string }>()
const open = defineModel<boolean>('open', { required: true })

interface Published {
  version: number
  repository: string
  digest: string
  publishedAt: string
  unchanged?: boolean
  image: string
  pinned: string
  command: string
}
interface Attempt {
  version: number
  error?: string
  runUrl?: string
  startedAt?: string
}
interface RunInfo {
  slug: string
  kind: string
  valid: boolean
  error?: string
  published?: Published
  pending?: Attempt
  failed?: Attempt
  unchanged?: boolean
  canPublish: boolean
  publishSetup?: string
  allowedSource?: string
  workload?: { name: string; slug: string; image: string }
  extract: string
  localCommand: string
}
const info = ref<RunInfo | null>(null)
const loadError = ref('')
const copied = ref('')
const closeBtn = ref<HTMLButtonElement | null>(null)
const publishing = ref(false)
const publishMsg = ref('')
const publishError = ref('')

watch(
  open,
  async (o) => {
    if (o) await load()
    else stopPolling()
  },
  { immediate: true },
)

// While a version is publishing, check every few seconds until it's done.
let poll: ReturnType<typeof setInterval> | undefined
function stopPolling() {
  clearInterval(poll)
  poll = undefined
}
function pollWhilePending() {
  if (!info.value?.pending || poll) return
  const waiting = info.value.pending.version
  poll = setInterval(async () => {
    const res = await fetch(`/blueprints/${props.blueprintId}/run`, { headers: { Accept: 'application/json' } }).catch(() => null)
    if (!res?.ok) return
    const next = (await res.json()) as RunInfo
    info.value = next
    if (next.pending) return
    stopPolling()
    if (next.published?.version === waiting) publishMsg.value = `Published v${waiting}.`
    else if (next.failed?.version === waiting) publishMsg.value = ''
  }, 4000)
}
onBeforeUnmount(stopPolling)
async function load() {
  info.value = null
  loadError.value = publishMsg.value = publishError.value = ''
  await nextTick()
  closeBtn.value?.focus()
  try {
    const res = await fetch(`/blueprints/${props.blueprintId}/run`, { headers: { Accept: 'application/json' } })
    if (!res.ok) throw new Error(res.status === 403 ? "You can't download this blueprint." : `couldn't load (${res.status})`)
    info.value = await res.json()
    pollWhilePending()
  } catch (e) {
    loadError.value = (e as Error).message
  }
}

async function publish() {
  publishing.value = true
  publishMsg.value = publishError.value = ''
  const before = info.value?.published?.version
  try {
    const res = await fetch(`/blueprints/${props.blueprintId}/publish`, { method: 'POST', headers: { Accept: 'application/json' } })
    const out = await res.json()
    if (!res.ok) throw new Error(out.error ?? `publishing failed (${res.status})`)
    info.value = out
    const now = out.published?.version
    if (out.pending) {
      publishMsg.value = ''
      pollWhilePending()
    } else publishMsg.value = out.unchanged || now === before ? `No changes since v${now}, so nothing was published.` : `Published v${now}.`
  } catch (e) {
    publishError.value = (e as Error).message
  } finally {
    publishing.value = false
  }
}

function ago(iso: string) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return new Date(iso).toLocaleDateString()
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text)
  copied.value = text
  setTimeout(() => copied.value === text && (copied.value = ''), 1500)
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-[90] flex items-center justify-center bg-slate-900/40 p-4" @mousedown.self="open = false" @keydown.esc="open = false">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="run-title"
        class="flex max-h-[calc(100vh-2rem)] w-full max-w-xl flex-col gap-4 overflow-y-auto rounded-xl bg-background p-5 shadow-[0_20px_60px_rgba(15,23,42,0.3)]"
      >
        <div class="flex items-start gap-3">
          <span class="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary"><Play class="size-4" /></span>
          <div class="flex min-w-0 flex-1 flex-col gap-0.5">
            <h2 id="run-title" class="text-[15px] font-semibold">Run {{ name }}</h2>
            <p class="text-[13px] text-slate-600">Run the published kit with sbx, so every new sandbox gets the latest version.</p>
          </div>
          <button ref="closeBtn" type="button" class="rounded p-1 text-slate-400 hover:bg-muted hover:text-slate-700" aria-label="Close" @click="open = false">
            <X class="size-4" />
          </button>
        </div>

        <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>
        <p v-else-if="!info" class="text-sm text-muted-foreground">Loading…</p>
        <template v-else>
          <div v-if="!info.valid" role="alert" class="flex gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            <TriangleAlert class="mt-0.5 size-4 shrink-0" />
            <span>This blueprint isn't a valid kit yet, so it can't be published or run: {{ info.error || 'fill its empty slots and fix the problems first.' }}</span>
          </div>

          <!-- Published version -->
          <section class="flex flex-col gap-2.5 rounded-lg border p-3.5 text-[13px]">
            <div class="flex items-center gap-2">
              <span class="font-semibold">{{ info.published ? `Published v${info.published.version}` : 'Not published yet' }}</span>
              <span v-if="info.published" class="text-xs text-muted-foreground">{{ ago(info.published.publishedAt) }}</span>
              <span class="flex-1" />
              <button
                v-if="info.canPublish"
                type="button"
                class="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                :disabled="!info.valid || publishing || !!info.publishSetup || !!info.pending"
                :title="info.publishSetup"
                @click="publish"
              >
                <Rocket class="size-3.5" />
                {{ publishing || info.pending ? 'Publishing…' : info.published ? `Publish v${info.published.version + 1}` : 'Publish v1' }}
              </button>
            </div>
            <p v-if="publishing" class="text-xs text-muted-foreground">Building the kit image with BuildKit and pushing it to the registry…</p>
            <p v-if="info.pending" role="status" class="flex items-center gap-1.5 text-xs text-slate-600">
              <LoaderCircle class="size-3.5 animate-spin" /> Building v{{ info.pending.version }} on GitHub Actions and pushing it to the registry; this takes a minute or two.
              <a v-if="info.pending.runUrl" :href="info.pending.runUrl" target="_blank" rel="noopener" class="inline-flex items-center gap-0.5 text-primary hover:underline">Watch <ExternalLink class="size-3" /></a>
            </p>
            <p v-if="info.failed && !publishing" role="alert" class="text-xs text-destructive">
              Publishing v{{ info.failed.version }} failed: {{ info.failed.error }}
              <a v-if="info.failed.runUrl" :href="info.failed.runUrl" target="_blank" rel="noopener" class="inline-flex items-center gap-0.5 text-primary hover:underline">Open the run <ExternalLink class="size-3" /></a>
            </p>
            <p v-if="publishMsg" class="text-xs text-green-700">{{ publishMsg }}</p>
            <p v-if="publishError" role="alert" class="text-xs whitespace-pre-wrap text-destructive">{{ publishError }}</p>
            <p v-if="info.publishSetup" class="text-xs text-muted-foreground">{{ info.publishSetup }}</p>

            <template v-if="info.published">
              <div class="flex items-center gap-2 rounded-lg bg-[#0B0D11] px-3 py-2 font-mono text-[12.5px] text-[#D5D9E0]">
                <code class="min-w-0 flex-1 truncate">{{ info.published.command }}</code>
                <button type="button" class="rounded p-1 text-white/60 hover:bg-white/10 hover:text-white" aria-label="Copy the run command" @click="copy(info.published.command)">
                  <Check v-if="copied === info.published.command" class="size-3.5 text-green-400" /><Copy v-else class="size-3.5" />
                </button>
              </div>
              <p v-if="info.kind === 'mixin'" class="text-xs text-slate-600">
                A mixin runs on a v3 workload kit:
                <template v-if="info.workload">{{ info.workload.name }}, the latest published workload blueprint. Swap in another if you like.</template>
                <template v-else>publish a workload blueprint first, or put your own workload kit in place of <code>&lt;workload-kit&gt;</code>.</template>
              </p>
              <div class="flex flex-wrap items-center gap-2">
                <a :href="`/blueprints/${blueprintId}/sbxenv.yaml`" class="inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 font-medium hover:bg-muted" download>
                  <Download class="size-3.5" /> sbxenv.yaml
                </a>
                <span class="text-xs text-muted-foreground">Follows <code>:latest</code> with <code>pullPolicy: always</code>; run <code>sbx env run</code> next to it.</span>
              </div>
              <button type="button" class="self-start text-xs text-primary hover:underline" @click="copy(info.published.pinned)">
                {{ copied === info.published.pinned ? 'Copied the pinned reference' : `Copy the pinned reference (v${info.published.version})` }}
              </button>
              <p v-if="info.allowedSource" class="rounded-md bg-muted/60 px-2.5 py-2 text-xs leading-relaxed text-slate-600">
                sbx pulls kits only from its allowed sources. If <code>sbx settings get kit.allowedSources</code> doesn't list
                <code>{{ info.allowedSource }}</code>, add it to that list (an administrator may manage it).
              </p>
            </template>
          </section>

          <!-- Local copy -->
          <details class="rounded-lg border p-3.5 text-[13px]">
            <summary class="cursor-pointer font-medium">Or run a local copy</summary>
            <div class="mt-3 flex flex-col gap-2.5">
              <a
                :href="info.valid ? `/blueprints/${blueprintId}/download` : undefined"
                :aria-disabled="!info.valid"
                class="inline-flex h-8 items-center gap-1.5 self-start rounded-lg border px-3 font-medium hover:bg-muted"
                :class="info.valid ? '' : 'pointer-events-none opacity-50'"
                download
              >
                <Download class="size-3.5" /> {{ info.slug }}.tar.gz
              </a>
              <div v-for="cmd in [info.extract, info.localCommand]" :key="cmd" class="flex items-center gap-2 rounded-lg bg-[#0B0D11] px-3 py-2 font-mono text-[12.5px] text-[#D5D9E0]">
                <code class="min-w-0 flex-1 truncate">{{ cmd }}</code>
                <button type="button" class="rounded p-1 text-white/60 hover:bg-white/10 hover:text-white" :aria-label="`Copy ${cmd}`" @click="copy(cmd)">
                  <Check v-if="copied === cmd" class="size-3.5 text-green-400" /><Copy v-else class="size-3.5" />
                </button>
              </div>
              <p class="text-xs leading-relaxed text-slate-600">
                A copy doesn't update. sbx builds the folder in its <code>sbx-kit-builder</code> sandbox on first use; if that fails with “OCI exporter
                is not supported for the docker driver”, turn on Docker Desktop's containerd image store.
              </p>
            </div>
          </details>
        </template>
      </div>
    </div>
  </Teleport>
</template>
