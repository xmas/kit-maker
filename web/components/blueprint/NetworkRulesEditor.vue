<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronDown, ChevronRight, Plus, RotateCcw, Trash2, Undo2 } from '@lucide/vue'
import { useDismiss } from '@/lib/useDismiss'
import MethodSelect from './MethodSelect.vue'
import type { SlotDetail } from '@/lib/blueprint'

interface Entry {
  _k: string
  label?: string
  hosts: string[]
  methods?: string[]
  paths?: string[]
}
type Which = 'allow' | 'deny'
type Phase = 'runtime' | 'install'
type Kind = 'inherited' | 'changed' | 'added'

const props = defineProps<{ slot: SlotDetail; canEdit: boolean }>()
const model = defineModel<Record<string, any>>({ required: true })

// Each phase is a section that opens and closes (chevron).
const openPhases = ref<Record<Phase, boolean>>({ runtime: true, install: false })
const phases = ['runtime', 'install'] as const

// Reads never create keys, so opening a phase doesn't make the draft dirty.
function list(p: string, which: Which): Entry[] {
  return (model.value[p]?.[which] ?? []) as Entry[]
}
function ensure(p: string, which: Which): Entry[] {
  const ph = (model.value[p] ??= {})
  return (ph[which] ??= []) as Entry[]
}
function baseList(p: string, which: Which): Entry[] {
  return (props.slot.base?.[p]?.[which] ?? []) as Entry[]
}
// A blueprint-only block has no library version to compare against.
const tracksLibrary = computed(() => props.slot.origin !== 'custom' && !!props.slot.base)

// Each row says where it comes from: the library version as is, changed in
// this blueprint, or added in this blueprint. Library rows removed here show struck out.
function origin(p: string, which: Which, e: Entry): Kind {
  if (!tracksLibrary.value) return 'inherited'
  const b = baseList(p, which).find((x) => x._k === e._k)
  if (!b) return 'added'
  return JSON.stringify(norm(b)) === JSON.stringify(norm(e)) ? 'inherited' : 'changed'
}
function norm(e: Entry) {
  return { label: e.label ?? '', hosts: e.hosts ?? [], methods: e.methods ?? [], paths: e.paths ?? [] }
}
function removed(p: string, which: Which): Entry[] {
  if (!tracksLibrary.value) return []
  const keys = new Set(list(p, which).map((e) => e._k))
  return baseList(p, which).filter((b) => !keys.has(b._k))
}
function restore(p: string, which: Which, e: Entry) {
  // Put it back where it was in the library version.
  const l = ensure(p, which)
  const order = baseList(p, which).map((b) => b._k)
  const at = l.findIndex((x) => order.indexOf(x._k) > order.indexOf(e._k))
  l.splice(at < 0 ? l.length : at, 0, JSON.parse(JSON.stringify(e)))
}
function undoChange(p: string, which: Which, e: Entry) {
  const b = baseList(p, which).find((x) => x._k === e._k)
  const l = list(p, which)
  if (b) l.splice(l.indexOf(e), 1, JSON.parse(JSON.stringify(b)))
}

// Every edit made in this blueprint, for the callout above the tables.
interface Edit {
  p: Phase
  which: Which
  e: Entry
  kind: 'changed' | 'added' | 'removed'
}
const edits = computed<Edit[]>(() => {
  const out: Edit[] = []
  for (const p of phases) {
    for (const which of ['allow', 'deny'] as const) {
      for (const e of list(p, which)) {
        const k = origin(p, which, e)
        if (k !== 'inherited') out.push({ p, which, e, kind: k })
      }
      for (const e of removed(p, which)) out.push({ p, which, e, kind: 'removed' })
    }
  }
  return out
})
function undo(x: Edit) {
  if (x.kind === 'changed') undoChange(x.p, x.which, x.e)
  else if (x.kind === 'added') del(x.p, x.which, x.e)
  else restore(x.p, x.which, x.e)
}
function undoAll() {
  for (const x of [...edits.value]) undo(x)
  listOpen.value = false
}
const listOpen = ref(false)
const stripRoot = ref<HTMLElement | null>(null)
useDismiss(stripRoot, listOpen)
watch(
  () => edits.value.length,
  (n) => n === 0 && (listOpen.value = false),
)
function editName(e: Entry) {
  return e.label || (e.hosts ?? []).join(', ') || 'Unnamed rule'
}

