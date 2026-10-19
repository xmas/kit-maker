<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Head, Link, router, usePage } from '@inertiajs/vue3'
import { ArrowLeft, Pencil, Save } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'
import CredentialEditor from '@/components/kit/CredentialEditor.vue'
import NetworkRulesEditor from '@/components/kit/NetworkRulesEditor.vue'
import RawEditor from '@/components/kit/RawEditor.vue'
import { sectionStyle, slotHref, type SlotDetail } from '@/lib/kit'

interface BlockDetail {
  id: string
  name: string
  slug: string
  type: string
  typeLabel: string
  section: string
  description: string
  team: string
  visibility: string
  version: number
  usedBy: number
  payload: Record<string, any>
  versions: { version: number; changelog: string; authoredBy: string; by?: string; createdAt: string }[]
  usedIn: { kitId: string; kit: string; slotKey: string; slotLabel: string; version: number; edits: number }[] | null
}

const props = defineProps<{ block: BlockDetail; can: { edit: boolean; publish: boolean } }>()

const page = usePage()
const errors = computed(() => (page.props.errors as Record<string, string> | undefined) ?? {})

// The editors are shared with the kit builder. In the library there's no
// base to compare against, so every row reads as the block's own.
const asSlot = computed(
  () =>
    ({
      key: props.block.slug,
      label: props.block.name,
      section: props.block.section,
      required: false,
      origin: 'custom',
      edits: 0,
      usedBy: props.block.usedBy,
      base: props.block.payload,
    }) as SlotDetail,
)

const draft = ref<Record<string, any>>({})
const saved = ref('')
const changelog = ref('')
watch(
  () => props.block,
  (b) => {
    draft.value = JSON.parse(JSON.stringify(b.payload))
    saved.value = JSON.stringify(draft.value)
    changelog.value = ''
  },
  { immediate: true },
)
const dirty = computed(() => JSON.stringify(draft.value) !== saved.value)
const saving = ref(false)
function publish() {
  saving.value = true
  router.post(
    `/blocks/${props.block.id}/versions`,
    { payload: draft.value, changelog: changelog.value },
    { preserveScroll: true, onFinish: () => (saving.value = false) },
  )
}

const editingDetails = ref(false)
const details = ref({ name: '', description: '' })
function editDetails() {
  details.value = { name: props.block.name, description: props.block.description }
  editingDetails.value = true
}
function saveDetails() {
  router.put(`/blocks/${props.block.id}`, details.value, { preserveScroll: true, onSuccess: () => (editingDetails.value = false) })
}

const style = computed(() => sectionStyle[props.block.section] ?? { fg: '#475569', bg: '#F1F3F6', label: '' })
const behind = computed(() => (props.block.usedIn ?? []).filter((u) => u.version < props.block.version).length)
function when(iso: string) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}
</script>

