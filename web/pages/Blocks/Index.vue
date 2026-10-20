<script setup lang="ts">
import { computed, ref } from 'vue'
import { Head, Link, useForm } from '@inertiajs/vue3'
import { Plus } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'
import { sectionStyle, type BlockType } from '@/lib/blueprint'
import { fuzzyFilter } from '@/lib/fuzzy'
import type { AISearchResult } from '@/lib/aiSearch'
import SearchBox from '@/components/blueprint/SearchBox.vue'

interface BlockRow {
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
  updatedAt: string
  abilities: string[] | null
  text: string
}

const props = defineProps<{ blocks: BlockRow[]; types: BlockType[]; abilities: { key: string; label: string }[]; teams: { id: string; name: string }[] }>()

const query = ref('')
const ai = ref<AISearchResult | null>(null)
const typeFilter = ref('')
// Types grouped by outline section, for the pickers.
const typeGroups = computed(() => {
  const groups: { section: string; label: string; types: BlockType[] }[] = []
  for (const t of props.types) {
    let g = groups.find((x) => x.section === t.section)
    if (!g) groups.push((g = { section: t.section, label: sectionStyle[t.section]?.label ?? t.section, types: [] }))
    g.types.push(t)
  }
  return groups
})
// Abilities: what a block lets a sandbox do. Picking several shows blocks
// with any of them; counts follow the search and type filter.
const picked = ref<string[]>([])
const abilityLabel = (k: string) => props.abilities.find((a) => a.key === k)?.label ?? k
function toggleAbility(k: string) {
  picked.value = picked.value.includes(k) ? picked.value.filter((x) => x !== k) : [...picked.value, k]
}
// Search is fuzzy over every field, including what's inside the block.
const matching = computed(() =>
  fuzzyFilter(
    query.value,
    props.blocks.filter((b) => !typeFilter.value || b.type === typeFilter.value),
    (b) => [
      { text: b.name, weight: 3 },
      { text: b.slug, weight: 2 },
      { text: `${b.typeLabel} ${b.type} ${b.team}`, weight: 1.5 },
      { text: b.description, weight: 1.5 },
      { text: (b.abilities ?? []).map(abilityLabel).join(' '), weight: 1.2 },
      { text: b.text ?? '', weight: 1 },
    ],
  ),
)
const abilityCounts = computed(() =>
  props.abilities
    .map((a) => ({ ...a, count: matching.value.filter((b) => b.abilities?.includes(a.key)).length }))
    .filter((a) => a.count > 0 || picked.value.includes(a.key)),
)
const reasons = computed(() => Object.fromEntries((ai.value?.matches ?? []).map((m) => [m.id, m.reason])))
const shown = computed(() => {
  // Ask AI: its picks in its order (the ability chips still narrow them).
  const base = ai.value
    ? ai.value.matches.flatMap((m) => props.blocks.filter((b) => b.id === m.id))
    : matching.value
  return picked.value.length ? base.filter((b) => b.abilities?.some((a) => picked.value.includes(a))) : base
})

const creating = ref(false)
const form = useForm({ teamId: props.teams[0]?.id ?? '', type: 'network-rules', name: '', description: '' })
const formError = computed(() => (form.errors as Record<string, string>).form || form.errors.teamId)

function create() {
  form.post('/blocks')
}

const field = 'h-9 rounded-lg border bg-background px-3 text-[13.5px] focus:border-primary focus:outline-none'
</script>

