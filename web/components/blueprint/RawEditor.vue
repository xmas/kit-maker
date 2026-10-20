<script setup lang="ts">
import { ref, watch } from 'vue'
import { parse, stringify } from 'yaml'
import type { BlockType } from '@/lib/blueprint'

// The editor for block types without a form of their own: the payload as
// YAML (the capability's config, plus optional and description). Item keys
// (_k) stay hidden and are kept on the items they belong to.
const props = defineProps<{ canEdit: boolean; type?: BlockType }>()
const model = defineModel<Record<string, any>>({ required: true })

function strip(v: any): any {
  if (Array.isArray(v)) return v.map(strip)
  if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).filter(([k]) => k !== '_k').map(([k, x]) => [k, strip(x)]))
  return v
}
const show = (v: Record<string, any>) => (Object.keys(v).length ? stringify(strip(v), { lineWidth: 0 }) : '')
const text = ref(show(model.value))
const error = ref('')

watch(text, (v) => {
  try {
    const parsed = parse(v) ?? {}
    if (typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('expected settings as key: value lines')
    for (const k of Object.keys(model.value)) delete model.value[k]
    Object.assign(model.value, parsed)
    error.value = ''
  } catch (e) {
    error.value = (e as Error).message.split('\n')[0] ?? 'invalid YAML'
  }
})
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <div class="flex items-baseline justify-between gap-3">
      <label for="raw-yaml" class="text-[13px] font-medium">Settings (YAML)</label>
      <code v-if="props.type?.capability" class="truncate font-mono text-[11.5px] text-muted-foreground">{{ props.type.capability }}</code>
    </div>
    <p v-if="props.type?.description" class="text-xs text-muted-foreground">{{ props.type.description }}</p>
    <textarea
      id="raw-yaml"
      v-model="text"
      :disabled="!canEdit"
      rows="14"
      spellcheck="false"
      placeholder="optional: true"
      class="rounded-lg border bg-background p-3 font-mono text-[12.5px] leading-relaxed focus:border-primary focus:outline-none"
    />
    <span v-if="error" role="alert" class="text-xs text-destructive">{{ error }}</span>
    <span v-else class="text-[11.5px] text-muted-foreground">optional and description apply to the entry; everything else is its config.</span>
  </div>
</template>
