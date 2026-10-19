<script setup lang="ts">
import { computed } from 'vue'
import { router } from '@inertiajs/vue3'
import { Link2, Plus, Sparkles } from '@lucide/vue'
import { slotHref, type Candidate, type SlotDetail } from '@/lib/kit'

// One picker for two jobs: filling an empty slot, and re-linking a filled
// slot to a different block (mode "relink").
const props = defineProps<{
  kitId: string
  slot: SlotDetail
  candidates: Candidate[]
  inKit: string[]
  canUse: boolean
  canCreate: boolean
  mode?: 'fill' | 'relink'
}>()
const emit = defineEmits<{ cancel: [] }>()
const relink = computed(() => props.mode === 'relink')

// Blocks this kit already links go last and say so.
const sorted = computed(() =>
  [...props.candidates].filter((c) => !relink.value || c.slug !== props.slot.blockSlug).sort((a, b) => Number(props.inKit.includes(a.slug)) - Number(props.inKit.includes(b.slug))),
)

function startNew() {
  router.post(`${slotHref(props.kitId, props.slot.key)}/custom`, {}, { preserveScroll: true })
}
function use(c: Candidate) {
  if (relink.value) {
    const n = props.slot.edits
    if (n && !confirm(`Switch to ${c.name}? The ${n} ${n === 1 ? 'edit' : 'edits'} made here belong to ${props.slot.blockName} and will be dropped. Use "Save as new block" first to keep them.`)) return
    router.post(`${slotHref(props.kitId, props.slot.key)}/relink`, { blockId: c.id }, { preserveScroll: true })
    return
  }
  router.post(`${slotHref(props.kitId, props.slot.key)}/link`, { blockId: c.id }, { preserveScroll: true })
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div v-if="relink" class="flex items-center justify-between gap-2">
      <p class="text-sm font-medium">Pick a different block for this slot</p>
      <button type="button" class="rounded-md px-2.5 py-1 text-[13px] text-slate-600 hover:bg-muted" @click="emit('cancel')">Cancel</button>
    </div>
    <p v-else class="text-sm text-slate-600">
      This slot is empty. Pick a library block to link it; the kit keeps following the block's new versions, and any
      edits you make here are kept as overrides.
    </p>
    <div class="grid gap-3 @xl:grid-cols-2 @4xl:grid-cols-3">
      <article v-for="c in sorted" :key="c.id" class="flex flex-col gap-2 rounded-lg border bg-background p-3.5">
        <div class="flex items-baseline justify-between gap-2">
          <h3 class="text-sm font-semibold">{{ c.name }}</h3>
          <span class="text-xs text-muted-foreground">v{{ c.version }}<template v-if="c.usedBy"> · used by {{ c.usedBy }}</template></span>
        </div>
        <p class="font-mono text-xs text-muted-foreground">
          {{ c.slug }}<span v-if="inKit.includes(c.slug)" class="ml-2 rounded-full bg-slate-100 px-1.5 py-px font-sans text-[11px] text-slate-600">already in this kit</span>
        </p>
        <p v-if="c.description" class="text-[13px] text-slate-600">{{ c.description }}</p>
        <button
          v-if="canUse"
          type="button"
          class="mt-auto inline-flex items-center gap-1.5 self-start rounded-md border border-primary px-2.5 py-1 text-[13px] font-medium text-primary hover:bg-primary/5"
          @click="use(c)"
        >
          <Link2 class="size-3.5" /> {{ relink ? 'Switch to this block' : 'Use this block' }}
        </button>
      </article>
    </div>
    <p v-if="!sorted.length" class="rounded-lg border border-dashed bg-background p-4 text-sm text-muted-foreground">
      No library blocks of this type are visible to your teams yet.
    </p>
    <div v-if="canCreate && !relink" class="flex flex-wrap items-center gap-3 rounded-lg border border-dashed bg-background p-3.5">
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="text-sm font-semibold">Nothing fits?</span>
        <span class="text-[13px] text-slate-600">Start a new block just for this kit. You can share it to the library later.</span>
      </div>
      <button type="button" class="inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-[13px] font-medium hover:bg-muted" @click="startNew">
        <Plus class="size-3.5" /> Start a new block
      </button>
    </div>
    <p v-if="!relink" class="flex items-center gap-1.5 text-xs text-muted-foreground">
      <Sparkles class="size-3.5 text-ai" /> Drafting a new block with AI arrives with the AI client.
    </p>
  </div>
</template>
