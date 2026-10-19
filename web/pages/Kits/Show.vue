<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Head, router, usePage } from '@inertiajs/vue3'
import { ArrowUp, BookPlus, FileCode, History, LayoutDashboard, ListTree, Pencil, RefreshCw, Save, Trash2, Undo2, Unlink } from '@lucide/vue'
import ActionMenu, { type MenuItem } from '@/components/ActionMenu.vue'
import AppShell from '@/components/AppShell.vue'
import BlankSlot from '@/components/kit/BlankSlot.vue'
import CredentialEditor from '@/components/kit/CredentialEditor.vue'
import ExplainPopover from '@/components/kit/ExplainPopover.vue'
import HistoryList, { type Revision } from '@/components/kit/HistoryList.vue'
import NetworkRulesEditor from '@/components/kit/NetworkRulesEditor.vue'
import Outline from '@/components/kit/Outline.vue'
import RawEditor from '@/components/kit/RawEditor.vue'
import SlotFrame from '@/components/kit/SlotFrame.vue'
import YamlPanel from '@/components/kit/YamlPanel.vue'
import { confirm } from '@/lib/confirm'
import { sectionStyle, slotHref, type Candidate, type Compiled, type KitInfo, type Section, type SlotDetail } from '@/lib/kit'

const props = defineProps<{
  kit: KitInfo
  sections: Section[]
  compiled: Compiled
  selected: SlotDetail | null
  can: { edit: boolean; revert: boolean; use: boolean; ai: boolean; create: boolean; update: boolean; history: boolean }
  candidates?: Candidate[]
  view?: 'kit' | 'history'
  history?: Revision[] | null
}>()

const page = usePage()
const errors = computed(() => (page.props.errors as Record<string, string> | undefined) ?? {})

// --- Sidebars: the outline (left) and the YAML (right) can each be hidden.
// A hidden outline leaves a rail that opens it as a flyout on hover.
function stored(key: string, fallback: boolean): boolean {
  try {
    const v = localStorage.getItem(key)
    return v === null ? fallback : v === '1'
  } catch {
    return fallback
  }
}
function store(key: string, v: boolean) {
  try {
    localStorage.setItem(key, v ? '1' : '0')
  } catch {
    /* storage may be unavailable */
  }
}
const outlineHidden = ref(stored('kitmaker.builder.outlineHidden', false))
const yamlHidden = ref(stored('kitmaker.builder.yamlHidden', typeof window !== 'undefined' && window.innerWidth < 1280))
watch(outlineHidden, (v) => store('kitmaker.builder.outlineHidden', v))
watch(yamlHidden, (v) => store('kitmaker.builder.yamlHidden', v))

