<script setup lang="ts">
import { ref } from 'vue'
import { Sparkles } from '@lucide/vue'

// "Draft with AI" for a filled slot: describe what the block should do and
// the AI's version arrives as an unsaved draft (the YAML panel shows what
// changes; nothing is saved until Save).
const props = defineProps<{ href: string }>()
const emit = defineEmits<{ draft: [payload: Record<string, any>] }>()
const prompt = ref('')
const busy = ref(false)
const note = ref('')
const error = ref('')

async function run() {
  if (!prompt.value.trim() || busy.value) return
  busy.value = true
  note.value = error.value = ''
  try {
    const res = await fetch(`${props.href}/draft`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ prompt: prompt.value }),
    })
    if (!res.ok) throw new Error(`the AI request failed (${res.status})`)
    const out: { payload?: Record<string, any>; note?: string; error?: string } = await res.json()
    if (out.error) throw new Error(out.error)
    note.value = out.note ?? ''
    if (out.payload) emit('draft', out.payload)
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <form class="flex gap-2" @submit.prevent="run">
      <div class="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-lg border border-ai/30 bg-background px-3 focus-within:border-ai">
        <Sparkles class="size-3.5 shrink-0 text-ai" aria-hidden="true" />
        <input
          v-model="prompt"
          class="min-w-0 flex-1 bg-transparent text-[13.5px] focus:outline-none"
          placeholder="Describe what this block should do, e.g. read-only access to our Jira API"
          aria-label="Describe the block for the AI"
        />
      </div>
      <button
        type="submit"
        class="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg bg-ai px-3 text-[13px] font-medium text-white hover:bg-ai/90 disabled:opacity-50"
        :disabled="!prompt.trim() || busy"
      >
        {{ busy ? 'Drafting…' : 'Draft with AI' }}
      </button>
    </form>
    <p v-if="error" role="alert" class="text-xs text-destructive">{{ error }}</p>
    <p v-else-if="note" class="text-xs text-ai">{{ note }}</p>
  </div>
</template>
