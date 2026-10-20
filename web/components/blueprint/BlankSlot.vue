<script setup lang="ts">
import { computed, ref } from 'vue'
import { router } from '@inertiajs/vue3'
import { Link2, Plus, Sparkles } from '@lucide/vue'
import { slotHref, type Candidate, type SlotDetail } from '@/lib/blueprint'
import { fuzzyFilter } from '@/lib/fuzzy'
import type { AISearchResult } from '@/lib/aiSearch'
import SearchBox from './SearchBox.vue'

const props = defineProps<{
  blueprintId: string
  slot: SlotDetail
  candidates: Candidate[]
  inBlueprint: string[]
  canUse: boolean
  canCreate: boolean
  canAi?: boolean
}>()

// Blocks this blueprint already links go last and say so. Typing filters
// fuzzily over every field; Ask AI shows its picks in its order, with why.
const query = ref('')
const ai = ref<AISearchResult | null>(null)
const sorted = computed(() =>
  [...props.candidates].sort((a, b) => Number(props.inBlueprint.includes(a.slug)) - Number(props.inBlueprint.includes(b.slug))),
)
const shown = computed<(Candidate & { reason?: string })[]>(() => {
  if (ai.value) {
    return ai.value.matches.flatMap((m) => {
      const c = props.candidates.find((x) => x.id === m.id)
      return c ? [{ ...c, reason: m.reason }] : []
    })
  }
  return fuzzyFilter(query.value, sorted.value, (c) => [
    { text: c.name, weight: 3 },
    { text: c.slug, weight: 2 },
    { text: c.description, weight: 1.5 },
    { text: c.text ?? '', weight: 1 },
  ])
})

function startNew() {
  router.post(`${slotHref(props.blueprintId, props.slot.key)}/custom`, {}, { preserveScroll: true })
}
// Start with AI: a new block for this blueprint, drafted from a description.
const aiPrompt = ref('')
const aiBusy = ref(false)
function startWithAI() {
  if (!aiPrompt.value.trim()) return
  router.post(
    `${slotHref(props.blueprintId, props.slot.key)}/custom`,
    { prompt: aiPrompt.value },
    { preserveScroll: true, onStart: () => (aiBusy.value = true), onFinish: () => (aiBusy.value = false) },
  )
}
function use(c: Candidate) {
  router.post(`${slotHref(props.blueprintId, props.slot.key)}/link`, { blockId: c.id }, { preserveScroll: true })
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <p class="text-sm text-slate-600">
      This slot is empty. Pick a library block to link it; the blueprint keeps following the block's new versions, and any
      edits you make here are kept as overrides.
    </p>
    <SearchBox v-if="candidates.length > 3" v-model="query" v-model:ai="ai" :type="slot.blockType" />
    <div class="grid gap-3 @xl:grid-cols-2 @4xl:grid-cols-3">
      <article v-for="c in shown" :key="c.id" class="flex flex-col gap-2 rounded-lg border bg-background p-3.5">
        <div class="flex items-baseline justify-between gap-2">
          <h3 class="text-sm font-semibold">{{ c.name }}</h3>
          <span class="text-xs text-muted-foreground">v{{ c.version }}<template v-if="c.usedBy"> · used by {{ c.usedBy }}</template></span>
        </div>
        <p class="font-mono text-xs text-muted-foreground">
          {{ c.slug }}<span v-if="inBlueprint.includes(c.slug)" class="ml-2 rounded-full bg-slate-100 px-1.5 py-px font-sans text-[11px] text-slate-600">already in this blueprint</span>
        </p>
        <p v-if="c.description" class="text-[13px] text-slate-600">{{ c.description }}</p>
        <p v-if="c.reason" class="flex gap-1.5 text-[12.5px] text-ai"><Sparkles class="mt-0.5 size-3.5 shrink-0" /> {{ c.reason }}</p>
        <button
          v-if="canUse"
          type="button"
          class="mt-auto inline-flex items-center gap-1.5 self-start rounded-md border border-primary px-2.5 py-1 text-[13px] font-medium text-primary hover:bg-primary/5"
          @click="use(c)"
        >
          <Link2 class="size-3.5" /> Use this block
        </button>
      </article>
    </div>
    <p v-if="!shown.length" class="rounded-lg border border-dashed bg-background p-4 text-sm text-muted-foreground">
      {{ candidates.length ? 'No blocks match.' : 'No library blocks of this type are visible to your teams yet.' }}
    </p>
    <div v-if="canCreate" class="flex flex-wrap items-center gap-3 rounded-lg border border-dashed bg-background p-3.5">
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="text-sm font-semibold">Nothing fits?</span>
        <span class="text-[13px] text-slate-600">Start a new block just for this blueprint. You can share it to the library later.</span>
      </div>
      <button type="button" class="inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-[13px] font-medium hover:bg-muted" @click="startNew">
        <Plus class="size-3.5" /> Start a new block
      </button>
    </div>
    <form v-if="canCreate && canAi" class="flex flex-col gap-2 rounded-lg border border-ai/30 bg-background p-3.5" @submit.prevent="startWithAI">
      <span class="inline-flex items-center gap-1.5 text-sm font-semibold"><Sparkles class="size-3.5 text-ai" /> Start with AI</span>
      <div class="flex gap-2">
        <input
          v-model="aiPrompt"
          class="h-9 min-w-0 flex-1 rounded-lg border bg-background px-3 text-[13.5px] focus:border-ai focus:outline-none"
          :placeholder="`Describe the ${slot.label.toLowerCase()} you need`"
          aria-label="Describe the new block for the AI"
        />
        <button
          type="submit"
          class="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg bg-ai px-3 text-[13px] font-medium text-white hover:bg-ai/90 disabled:opacity-50"
          :disabled="!aiPrompt.trim() || aiBusy"
        >
          {{ aiBusy ? 'Drafting…' : 'Draft a block' }}
        </button>
      </div>
      <span class="text-xs text-muted-foreground">Creates a block for this blueprint from your description. Check it, then save or edit as usual.</span>
    </form>
  </div>
</template>