<template>
  <Head title="Blocks" />
  <AppShell>
    <main class="mx-auto w-full max-w-6xl px-4 py-8">
      <div class="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight">Blocks</h1>
          <p class="text-sm text-muted-foreground">The shared library. Blueprints link these blocks and keep receiving their new versions.</p>
        </div>
        <button
          v-if="teams.length && !creating"
          type="button"
          class="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3.5 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
          @click="creating = true"
        >
          <Plus class="size-4" /> New block
        </button>
      </div>

      <form v-if="creating" class="mb-6 grid gap-3 rounded-xl border bg-background p-4 sm:grid-cols-[180px_1fr_1fr_auto]" @submit.prevent="create">
        <select v-model="form.type" :class="field" aria-label="Block type">
          <optgroup v-for="g in typeGroups" :key="g.section" :label="g.label">
            <option v-for="t in g.types" :key="t.key" :value="t.key">{{ t.label }}</option>
          </optgroup>
        </select>
        <input v-model="form.name" :class="field" placeholder="Name, e.g. npm mirror" aria-label="Name" required autofocus />
        <input v-model="form.description" :class="field" placeholder="What it's for (optional)" aria-label="Description" />
        <div class="flex gap-2">
          <select v-if="teams.length > 1" v-model="form.teamId" :class="field" aria-label="Owner team">
            <option v-for="t in teams" :key="t.id" :value="t.id">{{ t.name }}</option>
          </select>
          <button type="submit" :disabled="form.processing" class="rounded-lg bg-primary px-3.5 text-sm font-medium text-primary-foreground">Create</button>
          <button type="button" class="rounded-lg px-3 text-sm text-slate-600 hover:bg-muted" @click="creating = false">Cancel</button>
        </div>
        <p v-if="formError" role="alert" class="text-sm text-red-700 sm:col-span-4">
          {{ formError }}
        </p>
      </form>

      <div class="mb-4 flex flex-wrap gap-2">
        <SearchBox v-model="query" v-model:ai="ai" :type="typeFilter" class="min-w-[240px] flex-1" />
        <select v-model="typeFilter" :class="field" aria-label="Filter by type">
          <option value="">All types</option>
          <optgroup v-for="g in typeGroups" :key="g.section" :label="g.label">
            <option v-for="t in g.types" :key="t.key" :value="t.key">{{ t.label }}</option>
          </optgroup>
        </select>
      </div>

      <div role="group" aria-label="Filter by ability" class="mb-4 flex flex-wrap items-center gap-1.5">
        <span class="mr-1 text-xs font-medium text-muted-foreground">Can</span>
        <button
          v-for="a in abilityCounts"
          :key="a.key"
          type="button"
          class="inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs"
          :class="picked.includes(a.key) ? 'border-primary bg-primary font-semibold text-primary-foreground' : 'bg-background text-slate-700 hover:bg-muted'"
          :aria-pressed="picked.includes(a.key)"
          @click="toggleAbility(a.key)"
        >
          {{ a.label }} <span :class="picked.includes(a.key) ? 'opacity-80' : 'text-muted-foreground'">{{ a.count }}</span>
        </button>
        <button v-if="picked.length" type="button" class="ml-1 text-xs text-primary hover:underline" @click="picked = []">Clear</button>
      </div>

      <div class="overflow-hidden rounded-xl border bg-background">
        <Link
          v-for="b in shown"
          :key="b.id"
          :href="`/blocks/${b.id}`"
          class="flex items-center gap-3 border-b px-4 py-3 last:border-b-0 hover:bg-slate-50"
        >
          <span
            class="flex size-8 shrink-0 items-center justify-center rounded-lg text-xs font-bold"
            :style="{ background: sectionStyle[b.section]?.bg, color: sectionStyle[b.section]?.fg }"
          >
            {{ b.typeLabel.slice(0, 1) }}
          </span>
          <div class="flex min-w-0 flex-1 flex-col">
            <span class="text-sm font-semibold">{{ b.name }}</span>
            <span class="truncate text-[13px] text-muted-foreground">{{ b.description || b.slug }}</span>
            <span v-if="reasons[b.id]" class="text-[12.5px] text-ai">{{ reasons[b.id] }}</span>
            <span v-if="b.abilities?.length" class="mt-1 flex flex-wrap gap-1">
              <span
                v-for="a in b.abilities"
                :key="a"
                class="rounded-full px-1.5 py-px text-[10.5px] font-medium"
                :class="picked.includes(a) ? 'bg-primary/10 text-primary' : 'bg-slate-100 text-slate-600'"
                >{{ abilityLabel(a) }}</span
              >
            </span>
          </div>
          <span class="hidden text-xs text-muted-foreground sm:inline">{{ b.typeLabel }} · {{ b.team }}</span>
          <span class="w-24 text-right text-xs text-muted-foreground">used by {{ b.usedBy }}</span>
          <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-semibold text-slate-700">v{{ b.version }}</span>
        </Link>
        <p v-if="!shown.length" class="px-4 py-8 text-center text-sm text-muted-foreground">
          {{ blocks.length ? 'No blocks match.' : 'No blocks yet.' }}
        </p>
      </div>
    </main>
  </AppShell>
</template>
