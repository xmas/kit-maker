<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { Send, Sparkles, X } from '@lucide/vue'

// Streams the AI's explanation of a slot (POST …/explain, server-sent events).
const props = defineProps<{ href: string }>()

const open = ref(false)
const question = ref('')
const text = ref('')
const error = ref('')
const busy = ref(false)
const meta = ref('')
let abort: AbortController | null = null

watch(
  () => props.href,
  () => close(),
)
onBeforeUnmount(() => abort?.abort())

function close() {
  abort?.abort()
  open.value = false
  text.value = error.value = meta.value = question.value = ''
}

async function ask() {
  abort?.abort()
  abort = new AbortController()
  open.value = true
  busy.value = true
  text.value = error.value = meta.value = ''
  try {
    const res = await fetch(props.href, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
      body: JSON.stringify({ question: question.value }),
      signal: abort.signal,
    })
    if (!res.ok || !res.body) {
      error.value = res.status === 403 ? 'You do not have AI access on this team.' : (await res.text()) || `Request failed (${res.status})`
      return
    }
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
    let buf = ''
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      buf += value
      let i
      while ((i = buf.indexOf('\n\n')) >= 0) {
        handle(buf.slice(0, i))
        buf = buf.slice(i + 2)
      }
    }
  } catch (e) {
    if ((e as Error).name !== 'AbortError') error.value = 'The connection dropped. Try again.'
  } finally {
    busy.value = false
  }
}

function handle(block: string) {
  let event = 'message'
  let data = ''
  for (const line of block.split('\n')) {
    if (line.startsWith('event: ')) event = line.slice(7)
    else if (line.startsWith('data: ')) data += line.slice(6)
  }
  const d = data ? JSON.parse(data) : {}
  if (event === 'delta') text.value += d.text
  else if (event === 'error') error.value = d.message
  else if (event === 'done') meta.value = d.model
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <form class="flex items-center gap-2" @submit.prevent="ask">
      <div class="flex h-9 flex-1 items-center gap-2 rounded-lg border border-ai/30 bg-background px-3 focus-within:border-ai">
        <Sparkles class="size-4 shrink-0 text-ai" aria-hidden="true" />
        <input
          v-model="question"
          aria-label="Ask AI about this block"
          placeholder="Ask about this block, or press Explain"
          class="h-full flex-1 bg-transparent text-[13px] focus:outline-none"
        />
      </div>
      <button
        type="submit"
        :disabled="busy"
        class="inline-flex h-9 items-center gap-1.5 rounded-lg bg-ai px-3 text-[13px] font-medium text-white hover:bg-ai/90 disabled:opacity-60"
      >
        <Send v-if="question" class="size-3.5" />
        <Sparkles v-else class="size-3.5" />
        {{ question ? 'Ask' : 'Explain' }}
      </button>
    </form>
    <div v-if="open" aria-live="polite" class="relative rounded-lg border border-ai/25 bg-ai/5 px-4 py-3 pr-10 text-[13.5px] leading-relaxed">
      <button type="button" aria-label="Close" class="absolute top-2 right-2 rounded p-1 text-slate-500 hover:bg-white" @click="close">
        <X class="size-3.5" />
      </button>
      <p v-if="text" class="whitespace-pre-wrap">{{ text }}<span v-if="busy" class="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-ai align-middle" /></p>
      <p v-else-if="busy" class="text-muted-foreground">Thinking…</p>
      <p v-if="error" role="alert" class="text-red-700">{{ error }}</p>
      <p v-if="meta" class="mt-1.5 text-[11px] text-muted-foreground">AI · {{ meta }} · check anything before you rely on it</p>
    </div>
  </div>
</template>
