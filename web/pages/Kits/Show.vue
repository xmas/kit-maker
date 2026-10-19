<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Head, router, usePage } from '@inertiajs/vue3'
import { ArrowUp, BookPlus, FileCode, LayoutDashboard, ListTree, Pencil, Replace, RotateCcw, Save, Trash2, Unlink } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'
import BlankSlot from '@/components/kit/BlankSlot.vue'
import CredentialEditor from '@/components/kit/CredentialEditor.vue'
import ExplainPanel from '@/components/kit/ExplainPanel.vue'
import NetworkRulesEditor from '@/components/kit/NetworkRulesEditor.vue'
import Outline from '@/components/kit/Outline.vue'
import RawEditor from '@/components/kit/RawEditor.vue'
import SlotFrame from '@/components/kit/SlotFrame.vue'
import YamlPanel from '@/components/kit/YamlPanel.vue'
import { sectionStyle, slotHref, type Candidate, type Compiled, type KitInfo, type Section, type SlotDetail } from '@/lib/kit'

const props = defineProps<{
  kit: KitInfo
  sections: Section[]
  compiled: Compiled
  selected: SlotDetail | null
  can: { edit: boolean; revert: boolean; use: boolean; ai: boolean; create: boolean; update: boolean }
  candidates?: Candidate[]
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
const peek = ref(false)
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
const relinking = ref(false)
const savingAs = ref(false)
const saveAsName = ref('')
watch(
  () => props.selected,
  (s) => {
    draft.value = JSON.parse(JSON.stringify(s?.effective ?? {}))
    saved.value = JSON.stringify(draft.value)
    relinking.value = false
    savingAs.value = false
  },
  { immediate: true },
)
const dirty = computed(() => JSON.stringify(draft.value) !== saved.value)
const saving = ref(false)

function save() {
  saving.value = true
  router.put(href.value, { payload: draft.value }, { preserveScroll: true, onFinish: () => (saving.value = false) })
}
function discard() {
  draft.value = JSON.parse(saved.value)
}
function revert() {
  if (confirm('Drop every edit made in this kit and go back to the library version?'))
    router.post(`${href.value}/revert`, {}, { preserveScroll: true })
}
function update() {
  router.post(`${href.value}/update`, {}, { preserveScroll: true })
}
function unlink() {
  if (confirm('Unlink this block? The slot becomes empty and any edits made here are dropped.'))
    router.post(`${href.value}/unlink`, {}, { preserveScroll: true })
}
function removeSlot() {
  if (confirm(`Remove "${title.value}" from this kit?`)) router.delete(href.value)
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
const highlight = computed(() => (sel.value ? (props.compiled.spans?.[sel.value.key] ?? []) : []))
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

const btn = 'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium disabled:opacity-50'
const onBlue = btn + ' bg-white/15 hover:bg-white/25'
const rail =
  'hidden w-9 shrink-0 flex-col items-center gap-2 self-start rounded-xl border bg-background py-2.5 text-slate-500 hover:text-slate-800 lg:sticky lg:top-4 lg:flex'
</script>

<template>
  <Head :title="sel ? `${title} · ${kit.slug}` : kit.slug" />
  <AppShell>
    <main class="flex w-full flex-col gap-6 px-4 py-6 lg:flex-row lg:items-start" :class="outlineHidden ? 'lg:gap-4' : 'lg:gap-[31px]'">
      <!-- Left: the outline, or a rail that opens it on hover -->
      <div v-if="outlineHidden" class="relative hidden shrink-0 self-start lg:sticky lg:top-4 lg:block" @mouseleave="peek = false">
        <button type="button" :class="rail" aria-label="Show outline" title="Show outline" @mouseenter="peek = true" @click="outlineHidden = false">
          <ListTree class="size-4" />
          <span class="text-[11px] font-medium [writing-mode:vertical-rl]">Outline</span>
        </button>
        <Outline
          v-if="peek"
          :kit="kit"
          :sections="sections"
          :selected="sel?.key ?? null"
          :can-edit="can.edit"
          floating
          class="absolute top-0 left-0 z-30 shadow-[0_12px_40px_rgba(15,23,42,0.18)]"
        />
      </div>
      <Outline
        :kit="kit"
        :sections="sections"
        :selected="sel?.key ?? null"
        :can-edit="can.edit"
        class="shrink-0 lg:sticky lg:top-4"
        :class="outlineHidden ? 'lg:hidden' : ''"
        @hide="outlineHidden = true"
      />

      <!-- Middle: the overview or the selected slot -->
      <div class="flex min-w-0 flex-1 flex-col">
        <SlotFrame v-if="!sel" section="agent-base" eyebrow="Overview" :title="kit.displayName">
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
          v-else
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
          <template #actions>
            <button v-if="can.update && sel.latest && sel.version && sel.latest > sel.version" type="button" :class="btn + ' bg-white text-primary hover:bg-white/90'" @click="update">
              <ArrowUp class="size-3.5" /> Update to v{{ sel.latest }}
            </button>
            <template v-if="sel.blockSlug">
              <button v-if="can.use && can.edit && !relinking" type="button" :class="onBlue" title="Link a different block to this slot" @click="relinking = true">
                <Replace class="size-3.5" /> Change block
              </button>
              <button v-if="can.revert && sel.edits && sel.origin !== 'custom'" type="button" :class="onBlue" @click="revert">
                <RotateCcw class="size-3.5" /> Revert
              </button>
              <button v-if="can.edit" type="button" :class="onBlue" title="Empty this slot" @click="unlink">
                <Unlink class="size-3.5" /> Unlink
              </button>
            </template>
            <button v-if="can.edit" type="button" :class="onBlue" title="Remove this slot from the kit" @click="removeSlot">
              <Trash2 class="size-3.5" /> Remove
            </button>
          </template>

          <!-- Empty slot: pick a block -->
          <BlankSlot v-if="!sel.blockSlug" :kit-id="kit.id" :slot="sel" :candidates="candidates ?? []" :in-kit="inKit" :can-use="can.use" :can-create="can.create" />

          <!-- Re-link: pick a different block -->
          <BlankSlot
            v-else-if="relinking"
            mode="relink"
            :kit-id="kit.id"
            :slot="sel"
            :candidates="candidates ?? []"
            :in-kit="inKit"
            :can-use="can.use"
            :can-create="false"
            @cancel="relinking = false"
          />

          <template v-else>
            <p v-if="sel.blockDescription" class="text-sm text-slate-600">{{ sel.blockDescription }}</p>

            <!-- Linked and edited: say so, and offer to keep the edits as a block of their own -->
            <div
              v-if="(sel.edits && sel.origin !== 'custom') || sel.origin === 'custom'"
              class="flex flex-wrap items-center gap-3 rounded-lg border px-3.5 py-2.5"
              :class="sel.origin === 'custom' ? 'border-slate-200 bg-background' : 'border-[#C8D8FB] bg-[#F3F7FF]'"
            >
              <p class="min-w-0 flex-1 text-[13px]">
                <template v-if="sel.origin === 'custom'">This block lives only in this kit. Save it to the library to reuse it elsewhere.</template>
                <template v-else>
                  Linked to <b>{{ sel.blockName }} v{{ sel.version }}</b> with {{ sel.edits }} {{ sel.edits === 1 ? 'edit' : 'edits' }} made in
                  this kit, marked in the table. New library versions still flow in underneath.
                </template>
              </p>
              <button
                v-if="can.create && !savingAs"
                type="button"
                class="inline-flex items-center gap-1.5 rounded-md border bg-background px-2.5 py-1 text-[13px] font-medium hover:bg-muted disabled:opacity-50"
                :disabled="dirty"
                :title="dirty ? 'Save or discard your changes first' : 'Make a new library block from this one plus the edits'"
                @click="startSaveAs"
              >
                <BookPlus class="size-3.5" /> {{ sel.origin === 'custom' ? 'Save to library' : 'Save as new block' }}
              </button>
              <form v-if="savingAs" class="flex w-full flex-wrap items-center gap-2" @submit.prevent="saveAs">
                <input v-model="saveAsName" aria-label="New block name" required class="h-8 min-w-[220px] flex-1 rounded-md border bg-background px-2.5 text-[13px]" />
                <button type="submit" class="rounded-md bg-primary px-3 py-1 text-[13px] font-medium text-primary-foreground">Save block</button>
                <button type="button" class="rounded-md px-3 py-1 text-[13px] text-slate-600 hover:bg-muted" @click="savingAs = false">Cancel</button>
                <p class="w-full text-xs text-muted-foreground">
                  The slot then links the new block with no edits left over<template v-if="sel.origin !== 'custom'">; {{ sel.blockName }} stays as it is</template>.
                </p>
                <p v-if="errors.saveAs" role="alert" class="w-full text-xs text-red-700">{{ errors.saveAs }}</p>
              </form>
            </div>

            <ExplainPanel v-if="can.ai" :href="`${href}/explain`" />
            <div v-if="sel.problem" role="alert" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">{{ sel.problem }}</div>
            <div v-if="sel.conflicts?.length" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
              {{ sel.conflicts.length }} {{ sel.conflicts.length === 1 ? 'edit no longer applies' : 'edits no longer apply' }}: the library version
              removed what {{ sel.conflicts.length === 1 ? 'it' : 'they' }} changed. Save to drop {{ sel.conflicts.length === 1 ? 'it' : 'them' }}, or revert.
            </div>

            <NetworkRulesEditor v-if="sel.blockType === 'network-rules'" v-model="draft" :slot="sel" :can-edit="can.edit" />
            <CredentialEditor v-else-if="sel.blockType === 'credential'" :key="sel.key" v-model="draft" :slot="sel" :can-edit="can.edit" />
            <RawEditor v-else :key="sel.key" v-model="draft" :can-edit="can.edit" />

            <div v-if="errors.payload" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{{ errors.payload }}</div>
            <div v-if="can.edit" class="flex items-center gap-2">
              <button type="button" :class="btn + ' bg-primary text-primary-foreground hover:bg-primary/90'" :disabled="!dirty || saving" @click="save">
                <Save class="size-3.5" /> {{ saving ? 'Saving…' : 'Save to this kit' }}
              </button>
              <button v-if="dirty" type="button" :class="btn + ' text-slate-600 hover:bg-muted'" @click="discard">Discard changes</button>
              <span class="ml-auto text-xs text-muted-foreground">
                {{ sel.origin === 'custom' ? 'Saved into this kit’s own block.' : `Saved as edits on library v${sel.version}.` }}
              </span>
            </div>
          </template>
        </SlotFrame>
      </div>

      <!-- Right: the YAML, always there unless hidden -->
      <aside v-if="!yamlHidden" class="flex w-full shrink-0 flex-col lg:sticky lg:top-4 lg:h-[calc(100vh-5.5rem)] lg:w-[380px] xl:w-[440px]">
        <YamlPanel :compiled="compiled" :slug="kit.slug" :highlight="highlight" class="max-h-[70vh] flex-1 lg:max-h-none" @hide="yamlHidden = true" />
      </aside>
      <button v-else type="button" :class="rail" aria-label="Show YAML" title="Show YAML" @click="yamlHidden = false">
        <FileCode class="size-4" />
        <span class="text-[11px] font-medium [writing-mode:vertical-rl]">YAML</span>
      </button>
    </main>
  </AppShell>
</template>
