<script setup lang="ts">
import { computed, ref } from 'vue'
import { Head, Link, useForm } from '@inertiajs/vue3'
import { Plus, Search } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'
import { sectionStyle, type BlockType } from '@/lib/kit'

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
}

const props = defineProps<{ blocks: BlockRow[]; types: BlockType[]; teams: { id: string; name: string }[] }>()

const query = ref('')
const typeFilter = ref('')
const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return props.blocks.filter(
    (b) =>
      (!typeFilter.value || b.type === typeFilter.value) &&
      (!q || [b.name, b.slug, b.description].some((s) => s.toLowerCase().includes(q))),
  )
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
          <p class="text-sm text-muted-foreground">The shared library. Kits link these blocks and keep receiving their new versions.</p>
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
          <option v-for="t in types" :key="t.key" :value="t.key">{{ t.label }}</option>
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
        <div class="flex h-9 min-w-[240px] flex-1 items-center gap-2 rounded-lg border bg-background px-3">
          <Search class="size-4 text-muted-foreground" aria-hidden="true" />
          <input v-model="query" class="flex-1 bg-transparent text-[13.5px] focus:outline-none" placeholder="Search blocks" aria-label="Search blocks" />
        </div>
        <select v-model="typeFilter" :class="field" aria-label="Filter by type">
          <option value="">All types</option>
          <option v-for="t in types" :key="t.key" :value="t.key">{{ t.label }}</option>
        </select>
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