function csv(v?: string[]) {
  return (v ?? []).join(', ')
}
function setCsv(e: Entry, field: 'hosts' | 'methods' | 'paths', value: string) {
  const parts = value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => (field === 'methods' ? s.toUpperCase() : s))
  if (parts.length) e[field] = parts
  else if (field === 'hosts') e.hosts = []
  else delete e[field]
}
// An empty method list is "any method".
function setMethods(e: Entry, methods: string[]) {
  if (methods.length) e.methods = methods
  else delete e.methods
}
// The two phases of network-policy@2, said plainly.
const phaseHelp: Record<string, string> = {
  runtime: 'What the agent can reach while it works in the sandbox.',
  install: 'What install steps can reach while the sandbox is created. Separate from Runtime: no rules here means no network for them.',
}
const phaseLabel: Record<string, string> = { runtime: 'Runtime', install: 'Install' }
// Each phase has its colour (Docker blue, teal): violet means AI, and green,
// amber and red already mean added, changed and removed.
const phaseColor: Record<string, { edge: string; text: string; chip: string }> = {
  runtime: { edge: 'border-l-primary', text: 'text-primary', chip: 'bg-primary/10 text-primary' },
  install: { edge: 'border-l-teal-600', text: 'text-teal-700', chip: 'bg-teal-50 text-teal-700' },
}
function addInClosed(p: Phase) {
  openPhases.value[p] = true
  add(p, 'allow')
}
function add(p: string, which: Which) {
  ensure(p, which).push({ _k: Math.random().toString(16).slice(2, 12), label: '', hosts: [] })
}
function del(p: string, which: Which, e: Entry) {
  const l = list(p, which)
  l.splice(l.indexOf(e), 1)
}

const counts = computed(() => Object.fromEntries(phases.map((p) => [p, list(p, 'allow').length + list(p, 'deny').length])))
const editCounts = computed(() => Object.fromEntries(phases.map((p) => [p, edits.value.filter((x) => x.p === p).length])))
// A slot opens with the phases that have rules or edits (Runtime when neither does).
watch(
  () => props.slot.key,
  () => {
    const has = (p: Phase) => (counts.value[p] ?? 0) > 0 || (editCounts.value[p] ?? 0) > 0
    openPhases.value = { runtime: has('runtime') || !has('install'), install: has('install') }
  },
  { immediate: true },
)

// Same colours as the YAML: added green, changed amber, removed red.
const rowTint: Record<Kind, string> = {
  inherited: '',
  changed: 'bg-amber-50/70 shadow-[inset_3px_0_0_#F59E0B]',
  added: 'bg-emerald-50/70 shadow-[inset_3px_0_0_#10B981]',
}
const badge: Record<string, string> = {
  changed: 'bg-amber-100 text-amber-800',
  added: 'bg-emerald-100 text-emerald-800',
  removed: 'bg-red-100 text-red-800',
}
const badgeLabel: Record<string, string> = { changed: 'Changed here', added: 'Added here', removed: 'Removed here' }
const cell =
  'w-full rounded border border-transparent bg-transparent px-1 py-1 hover:border-slate-200 focus:border-primary focus:bg-white focus:outline-none'
</script>