// YAML width: about 40% of the window by default, resizable by dragging the
// sidebar's left edge (or with the arrow keys), remembered per browser.
// The editor keeps at least 420px.
function defaultYamlWidth() {
  return typeof window === 'undefined' ? 560 : Math.round(Math.min(900, Math.max(440, window.innerWidth * 0.4)))
}
function clampYaml(w: number) {
  // page padding, outline (or rail) + gap, editor minimum, gap before the YAML
  const gap = outlineHidden.value ? 16 : 31
  const outline = (outlineHidden.value ? 36 : 272) + gap
  const max = Math.max(360, (typeof window === 'undefined' ? 1440 : window.innerWidth) - 32 - outline - 420 - gap)
  return Math.round(Math.min(max, Math.max(320, w)))
}
const yamlWidth = ref(defaultYamlWidth())
try {
  const w = Number(localStorage.getItem('kitmaker.builder.yamlWidth'))
  if (w > 0) yamlWidth.value = w
} catch {
  /* storage may be unavailable */
}
yamlWidth.value = clampYaml(yamlWidth.value)
function saveYamlWidth() {
  try {
    localStorage.setItem('kitmaker.builder.yamlWidth', String(yamlWidth.value))
  } catch {
    /* storage may be unavailable */
  }
}
const resizing = ref(false)
function startResize(e: PointerEvent) {
  const aside = (e.currentTarget as HTMLElement).parentElement
  if (!aside) return
  const right = aside.getBoundingClientRect().right
  resizing.value = true
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  const move = (ev: PointerEvent) => (yamlWidth.value = clampYaml(right - ev.clientX))
  const up = () => {
    resizing.value = false
    saveYamlWidth()
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}
function resizeBy(dx: number) {
  yamlWidth.value = clampYaml(yamlWidth.value + dx)
  saveYamlWidth()
}
const reclamp = () => (yamlWidth.value = clampYaml(yamlWidth.value))
watch(outlineHidden, reclamp)
onMounted(() => window.addEventListener('resize', reclamp))
onBeforeUnmount(() => window.removeEventListener('resize', reclamp))
function resetYamlWidth() {
  yamlWidth.value = clampYaml(defaultYamlWidth())
  saveYamlWidth()
}
const peek = ref(false)
// The flyout stays open briefly after the pointer leaves, so a slip past
// its edge doesn't close it.
let peekTimer: ReturnType<typeof setTimeout> | undefined
function openPeek() {
  clearTimeout(peekTimer)
  peek.value = true
}
function closePeekSoon() {
  clearTimeout(peekTimer)
  peekTimer = setTimeout(() => (peek.value = false), 400)
}
watch(
  () => props.selected?.key,
  () => (peek.value = false),
)

// --- The selected slot. The editors work on a copy of the effective payload;
// saving sends it whole and the server stores the difference as overrides.
const sel = computed(() => props.selected)
const href = computed(() => slotHref(props.kit.id, sel.value?.key))
const draft = ref<Record<string, any>>({})
const saved = ref('')
const showSlotHistory = ref(false) // declared before the selection watcher, which resets it
let lastKey: string | undefined
const savingAs = ref(false)
const saveAsName = ref('')
watch(
  () => props.selected,
  (s) => {
    draft.value = JSON.parse(JSON.stringify(s?.effective ?? {}))
    saved.value = JSON.stringify(draft.value)
    savingAs.value = false
    if (s?.key !== lastKey) showSlotHistory.value = false
    lastKey = s?.key
  },
  { immediate: true },
)
const dirty = computed(() => JSON.stringify(draft.value) !== saved.value)
const saving = ref(false)

// Leaving with unsaved edits asks first: in-app visits through the dialog,
// closing or reloading the tab through the browser's own prompt.
let leaving = false
const offBefore = router.on('before', (event) => {
  const visit = event.detail.visit
  if (!dirty.value || leaving || visit.method !== 'get') return
  confirm({
    title: 'Leave without saving?',
    message: `Your unsaved changes to ${title.value} will be lost.`,
    confirmLabel: 'Leave',
  }).then((ok) => {
    if (!ok) return
    leaving = true
    router.visit(visit.url, { onFinish: () => (leaving = false) })
  })
  return false
})
function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => {
  offBefore()
  window.removeEventListener('beforeunload', beforeUnload)
})

// Live preview: while the draft differs from what's saved, compile it on the
// server (same compiler and spec validation as a save) and diff the YAML.
const preview = ref<Compiled | null>(null)
let previewTimer: ReturnType<typeof setTimeout> | undefined
let previewAbort: AbortController | null = null
watch(
  draft,
  () => {
    clearTimeout(previewTimer)
    if (!dirty.value || !sel.value?.blockSlug) {
      previewAbort?.abort()
      preview.value = null
      return
    }
    previewTimer = setTimeout(fetchPreview, 200)
  },
  { deep: true },
)
async function fetchPreview() {
  previewAbort?.abort()
  previewAbort = new AbortController()
  try {
    const res = await fetch(`${href.value}/preview`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ payload: draft.value }),
      signal: previewAbort.signal,
    })
    if (res.ok && dirty.value) preview.value = (await res.json()).compiled
  } catch {
    /* aborted by a newer edit, or offline: keep the last preview */
  }
}

