<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Plus, Trash2, Undo2 } from '@lucide/vue'
import type { SlotDetail } from '@/lib/kit'

interface Entry {
  _k: string
  label?: string
  hosts: string[]
  methods?: string[]
  paths?: string[]
}

const props = defineProps<{ slot: SlotDetail; canEdit: boolean }>()
const model = defineModel<Record<string, any>>({ required: true })

const phase = ref<'runtime' | 'install'>('runtime')
const phases = ['runtime', 'install'] as const

// Reads never create keys, so opening a phase doesn't make the draft dirty.
function list(p: string, which: 'allow' | 'deny'): Entry[] {
  return (model.value[p]?.[which] ?? []) as Entry[]
}
function ensure(p: string, which: 'allow' | 'deny'): Entry[] {
  const ph = (model.value[p] ??= {})
  return (ph[which] ??= []) as Entry[]
}
function baseList(p: string, which: 'allow' | 'deny'): Entry[] {
  return (props.slot.base?.[p]?.[which] ?? []) as Entry[]
}

// Each row says where it comes from: inherited from the library version,
// changed here, or added here. Rows removed here show greyed with Restore.
function origin(p: string, which: 'allow' | 'deny', e: Entry): 'inherited' | 'changed' | 'added' {
  const b = baseList(p, which).find((x) => x._k === e._k)
  if (!b) return 'added'
  return JSON.stringify(norm(b)) === JSON.stringify(norm(e)) ? 'inherited' : 'changed'
}
function norm(e: Entry) {
  return { label: e.label ?? '', hosts: e.hosts ?? [], methods: e.methods ?? [], paths: e.paths ?? [] }
}
function removed(p: string, which: 'allow' | 'deny'): Entry[] {
  const keys = new Set(list(p, which).map((e) => e._k))
  return baseList(p, which).filter((b) => !keys.has(b._k))
}
function restore(p: string, which: 'allow' | 'deny', e: Entry) {
  ensure(p, which).push(JSON.parse(JSON.stringify(e)))
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
function add(p: string, which: 'allow' | 'deny') {
  ensure(p, which).push({ _k: Math.random().toString(16).slice(2, 12), label: '', hosts: [] })
}
function del(p: string, which: 'allow' | 'deny', e: Entry) {
  const l = list(p, which)
  l.splice(l.indexOf(e), 1)
}

const counts = computed(() =>
  Object.fromEntries(phases.map((p) => [p, list(p, 'allow').length + list(p, 'deny').length])),
)
watch(
  () => props.slot.key,
  () => (phase.value = counts.value.runtime || !counts.value.install ? 'runtime' : 'install'),
  { immediate: true },
)

const badge: Record<string, string> = {
  inherited: 'bg-slate-100 text-slate-700 border border-slate-200',
  changed: 'bg-[#E8F0FE] text-[#1A4FBF] border border-[#C8D8FB]',
  added: 'bg-green-50 text-green-700 border border-green-200',
}
const badgeLabel: Record<string, string> = { inherited: 'Inherited', changed: 'Changed here', added: 'Added here' }
</script>

<template>
  <div class="flex flex-col gap-4">
    <div role="tablist" aria-label="Phase" class="flex gap-2">
      <button
        v-for="p in phases"
        :key="p"
        type="button"
        role="tab"
        :aria-selected="phase === p"
        class="rounded-full border px-3 py-1 text-[13px]"
        :class="phase === p ? 'border-primary bg-primary text-primary-foreground' : 'bg-background text-slate-600'"
        @click="phase = p"
      >
        {{ p === 'runtime' ? 'Runtime' : 'Install' }} <span class="opacity-75">· {{ counts[p] }}</span>
      </button>
    </div>

    <div v-for="which in (['allow', 'deny'] as const)" :key="which" class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between">
        <h3 class="text-sm font-semibold" :class="which === 'deny' ? 'text-red-700' : ''">
          {{ which === 'allow' ? 'Allow' : 'Deny' }}
        </h3>
        <span class="text-xs text-muted-foreground">
          {{ which === 'allow' ? 'A host with no methods allows any request. Add methods to bound it.' : 'Deny wins over allow.' }}
        </span>
      </div>
      <div class="overflow-x-auto rounded-lg border bg-background">
        <table class="w-full min-w-[640px] border-collapse text-[13px]">
          <thead>
            <tr class="bg-slate-50 text-left text-slate-500">
              <th class="px-3 py-2 font-medium">Rule</th>
              <th class="px-3 py-2 font-medium">Hosts</th>
              <th class="px-3 py-2 font-medium">Methods</th>
              <th class="px-3 py-2 font-medium">Paths</th>
              <th class="px-3 py-2 font-medium">Origin</th>
              <th class="w-10 px-2 py-2"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in list(phase, which)" :key="e._k" class="border-t">
              <td class="px-2 py-1.5">
                <input
                  :value="e.label"
                  :disabled="!canEdit"
                  placeholder="Name this rule"
                  aria-label="Rule name"
                  class="w-full rounded border-transparent bg-transparent px-1 py-1 hover:border-slate-200 focus:border-primary focus:outline-none"
                  @input="e.label = ($event.target as HTMLInputElement).value"
                />
              </td>
              <td class="px-2 py-1.5">
                <input
                  :value="csv(e.hosts)"
                  :disabled="!canEdit"
                  aria-label="Hosts"
                  placeholder="api.example.com"
                  class="w-full rounded border-transparent bg-transparent px-1 py-1 font-mono text-[12.5px] hover:border-slate-200 focus:border-primary focus:outline-none"
                  @change="setCsv(e, 'hosts', ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td class="px-2 py-1.5">
                <input
                  :value="csv(e.methods)"
                  :disabled="!canEdit"
                  aria-label="Methods"
                  placeholder="any"
                  class="w-full rounded border-transparent bg-transparent px-1 py-1 font-mono text-[12.5px] hover:border-slate-200 focus:border-primary focus:outline-none"
                  @change="setCsv(e, 'methods', ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td class="px-2 py-1.5">
                <input
                  :value="csv(e.paths)"
                  :disabled="!canEdit"
                  aria-label="Paths"
                  placeholder="all"
                  class="w-full rounded border-transparent bg-transparent px-1 py-1 font-mono text-[12.5px] hover:border-slate-200 focus:border-primary focus:outline-none"
                  @change="setCsv(e, 'paths', ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td class="px-3 py-1.5">
                <span v-if="slot.origin !== 'custom'" class="rounded-full px-2 py-0.5 text-[11px] font-medium whitespace-nowrap" :class="badge[origin(phase, which, e)]">
                  {{ badgeLabel[origin(phase, which, e)] }}
                </span>
              </td>
              <td class="px-2 py-1.5">
                <button
                  v-if="canEdit"
                  type="button"
                  aria-label="Remove rule"
                  class="rounded p-1.5 text-slate-400 hover:bg-red-50 hover:text-red-600"
                  @click="del(phase, which, e)"
                >
                  <Trash2 class="size-3.5" />
                </button>
              </td>
            </tr>
            <tr v-for="e in removed(phase, which)" :key="'rm-' + e._k" class="border-t bg-slate-50/60 text-slate-400">
              <td class="px-3 py-1.5 line-through">{{ e.label || '—' }}</td>
              <td class="px-3 py-1.5 font-mono text-[12.5px] line-through">{{ csv(e.hosts) }}</td>
              <td class="px-3 py-1.5 font-mono text-[12.5px] line-through">{{ csv(e.methods) || 'any' }}</td>
              <td class="px-3 py-1.5 font-mono text-[12.5px] line-through">{{ csv(e.paths) || 'all' }}</td>
              <td class="px-3 py-1.5">
                <span class="rounded-full border border-red-200 bg-red-50 px-2 py-0.5 text-[11px] font-medium text-red-700">Removed here</span>
              </td>
              <td class="px-2 py-1.5">
                <button
                  v-if="canEdit"
                  type="button"
                  aria-label="Restore rule from the library version"
                  title="Restore"
                  class="rounded p-1.5 text-slate-500 hover:bg-slate-100"
                  @click="restore(phase, which, e)"
                >
                  <Undo2 class="size-3.5" />
                </button>
              </td>
            </tr>
            <tr v-if="!list(phase, which).length && !removed(phase, which).length" class="border-t">
              <td colspan="6" class="px-3 py-3 text-center text-muted-foreground">No {{ which }} rules in this phase.</td>
            </tr>
          </tbody>
        </table>
      </div>
      <button
        v-if="canEdit"
        type="button"
        class="inline-flex items-center gap-1 self-start rounded px-2 py-1 text-[13px] font-medium text-primary hover:bg-primary/5"
        @click="add(phase, which)"
      >
        <Plus class="size-3.5" /> Add {{ which }} rule
      </button>
    </div>
  </div>
</template>