<template>
  <div class="flex flex-col gap-4">
    <!-- Edits strip: always one line high (even with no edits), so editing
         never moves the tables. The list opens over the content. -->
    <div v-if="tracksLibrary" ref="stripRoot" class="relative">
      <div
        class="flex h-9 items-center gap-2 rounded-lg border px-3 text-[13px]"
        :class="edits.length ? 'border-amber-200 bg-amber-50/60' : 'border-slate-200 bg-background text-muted-foreground'"
      >
        <span v-if="edits.length" class="size-1.5 shrink-0 rounded-full bg-amber-500" aria-hidden="true" />
        <span class="min-w-0 truncate">
          <template v-if="edits.length">
            <b class="font-semibold text-foreground">{{ edits.length }} {{ edits.length === 1 ? 'edit' : 'edits' }}</b> to {{ slot.blockName }} in this blueprint
          </template>
          <template v-else>No edits: same as {{ slot.blockName }} v{{ slot.version }}</template>
        </span>
        <span class="flex-1" />
        <template v-if="edits.length">
          <button
            v-if="canEdit"
            type="button"
            class="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[12px] text-slate-600 hover:bg-white"
            @click="undoAll"
          >
            <RotateCcw class="size-3" /> Undo all
          </button>
          <button
            type="button"
            class="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[12px] font-medium text-primary hover:bg-white"
            :aria-expanded="listOpen"
            @click="listOpen = !listOpen"
          >
            {{ listOpen ? 'Hide' : 'Show' }} <ChevronDown class="size-3 transition-transform" :class="listOpen ? 'rotate-180' : ''" />
          </button>
        </template>
      </div>
      <div
        v-if="listOpen && edits.length"
        role="dialog"
        aria-label="Edits in this blueprint"
        class="absolute top-full right-0 left-0 z-30 mt-1 max-h-72 overflow-y-auto rounded-lg border bg-background p-2 shadow-[0_12px_32px_rgba(15,23,42,0.16)]"
      >
        <ul class="flex flex-col">
          <li v-for="x in edits" :key="x.kind + x.p + x.which + x.e._k" class="flex items-center gap-2 rounded-md px-1.5 py-1 text-[13px] hover:bg-muted/60">
            <span class="w-[92px] shrink-0 rounded px-1.5 py-px text-center text-[11px] font-medium" :class="badge[x.kind]">{{ badgeLabel[x.kind] }}</span>
            <button type="button" class="min-w-0 truncate text-left hover:underline" :title="`Show in ${x.p}`" @click="openPhases[x.p] = true; listOpen = false">
              <span :class="x.kind === 'removed' ? 'line-through' : ''">{{ editName(x.e) }}</span>
              <span class="text-muted-foreground"> · {{ x.p }} {{ x.which }}</span>
            </button>
            <span class="flex-1" />
            <button
              v-if="canEdit"
              type="button"
              class="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[12px] font-medium text-primary hover:bg-white"
              :aria-label="`Undo: ${badgeLabel[x.kind]} ${editName(x.e)}`"
              @click="undo(x)"
            >
              <Undo2 class="size-3" /> Undo
            </button>
          </li>
        </ul>
        <p class="px-1.5 pt-1.5 text-[11.5px] text-muted-foreground">Undo changes the draft; save to keep it.</p>
      </div>
    </div>

    <!-- The two phases of network-policy@2, each a section that opens and closes. -->
    <section v-for="p in phases" :key="p" class="rounded-xl border border-l-4 bg-background" :class="phaseColor[p]?.edge">
      <div class="flex items-center gap-2 px-3 py-2.5">
        <button
          type="button"
          class="flex min-w-0 flex-1 flex-col items-start gap-0.5 text-left"
          :aria-expanded="openPhases[p]"
          :aria-controls="`phase-${slot.key}-${p}`"
          @click="openPhases[p] = !openPhases[p]"
        >
          <span class="flex items-center gap-2">
            <ChevronRight class="size-4 shrink-0 transition-transform" :class="[phaseColor[p]?.text, openPhases[p] ? 'rotate-90' : '']" aria-hidden="true" />
            <span class="text-sm font-semibold" :class="phaseColor[p]?.text">{{ phaseLabel[p] }}</span>
            <span class="shrink-0 rounded-full px-2 py-px text-[11.5px] font-medium" :class="phaseColor[p]?.chip">{{ counts[p] }} {{ counts[p] === 1 ? 'rule' : 'rules' }}</span>
            <span v-if="editCounts[p]" class="size-1.5 shrink-0 rounded-full bg-amber-500" :title="`${editCounts[p]} edits here`" />
          </span>
          <span class="pl-6 text-xs leading-snug text-muted-foreground">{{ phaseHelp[p] }}</span>
        </button>
        <button
          v-if="canEdit && !openPhases[p]"
          type="button"
          class="inline-flex shrink-0 items-center gap-1 rounded px-2 py-1 text-[13px] font-medium text-primary hover:bg-primary/5"
          @click="addInClosed(p)"
        >
          <Plus class="size-3.5" /> Add rule
        </button>
      </div>
      <div v-if="openPhases[p]" :id="`phase-${slot.key}-${p}`" class="flex flex-col gap-4 border-t px-3 pt-3 pb-3">
        <div v-for="which in (['allow', 'deny'] as const)" :key="which" class="flex flex-col gap-2">
          <div class="flex items-baseline justify-between gap-3">
            <h3 class="text-sm font-semibold" :class="which === 'deny' ? 'text-red-700' : ''">{{ which === 'allow' ? 'Allow' : 'Deny' }}</h3>
            <span class="text-right text-xs text-muted-foreground">
              {{ which === 'allow' ? 'A host with no methods allows any request. Add methods to bound it.' : 'Deny wins over allow.' }}
            </span>
          </div>
          <div class="overflow-hidden rounded-lg border bg-background">
            <table class="w-full table-fixed border-collapse text-[13px]">
              <colgroup>
                <col class="w-[26%]" />
                <col class="w-[28%]" />
                <col class="w-[22%]" />
                <col />
                <col class="w-[64px]" />
              </colgroup>
              <thead>
                <tr class="bg-slate-50 text-left text-slate-500">
                  <th class="px-3 py-2 font-medium">Rule</th>
                  <th class="px-3 py-2 font-medium">Hosts</th>
                  <th class="px-3 py-2 font-medium">Methods</th>
                  <th class="px-3 py-2 font-medium">Paths</th>
                  <th><span class="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="e in list(p, which)" :key="e._k" class="border-t align-top" :class="rowTint[origin(p, which, e)]">
                  <td class="px-2 py-1.5">
                    <input
                      :value="e.label"
                      :disabled="!canEdit"
                      placeholder="Name this rule"
                      aria-label="Rule name"
                      :class="cell"
                      @input="e.label = ($event.target as HTMLInputElement).value"
                    />
                    <span
                      v-if="origin(p, which, e) !== 'inherited'"
                      class="ml-1 inline-block rounded px-1.5 py-px text-[10.5px] font-medium"
                      :class="badge[origin(p, which, e)]"
                      >{{ badgeLabel[origin(p, which, e)] }}</span
                    >
                  </td>
                  <td class="px-2 py-1.5">
                    <input :value="csv(e.hosts)" :disabled="!canEdit" aria-label="Hosts" placeholder="api.example.com" :class="cell + ' font-mono text-[12.5px]'" @input="setCsv(e, 'hosts', ($event.target as HTMLInputElement).value)" />
                  </td>
                  <td class="px-2 py-1.5">
                    <MethodSelect
                      :model-value="e.methods ?? []"
                      :disabled="!canEdit"
                      :has-paths="!!e.paths?.length"
                      :wildcard-host="(e.hosts ?? []).some((h) => h.includes('*'))"
                      @update:model-value="setMethods(e, $event)"
                    />
                  </td>
                  <td class="px-2 py-1.5">
                    <input :value="csv(e.paths)" :disabled="!canEdit" aria-label="Paths" placeholder="all" :class="cell + ' font-mono text-[12.5px]'" @input="setCsv(e, 'paths', ($event.target as HTMLInputElement).value)" />
                  </td>
                  <td class="px-1 py-1.5 text-right whitespace-nowrap">
                    <button
                      v-if="canEdit && origin(p, which, e) === 'changed'"
                      type="button"
                      aria-label="Undo this change"
                      title="Undo this change"
                      class="rounded p-1.5 text-amber-700 hover:bg-amber-100"
                      @click="undoChange(p, which, e)"
                    >
                      <Undo2 class="size-3.5" />
                    </button>
                    <button v-if="canEdit" type="button" aria-label="Remove rule" title="Remove rule" class="rounded p-1.5 text-slate-400 hover:bg-red-50 hover:text-red-600" @click="del(p, which, e)">
                      <Trash2 class="size-3.5" />
                    </button>
                  </td>
                </tr>
                <tr v-for="e in removed(p, which)" :key="'rm-' + e._k" class="border-t bg-red-50/50 text-slate-400 shadow-[inset_3px_0_0_#F87171]">
                  <td class="px-3 py-1.5">
                    <span class="line-through">{{ e.label || '—' }}</span>
                    <span class="ml-1 inline-block rounded bg-red-100 px-1.5 py-px text-[10.5px] font-medium text-red-800">Removed here</span>
                  </td>
                  <td class="truncate px-3 py-1.5 font-mono text-[12.5px] line-through">{{ csv(e.hosts) }}</td>
                  <td class="truncate px-3 py-1.5 font-mono text-[12.5px] line-through">{{ csv(e.methods) || 'any' }}</td>
                  <td class="truncate px-3 py-1.5 font-mono text-[12.5px] line-through">{{ csv(e.paths) || 'all' }}</td>
                  <td class="px-1 py-1.5 text-right">
                    <button v-if="canEdit" type="button" aria-label="Restore rule from the library version" title="Restore" class="rounded p-1.5 text-slate-500 hover:bg-white" @click="restore(p, which, e)">
                      <Undo2 class="size-3.5" />
                    </button>
                  </td>
                </tr>
                <tr v-if="!list(p, which).length && !removed(p, which).length" class="border-t">
                  <td colspan="5" class="px-3 py-3 text-center text-muted-foreground">No {{ which }} rules in this phase.</td>
                </tr>
              </tbody>
            </table>
          </div>
          <button
            v-if="canEdit"
            type="button"
            class="inline-flex items-center gap-1 self-start rounded px-2 py-1 text-[13px] font-medium text-primary hover:bg-primary/5"
            @click="add(p, which)"
          >
            <Plus class="size-3.5" /> Add {{ which }} rule
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