function save() {
  saving.value = true
  router.put(href.value, { payload: draft.value }, { preserveScroll: true, onFinish: () => (saving.value = false) })
}
async function discard() {
  const ok = await confirm({
    title: 'Revert your unsaved changes?',
    message: 'The edits you made since the last save are thrown away. Saved edits and history are not affected.',
    confirmLabel: 'Revert changes',
  })
  if (ok) draft.value = JSON.parse(saved.value)
}
function resync() {
  const s = sel.value
  if (!s) return
  const n = s.edits
  const msg = n
    ? `Drop the ${n} ${n === 1 ? 'edit' : 'edits'} made in this kit and use ${s.blockName} v${s.latest || s.version} exactly as it is in the library?`
    : `Move to ${s.blockName} v${s.latest}?`
  confirm({ title: 'Resync to the library block?', message: msg, confirmLabel: 'Resync' }).then(
    (ok) => ok && router.post(`${href.value}/resync`, {}, { preserveScroll: true }),
  )
}

// --- History: the kit's (History view) or the selected slot's (panel).
// Previewing a point shows its YAML against the kit as it is now.
const slotHistory = ref<Revision[]>([])
const revPreview = ref<{ rev: Revision; compiled: Compiled } | null>(null)
async function loadSlotHistory() {
  if (!sel.value) return
  const res = await fetch(`/kits/${props.kit.id}/history.json?slot=${encodeURIComponent(sel.value.key)}`, { headers: { Accept: 'application/json' } })
  if (res.ok) slotHistory.value = (await res.json()).revisions
}
function toggleSlotHistory() {
  showSlotHistory.value = !showSlotHistory.value
  revPreview.value = null
  if (showSlotHistory.value) loadSlotHistory()
}
async function previewRevision(rev: Revision | null) {
  if (!rev) {
    revPreview.value = null
    return
  }
  const res = await fetch(`/kits/${props.kit.id}/revisions/${rev.id}/yaml`, { headers: { Accept: 'application/json' } })
  if (res.ok) revPreview.value = { rev, compiled: { yaml: (await res.json()).yaml, valid: true } }
}
watch(
  () => [props.selected?.key, props.view, props.compiled.digest, props.compiled.yaml],
  () => {
    revPreview.value = null
    if (showSlotHistory.value) loadSlotHistory()
  },
)

function update() {
  router.post(`${href.value}/update`, {}, { preserveScroll: true })
}
async function unlink() {
  const n = sel.value?.edits ?? 0
  const ok = await confirm({
    title: `Unlink ${title.value}?`,
    message: `The slot stays in the kit but becomes empty${n ? `, and the ${n} ${n === 1 ? 'edit' : 'edits'} made here are dropped` : ''}. You can restore it from History.`,
    confirmLabel: 'Unlink',
  })
  if (ok) router.post(`${href.value}/unlink`, {}, { preserveScroll: true })
}
async function removeSlot() {
  const ok = await confirm({
    title: `Remove ${title.value} from this kit?`,
    message: 'The slot and its edits are removed from the kit. The library block itself is not changed, and you can restore the slot from History.',
    confirmLabel: 'Remove',
  })
  if (ok) router.delete(href.value)
}
function startSaveAs() {
  const s = sel.value
  if (!s) return
  saveAsName.value = s.origin === 'custom' ? s.blockName || s.label : `${s.blockName} (${props.kit.displayName})`
  savingAs.value = true
}
function saveAs() {
  router.post(`${href.value}/save-as-block`, { name: saveAsName.value }, { preserveScroll: true })
}

// A filled slot is shown as the block it holds.
const title = computed(() => (sel.value ? (sel.value.blockSlug ? sel.value.blockName || sel.value.label : sel.value.label) : ''))
const sectionLabel = computed(() => (sel.value ? (sectionStyle[sel.value.section]?.label ?? sel.value.section) : ''))
const highlight = computed(() =>
  sel.value && !(revPreview.value && !preview.value) ? ((preview.value ?? props.compiled).spans?.[sel.value.key] ?? []) : [],
)
// Unsaved edits win over a history preview.
const yamlPreview = computed(() => preview.value ?? revPreview.value?.compiled ?? null)
const yamlPreviewLabel = computed(() => (!preview.value && revPreview.value ? `Previewing #${revPreview.value.rev.seq} vs now` : undefined))
const inKit = computed(() => props.sections.flatMap((s) => s.items).flatMap((i) => (i.blockSlug ? [i.blockSlug] : [])))
const counts = computed(() => {
  const items = props.sections.flatMap((s) => s.items)
  return { slots: items.length, empty: items.filter((i) => !i.blockSlug).length, edited: items.filter((i) => i.edits > 0).length }
})

