<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Check, ChevronDown } from '@lucide/vue'

// The methods of a network rule, picked from a list. "Any method" is an
// empty list: the rule allows (or denies) every request to its hosts. The
// list opens over the page (the rules table clips its overflow), below the
// cell or above it near the bottom of the screen.
const props = defineProps<{
  modelValue: string[]
  disabled?: boolean
  // Paths need methods, and methods need literal hosts (blocktypes rules).
  hasPaths?: boolean
  wildcardHost?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [string[]] }>()

const COMMON = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS']
// Methods the rule already has that aren't common ones stay pickable.
const choices = computed(() => [...COMMON, ...props.modelValue.filter((m) => !COMMON.includes(m))])
const any = computed(() => props.modelValue.length === 0)
const label = computed(() => (any.value ? 'Any' : props.modelValue.join(', ')))

const open = ref(false)
const button = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const pos = ref({ top: 0, left: 0, up: false })

function place() {
  const r = button.value?.getBoundingClientRect()
  if (!r) return
  const up = window.innerHeight - r.bottom < 320 && r.top > 320
  pos.value = { top: up ? r.top - 4 : r.bottom + 4, left: Math.min(r.left, window.innerWidth - 232), up }
}
async function toggle() {
  if (props.disabled) return
  open.value = !open.value
  if (open.value) {
    place()
    await nextTick()
    panel.value?.querySelector<HTMLInputElement>('input:not(:disabled)')?.focus()
  }
}

function setAny() {
  emit('update:modelValue', [])
}
function flip(m: string) {
  const next = props.modelValue.includes(m) ? props.modelValue.filter((x) => x !== m) : [...props.modelValue, m]
  // Keep the usual order: common methods first, as listed.
  emit('update:modelValue', choices.value.filter((c) => next.includes(c)))
}

const onDown = (e: MouseEvent) => {
  const t = e.target as Node
  if (!button.value?.contains(t) && !panel.value?.contains(t)) open.value = false
}
const onKey = (e: KeyboardEvent) => {
  if (e.key === 'Escape' && open.value) {
    open.value = false
    button.value?.focus()
  }
}
const close = () => (open.value = false)
function listen(on: boolean) {
  if (on) {
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
  } else {
    document.removeEventListener('mousedown', onDown)
    document.removeEventListener('keydown', onKey)
    window.removeEventListener('scroll', close, true)
    window.removeEventListener('resize', close)
  }
}
watch(open, (o) => listen(o))
onBeforeUnmount(() => listen(false))
</script>

<template>
  <button
    ref="button"
    type="button"
    :disabled="disabled"
    aria-label="Methods"
    aria-haspopup="true"
    :aria-expanded="open"
    class="flex w-full items-center gap-1 rounded border border-transparent bg-transparent px-1 py-1 text-left font-mono text-[12.5px] hover:border-slate-200 focus:border-primary focus:bg-white focus:outline-none disabled:hover:border-transparent"
    :class="[open ? 'border-primary bg-white' : '', any ? 'text-muted-foreground' : '']"
    @click="toggle"
  >
    <span class="min-w-0 flex-1 truncate">{{ label }}</span>
    <ChevronDown v-if="!disabled" class="size-3 shrink-0 opacity-50" aria-hidden="true" />
  </button>
  <Teleport to="body">
    <div
      v-if="open"
      ref="panel"
      role="group"
      aria-label="Methods"
      class="fixed z-[95] w-56 rounded-lg border bg-background p-1.5 text-[13px] shadow-[0_12px_32px_rgba(15,23,42,0.16)]"
      :style="{ top: `${pos.top}px`, left: `${pos.left}px`, transform: pos.up ? 'translateY(-100%)' : undefined }"
    >
      <label
        class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted"
        :class="hasPaths ? 'cursor-not-allowed opacity-50 hover:bg-transparent' : ''"
        :title="hasPaths ? 'Paths need methods: clear the paths to allow any method' : undefined"
      >
        <input type="checkbox" class="sr-only" :checked="any" :disabled="hasPaths" @change="setAny" />
        <span class="flex size-4 items-center justify-center rounded border" :class="any ? 'border-primary bg-primary text-white' : 'border-slate-300'">
          <Check v-if="any" class="size-3" />
        </span>
        <span class="flex-1">Any method</span>
        <span class="text-[11px] text-muted-foreground">All</span>
      </label>
      <div class="my-1 h-px bg-border" />
      <p v-if="wildcardHost" class="px-2 py-1 text-[11.5px] leading-snug text-muted-foreground">
        A wildcard host can't be limited to methods. List exact hosts to pick methods.
      </p>
      <label
        v-for="m in choices"
        :key="m"
        class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 font-mono text-[12.5px] hover:bg-muted"
        :class="wildcardHost ? 'cursor-not-allowed opacity-50 hover:bg-transparent' : ''"
      >
        <input type="checkbox" class="sr-only" :checked="modelValue.includes(m)" :disabled="wildcardHost" @change="flip(m)" />
        <span
          class="flex size-4 items-center justify-center rounded border"
          :class="modelValue.includes(m) ? 'border-primary bg-primary text-white' : 'border-slate-300'"
        >
          <Check v-if="modelValue.includes(m)" class="size-3" />
        </span>
        {{ m }}
      </label>
    </div>
  </Teleport>
</template>