<template>
  <Head :title="`${block.name} · Blocks`" />
  <AppShell>
    <main class="mx-auto flex w-full max-w-[1300px] flex-col gap-6 px-4 py-6">
      <Link href="/blocks" class="inline-flex items-center gap-1 self-start text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft class="size-3.5" /> Blocks
      </Link>

      <div class="flex flex-col gap-6 xl:flex-row xl:items-start">
        <section class="min-w-0 flex-1 overflow-hidden rounded-[14px] border bg-[#FAFBFF]">
          <header class="flex flex-wrap items-center gap-3.5 border-b px-5 py-4" :style="{ background: style.bg }">
            <span class="flex size-9 items-center justify-center rounded-[9px] bg-white text-sm font-bold" :style="{ color: style.fg }">
              {{ block.typeLabel.slice(0, 1) }}
            </span>
            <div class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-[11px] font-semibold tracking-wider uppercase" :style="{ color: style.fg }">{{ block.typeLabel }} · {{ block.team }}</span>
              <h1 class="text-xl font-semibold">{{ block.name }}</h1>
            </div>
            <span class="rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-slate-700">v{{ block.version }}</span>
            <button v-if="can.edit && !editingDetails" type="button" class="inline-flex items-center gap-1 rounded-md bg-white px-2.5 py-1 text-[13px] hover:bg-white/80" @click="editDetails">
              <Pencil class="size-3" /> Details
            </button>
          </header>

          <div class="flex flex-col gap-4 p-5">
            <form v-if="editingDetails" class="flex flex-col gap-2 rounded-lg border bg-background p-3.5" @submit.prevent="saveDetails">
              <input v-model="details.name" aria-label="Name" class="h-9 rounded-md border px-2.5 text-[14px]" required />
              <input v-model="details.description" aria-label="Description" placeholder="What it's for" class="h-9 rounded-md border px-2.5 text-[14px]" />
              <p v-if="errors.details" role="alert" class="text-xs text-red-700">{{ errors.details }}</p>
              <div class="flex gap-2">
                <button type="submit" class="rounded-md bg-primary px-3 py-1 text-[13px] font-medium text-primary-foreground">Save details</button>
                <button type="button" class="rounded-md px-3 py-1 text-[13px] text-slate-600 hover:bg-muted" @click="editingDetails = false">Cancel</button>
              </div>
            </form>
            <p v-else class="text-sm text-slate-600">
              {{ block.description || 'No description yet.' }} <span class="font-mono text-xs text-muted-foreground">· {{ block.slug }}</span>
            </p>

            <NetworkRulesEditor v-if="block.type === 'network-rules'" v-model="draft" :slot="asSlot" :can-edit="can.publish" />
            <CredentialEditor v-else-if="block.type === 'credential'" :key="block.id + block.version" v-model="draft" :slot="asSlot" :can-edit="can.publish" />
            <RawEditor v-else :key="block.id + block.version" v-model="draft" :can-edit="can.publish" />

            <div v-if="errors.payload" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{{ errors.payload }}</div>
            <div v-if="can.publish" class="flex flex-wrap items-center gap-2">
              <input
                v-model="changelog"
                :disabled="!dirty"
                aria-label="What changed"
                placeholder="What changed? (shown to kits that use this block)"
                class="h-9 min-w-[260px] flex-1 rounded-md border bg-background px-2.5 text-[13px] disabled:bg-muted/40"
              />
              <button
                type="button"
                :disabled="!dirty || saving"
                class="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                @click="publish"
              >
                <Save class="size-3.5" /> {{ saving ? 'Saving…' : `Save as v${block.version + 1}` }}
              </button>
            </div>
            <p v-else class="text-xs text-muted-foreground">You can view this block. Saving new versions needs the block.publish_version permission.</p>
          </div>
        </section>

        <aside class="flex w-full shrink-0 flex-col gap-4 xl:w-[360px]">
          <section class="rounded-xl border bg-background px-4 py-3.5">
            <div class="mb-2 flex items-baseline justify-between">
              <h2 class="text-sm font-semibold">Used by</h2>
              <span v-if="behind" class="text-xs text-green-700">{{ behind }} can update to v{{ block.version }}</span>
            </div>
            <Link
              v-for="u in block.usedIn ?? []"
              :key="u.kitId + u.slotKey"
              :href="slotHref(u.kitId, u.slotKey)"
              class="flex items-center gap-2 border-t border-slate-100 py-2 text-[13px] hover:text-primary"
            >
              <span class="min-w-0 flex-1 truncate">{{ u.kit }} <span class="text-muted-foreground">· {{ u.slotLabel }}</span></span>
              <span v-if="u.edits" class="text-xs text-[#1A4FBF]">{{ u.edits }} edits</span>
              <span class="rounded-full px-1.5 py-px text-[11px] font-semibold" :class="u.version < block.version ? 'bg-green-50 text-green-700' : 'bg-slate-100 text-slate-600'">
                v{{ u.version }}
              </span>
            </Link>
            <p v-if="!block.usedIn?.length" class="text-sm text-muted-foreground">No kits use this block yet.</p>
          </section>

          <section class="rounded-xl border bg-background px-4 py-3.5">
            <h2 class="mb-2 text-sm font-semibold">Versions</h2>
            <div v-for="v in block.versions" :key="v.version" class="flex gap-3 border-t border-slate-100 py-2 text-[13px]">
              <span class="w-8 shrink-0 font-semibold">v{{ v.version }}</span>
              <div class="flex min-w-0 flex-col">
                <span>{{ v.changelog || '—' }}</span>
                <span class="text-xs text-muted-foreground">{{ v.by ?? (v.authoredBy === 'ai' ? 'AI' : 'Someone') }} · {{ when(v.createdAt) }}</span>
              </div>
            </div>
          </section>
        </aside>
      </div>
    </main>
  </AppShell>
</template>