// Goal statement, edited in place on the overview.
const editingGoal = ref(false)
const goalDraft = ref('')
function editGoal() {
  goalDraft.value = props.kit.goal
  editingGoal.value = true
}
function saveGoal() {
  router.put(`/kits/${props.kit.id}/goal`, { goal: goalDraft.value }, { preserveScroll: true, onSuccess: () => (editingGoal.value = false) })
}

// Less common actions live in the "⋯" menu; destructive ones confirm first.
const menuItems = computed<MenuItem[]>(() => {
  const s = sel.value
  if (!s) return []
  const items: MenuItem[] = []
  const behind = !!(s.latest && s.version && s.latest > s.version)
  if (s.blockSlug && props.can.revert && props.can.update && s.origin !== 'custom' && (s.edits || behind)) {
    items.push({ label: 'Resync to block', icon: RefreshCw, hint: "Drop this kit's edits; use the library block as it is", onSelect: resync })
  }
  if (s.blockSlug && props.can.edit) items.push({ label: 'Unlink', icon: Unlink, hint: 'Keep the slot, empty it', danger: true, onSelect: unlink })
  if (props.can.edit) items.push({ label: 'Remove from kit', icon: Trash2, hint: 'Remove the slot', danger: true, onSelect: removeSlot })
  return items
})

const btn = 'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium disabled:opacity-50'
const onBlue = btn + ' h-8 py-0 bg-white/15 hover:bg-white/25'
const rail =
  'hidden w-9 shrink-0 flex-col items-center gap-2 self-start rounded-xl border bg-background py-2.5 text-slate-500 hover:text-slate-800 lg:sticky lg:top-4 lg:flex'
</script>

