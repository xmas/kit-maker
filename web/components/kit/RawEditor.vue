<script setup lang="ts">
import { ref, watch } from 'vue'

// Escape hatch for any capability type: edit the payload as JSON.
defineProps<{ canEdit: boolean }>()
const model = defineModel<Record<string, any>>({ required: true })
const text = ref(JSON.stringify(model.value, null, 2))
const error = ref('')

watch(text, (v) => {
  try {
    const parsed = JSON.parse(v)
    for (const k of Object.keys(model.value)) delete model.value[k]
    Object.assign(model.value, parsed)
    error.value = ''
  } catch (e) {
    error.value = (e as Error).message
  }
})
</script>

<template>
  <label class="flex flex-col gap-1.5 text-[13px] font-medium">
    Capability payload (JSON)
    <textarea
      v-model="text"
      :disabled="!canEdit"
      rows="14"
      spellcheck="false"
      class="rounded-lg border bg-background p-3 font-mono text-[12.5px] focus:border-primary focus:outline-none"
    />
    <span v-if="error" role="alert" class="text-xs font-normal text-destructive">{{ error }}</span>
  </label>
</template>
