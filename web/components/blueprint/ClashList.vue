<script setup lang="ts">
import { Link, router } from '@inertiajs/vue3'
import { slotHref, type Clash } from '@/lib/blueprint'

// Blocks working against each other, each with what fixes it: hiding one
// entry of one block in this blueprint (an override; History or Revert
// brings it back), or opening the blocks involved.
const props = defineProps<{ clashes: Clash[]; blueprintId: string; labels: Record<string, string>; canEdit: boolean; here?: string | null }>()

const kindLabel: Record<Clash['kind'], string> = { blocked: 'Blocked', widened: 'Widened', duplicate: 'Duplicate', gap: 'Missing access', missing: 'Missing value' }

function fix(f: NonNullable<Clash['fixes']>[number]) {
  router.post(`${slotHref(props.blueprintId, f.slot)}/hide`, { path: f.path, key: f.key, back: window.location.pathname }, { preserveScroll: true })
}
</script>

<template>
  <ul class="flex flex-col gap-2">
    <li v-for="(c, i) in clashes" :key="i" class="flex flex-col gap-2 rounded-lg border border-amber-200 bg-amber-50/60 px-3 py-2.5 text-[13px]">
      <p class="leading-snug text-slate-800">
        <span class="mr-1.5 rounded-full bg-amber-100 px-1.5 py-px text-[10.5px] font-semibold text-amber-800">{{ kindLabel[c.kind] }}</span>
        {{ c.message }}
      </p>
      <div class="flex flex-wrap items-center gap-1.5">
        <template v-if="canEdit">
          <button
            v-for="f in c.fixes"
            :key="f.slot + f.key"
            type="button"
            class="rounded-md border bg-background px-2 py-1 text-xs font-medium hover:bg-muted"
            @click="fix(f)"
          >
            {{ f.label }}
          </button>
        </template>
        <Link
          v-for="s in c.slots.filter((k) => k !== here)"
          :key="s"
          :href="slotHref(blueprintId, s)"
          class="rounded-md px-2 py-1 text-xs text-primary hover:bg-primary/5"
          >Open {{ labels[s] ?? s }}</Link
        >
      </div>
    </li>
  </ul>
</template>