<template>
  <Head :title="sel ? `${title} · ${kit.slug}` : kit.slug" />
  <AppShell>
    <main class="flex w-full flex-col gap-6 px-4 py-6 lg:flex-row lg:items-start" :class="outlineHidden ? 'lg:gap-4' : 'lg:gap-[31px]'">
      <!-- Left: the outline, or a rail that opens it on hover -->
      <!-- z-40: a sticky element is its own stacking context, so the flyout's
           layer has to be set here or the editor frame paints over it. -->
      <div
        v-if="outlineHidden"
        class="relative hidden shrink-0 self-start lg:sticky lg:top-4 lg:z-40 lg:block"
        @mouseenter="openPeek"
        @mouseleave="closePeekSoon"
      >
        <button type="button" :class="rail" aria-label="Show outline" title="Click to keep the outline open" @click="outlineHidden = false">
          <ListTree class="size-4" />
          <span class="text-[11px] font-medium [writing-mode:vertical-rl]">Outline</span>
        </button>
        <!-- The flyout opens beside the rail (its padding bridges the gap, so
             the pointer can cross without closing it) and keeps the rail clickable. -->
        <div v-if="peek" class="absolute top-0 left-full max-h-[calc(100vh-6rem)] overflow-y-auto pr-3 pb-3 pl-2">
          <Outline
            :kit="kit"
            :sections="sections"
            :selected="sel?.key ?? null"
            :can-edit="can.edit"
            :dirty-key="dirty ? sel?.key : null"
            :show-history="can.history"
            :view="view"
            floating
            class="shadow-[0_12px_40px_rgba(15,23,42,0.18)]"
            @pin="outlineHidden = false"
          />
        </div>
      </div>
      <Outline
        :kit="kit"
        :sections="sections"
        :selected="sel?.key ?? null"
        :can-edit="can.edit"
        :dirty-key="dirty ? sel?.key : null"
        :show-history="can.history"
        :view="view"
        class="shrink-0 lg:sticky lg:top-4"
        :class="outlineHidden ? 'lg:hidden' : ''"
        @hide="outlineHidden = true"
      />

      <!-- Middle: the overview or the selected slot -->
      <div class="flex min-w-0 flex-1 flex-col">
        <!-- History: every change to this kit; preview any point, restore the kit to it -->
        <SlotFrame v-if="view === 'history'" section="agent-base" eyebrow="History" :title="kit.displayName">
          <template #icon><History class="size-4.5 text-primary" /></template>
          <p class="text-sm text-slate-600">
            Every change to this kit, newest first. Preview a point to see its YAML against the kit as it is now; restore puts the
            whole kit back (goal, blocks and their edits). To go back for one block only, open its History.
          </p>
          <p v-if="errors.restore" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{{ errors.restore }}</p>
          <div class="rounded-lg border bg-background px-4 py-1">
            <HistoryList :kit-id="kit.id" :revisions="history ?? []" :previewing="revPreview?.rev.id" :can-restore="can.edit" @preview="previewRevision" />
          </div>
        </SlotFrame>

        <SlotFrame v-else-if="!sel" section="agent-base" eyebrow="Overview" :title="kit.displayName">
          <template #icon><LayoutDashboard class="size-4.5 text-primary" /></template>
          <div class="flex flex-col gap-1.5 rounded-lg border bg-background p-4">
            <div class="flex items-center justify-between gap-2">
              <h2 class="text-sm font-semibold">What this kit is for</h2>
              <button v-if="can.edit && !editingGoal" type="button" class="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[12.5px] text-primary hover:bg-primary/5" @click="editGoal">
                <Pencil class="size-3" /> Edit
              </button>
            </div>
            <form v-if="editingGoal" class="flex flex-col gap-2" @submit.prevent="saveGoal">
              <textarea
                v-model="goalDraft"
                rows="3"
                aria-label="Goal"
                placeholder="What should the agent be able to do, and what must it never do?"
                class="rounded-md border p-2.5 text-[14px] leading-relaxed focus:border-primary focus:outline-none"
              />
              <div class="flex gap-2">
                <button type="submit" class="rounded-md bg-primary px-3 py-1 text-[13px] font-medium text-primary-foreground">Save</button>
                <button type="button" class="rounded-md px-3 py-1 text-[13px] text-slate-600 hover:bg-muted" @click="editingGoal = false">Cancel</button>
              </div>
            </form>
            <template v-else>
              <p class="text-[15px] leading-relaxed">{{ kit.goal || kit.description || 'Nothing written yet.' }}</p>
              <p v-if="kit.goal && kit.description" class="text-sm text-muted-foreground">{{ kit.description }}</p>
            </template>
          </div>
          <p class="text-sm text-slate-600">
            {{ counts.slots }} {{ counts.slots === 1 ? 'block' : 'blocks' }}<template v-if="counts.empty"> · {{ counts.empty }} empty</template
            ><template v-if="counts.edited"> · {{ counts.edited }} edited in this kit</template>. Pick one in the outline to edit it, or add
            blocks under any section.
          </p>
        </SlotFrame>

        <SlotFrame
          v-else-if="sel"
          :section="sel.section"
          :eyebrow="sel.blockSlug ? sectionLabel : `${sectionLabel} · empty slot`"
          :title="title"
          :origin="sel.origin"
          :edits="sel.edits"
          :version="sel.version"
          :latest="sel.latest"
          :used-by="sel.usedBy"
          :block-slug="sel.blockSlug"
        >
          <template v-if="sel.blockSlug" #status="{ close }">
            <div class="flex flex-col gap-3 text-[13px]">
              <p v-if="sel.origin === 'custom'">
                <b>{{ sel.blockName }}</b> lives only in this kit. Save it to the library to reuse it in other kits.
              </p>
              <p v-else-if="sel.edits">
                Linked to <b>{{ sel.blockName }} v{{ sel.version }}</b> with {{ sel.edits }} {{ sel.edits === 1 ? 'edit' : 'edits' }} made in this kit
                (marked in the table). New library versions still flow in underneath the edits.
              </p>
              <p v-else>
                Linked to <b>{{ sel.blockName }} v{{ sel.version }}</b> as it is in the library. New library versions flow in.
              </p>
              <p v-if="sel.usedBy > 1" class="text-xs text-muted-foreground">Used by {{ sel.usedBy }} kits.</p>
              <div v-if="can.create && (sel.edits || sel.origin === 'custom')" class="flex flex-col gap-2 border-t pt-3">
                <form v-if="savingAs" class="flex flex-col gap-2" @submit.prevent="saveAs">
                  <label class="flex flex-col gap-1 text-xs font-medium text-slate-600">
                    New block name
                    <input v-model="saveAsName" required class="h-8 rounded-md border bg-background px-2.5 text-[13px] font-normal text-foreground" />
                  </label>
                  <p class="text-xs text-muted-foreground">
                    The slot then links the new block with no edits left over<template v-if="sel.origin !== 'custom'">; {{ sel.blockName }} stays as it is</template>.
                  </p>
                  <p v-if="errors.saveAs" role="alert" class="text-xs text-red-700">{{ errors.saveAs }}</p>
                  <div class="flex gap-2">
                    <button type="submit" class="rounded-md bg-primary px-3 py-1 text-[13px] font-medium text-primary-foreground">Save block</button>
                    <button type="button" class="rounded-md px-3 py-1 text-[13px] text-slate-600 hover:bg-muted" @click="savingAs = false">Cancel</button>
                  </div>
                </form>
                <button
                  v-else
                  type="button"
                  class="inline-flex items-center gap-1.5 self-start rounded-md border px-2.5 py-1 font-medium hover:bg-muted disabled:opacity-50"
                  :disabled="dirty"
                  :title="dirty ? 'Save or revert your changes first' : 'Make a new library block from this one plus the edits'"
                  @click="startSaveAs"
                >
                  <BookPlus class="size-3.5" /> {{ sel.origin === 'custom' ? 'Save to library' : 'Save as new block' }}
                </button>
              </div>
              <button
                v-if="menuItems.some((i) => i.label === 'Resync to block')"
                type="button"
                class="inline-flex items-center gap-1.5 self-start rounded-md px-2.5 py-1 text-slate-700 hover:bg-muted"
                @click="close(); resync()"
              >
                <RefreshCw class="size-3.5" /> Resync to block
              </button>
            </div>
          </template>
          <template #actions>
            <ExplainPopover v-if="can.ai && sel.blockSlug" :href="`${href}/explain`" />
            <button v-if="can.update && sel.latest && sel.version && sel.latest > sel.version" type="button" :class="btn + ' h-8 py-0 bg-white text-primary hover:bg-white/90'" @click="update">
              <ArrowUp class="size-3.5" /> Update to v{{ sel.latest }}
            </button>
            <button v-if="can.history" type="button" :class="onBlue + (showSlotHistory ? ' bg-white/30' : '')" :aria-pressed="showSlotHistory" @click="toggleSlotHistory">
              <History class="size-3.5" /> History
            </button>
            <ActionMenu v-if="menuItems.length" :items="menuItems" :label="`More actions for ${title}`" />
          </template>

          <!-- This slot's history -->
          <section v-if="showSlotHistory" aria-label="Slot history" class="rounded-lg border bg-background px-4 py-2.5">
            <div class="mb-1 flex items-baseline justify-between gap-2">
              <h3 class="text-sm font-semibold">History of this slot</h3>
              <span class="text-xs text-muted-foreground">Restore changes only this slot.</span>
            </div>
            <HistoryList
              :kit-id="kit.id"
              :revisions="slotHistory"
              :slot-key="sel.key"
              :previewing="revPreview?.rev.id"
              :can-restore="can.edit"
              @preview="previewRevision"
            />
          </section>

          <!-- Empty slot: pick a block -->
          <BlankSlot v-if="!sel.blockSlug" :kit-id="kit.id" :slot="sel" :candidates="candidates ?? []" :in-kit="inKit" :can-use="can.use" :can-create="can.create" />


          <template v-else>
            <p v-if="sel.blockDescription" class="text-sm text-slate-600">{{ sel.blockDescription }}</p>

            <div v-if="sel.problem" role="alert" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">{{ sel.problem }}</div>
            <div v-if="sel.conflicts?.length" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
              {{ sel.conflicts.length }} {{ sel.conflicts.length === 1 ? 'edit no longer applies' : 'edits no longer apply' }}: the library version
              removed what {{ sel.conflicts.length === 1 ? 'it' : 'they' }} changed. Save to drop {{ sel.conflicts.length === 1 ? 'it' : 'them' }}, or revert.
            </div>

            <NetworkRulesEditor v-if="sel.blockType === 'network-rules'" v-model="draft" :slot="sel" :can-edit="can.edit" />
            <CredentialEditor v-else-if="sel.blockType === 'credential'" :key="sel.key" v-model="draft" :slot="sel" :can-edit="can.edit" />
            <RawEditor v-else :key="sel.key" v-model="draft" :can-edit="can.edit" />

            <div v-if="errors.payload" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{{ errors.payload }}</div>
            <!-- Save bar: pinned to the bottom of the screen while the editor scrolls. -->
            <div
              v-if="can.edit"
              class="sticky bottom-0 z-20 -mx-4 -mb-4 flex items-center gap-2 rounded-b-[11px] border-t bg-[#FAFBFF]/95 px-4 py-3 backdrop-blur-sm"
              :class="dirty ? 'shadow-[0_-8px_20px_rgba(15,23,42,0.06)]' : ''"
            >
              <button type="button" :class="btn + ' bg-primary text-primary-foreground hover:bg-primary/90'" :disabled="!dirty || saving" @click="save">
                <Save class="size-3.5" /> {{ saving ? 'Saving…' : 'Save to this kit' }}
              </button>
              <button
                type="button"
                :class="btn + ' text-slate-600 hover:bg-muted'"
                :disabled="!dirty"
                title="Throw away the unsaved changes"
                @click="discard"
              >
                <Undo2 class="size-3.5" /> Revert changes
              </button>
              <span class="ml-auto inline-flex items-center gap-1.5 text-xs" :class="dirty ? 'font-medium text-amber-700' : 'text-muted-foreground'">
                <span v-if="dirty" class="size-1.5 rounded-full bg-amber-500" aria-hidden="true" />
                {{ dirty ? 'Unsaved changes' : sel.origin === 'custom' ? 'Saved in this kit’s own block' : `Saved as edits on library v${sel.version}` }}
              </span>
            </div>
          </template>
        </SlotFrame>
      </div>

      <!-- Right: the YAML, always there unless hidden -->
      <aside
        v-if="!yamlHidden"
        class="relative flex w-full shrink-0 flex-col lg:sticky lg:top-4 lg:h-[calc(100vh-5.5rem)] lg:w-(--yaml-w)"
        :style="{ '--yaml-w': `${yamlWidth}px` }"
        :class="resizing ? 'select-none' : ''"
      >
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize YAML panel"
          :aria-valuenow="yamlWidth"
          tabindex="0"
          title="Drag to resize · double-click to reset"
          class="group absolute inset-y-0 -left-3 z-10 hidden w-3 cursor-col-resize items-center justify-center focus:outline-none lg:flex"
          @pointerdown.prevent="startResize"
          @dblclick="resetYamlWidth"
          @keydown.left.prevent="resizeBy(24)"
          @keydown.right.prevent="resizeBy(-24)"
        >
          <span
            class="h-12 w-1 rounded-full transition-colors"
            :class="resizing ? 'bg-primary' : 'bg-slate-300 group-hover:bg-primary group-focus-visible:bg-primary'"
          />
        </div>
        <YamlPanel :compiled="compiled" :preview="yamlPreview" :preview-label="yamlPreviewLabel" :slug="kit.slug" :highlight="highlight" class="max-h-[70vh] flex-1 lg:max-h-none" @hide="yamlHidden = true" />
      </aside>
      <button v-else type="button" :class="rail" aria-label="Show YAML" title="Show YAML" @click="yamlHidden = false">
        <FileCode class="size-4" />
        <span class="text-[11px] font-medium [writing-mode:vertical-rl]">YAML</span>
      </button>
    </main>
  </AppShell>
</template>
