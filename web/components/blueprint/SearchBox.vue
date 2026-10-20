<script setup lang="ts">
import { ref } from 'vue'
import { Search, Sparkles, X } from '@lucide/vue'
import { aiSearch, type AISearchResult } from '@/lib/aiSearch'

// A search box with fuzzy matching as you type (the parent filters with
// v-model) and "Ask AI" for a plain-language need, whose ranked answer
// the parent shows through `ai`.
const props = defineProps<{ type?: string; placeholder?: string }>()
const query = defineModel<string>({ required: true })
const ai = defineModel<AISearchResult | null>('ai', { default: null })
const asking = ref(false)
const error = ref('')

async function ask() {
  if (!query.value.trim() || asking.value) return
  asking.value = true
  error.value = ''
  try {
    ai.value = await aiSearch(query.value, props.type)
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    asking.value = false
  }
}
function clear() {
  query.value = ''
  ai.value = null
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <form class="flex gap-2" @submit.prevent="ask">
      <div class="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-lg border bg-background px-3 focus-within:border-primary">
        <Search class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <input
          v-model="query"
          class="min-w-0 flex-1 bg-transparent text-[13.5px] focus:outline-none"
          :placeholder="placeholder ?? 'Search names, hosts, commands, anything'"
          aria-label="Search blocks"
          @input="ai = null"
        />
        <button v-if="query" type="button" class="text-muted-foreground hover:text-foreground" aria-label="Clear search" @click="clear">
          <X class="size-3.5" />
        </button>
      </div>
      <button
        type="submit"
        class="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg bg-ai px-3 text-[13px] font-medium text-white hover:bg-ai/90 disabled:opacity-50"
        :disabled="!query.trim() || asking"
        title="Describe what you need and let AI pick the blocks"
      >
        <Sparkles class="size-3.5" /> {{ asking ? 'Asking…' : 'Ask AI' }}
      </button>
    </form>
    <p v-if="error" role="alert" class="text-xs text-destructive">{{ error }}</p>
    <p v-else-if="ai" class="text-xs" :class="ai.source === 'ai' ? 'text-ai' : 'text-muted-foreground'">
      {{ ai.source === 'ai' ? `AI picked ${ai.matches.length} ${ai.matches.length === 1 ? 'block' : 'blocks'}, best first.` : ai.message }}
      <button type="button" class="ml-1 text-primary hover:underline" @click="ai = null">Back to typed search</button>
    </p>
  </div>
</template>
