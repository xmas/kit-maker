<script setup lang="ts">
import { computed, ref } from 'vue'
import { Link, router, usePage } from '@inertiajs/vue3'
import { Plus, Trash2 } from '@lucide/vue'
import { slotHref, type CheckResult } from '@/lib/blueprint'

const props = defineProps<{ checks: CheckResult[]; blueprintId: string; editable?: boolean }>()

const page = usePage()
const error = computed(() => (page.props.errors as Record<string, string> | undefined)?.check)
const adding = ref(false)
const form = ref({ label: '', method: 'GET', url: '', expect: 'reach' })
function add() {
  router.post(`/blueprints/${props.blueprintId}/checks`, form.value, {
    preserveScroll: true,
    onSuccess: () => {
      adding.value = false
      form.value = { label: '', method: 'GET', url: '', expect: 'reach' }
    },
  })
}
function remove(id: string) {
  router.delete(`/blueprints/${props.blueprintId}/checks/${id}`, { preserveScroll: true })
}
const field = 'h-8 rounded-md border bg-background px-2 text-[13px]'
const summary = computed(() => {
  const pass = props.checks.filter((c) => c.status === 'pass').length
  const fail = props.checks.filter((c) => c.status === 'fail').length
  return { pass, fail, pending: props.checks.length - pass - fail }
})
const icon: Record<string, string> = { pass: '✓', fail: '✕', pending: '…' }
const iconClass: Record<string, string> = {
  pass: 'bg-green-50 text-green-700',
  fail: 'bg-red-50 text-red-700',
  pending: 'bg-slate-100 text-slate-500',
}
</script>

<template>
  <section aria-label="Goal checks" class="flex flex-col gap-1 rounded-xl border bg-background px-4 py-3.5">
    <div class="mb-1.5 flex items-baseline justify-between">
      <h2 class="text-sm font-semibold">Goal checks</h2>
      <span class="text-xs text-muted-foreground">
        {{ summary.pass }} pass · {{ summary.fail }} fail<template v-if="summary.pending"> · {{ summary.pending }} pending</template>
      </span>
    </div>
    <div v-for="c in checks" :key="c.id" class="flex items-start gap-2.5 border-t border-slate-100 py-2">
      <span class="flex size-5 shrink-0 items-center justify-center rounded-full text-xs font-bold" :class="iconClass[c.status]">{{ icon[c.status] }}</span>
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="text-[13px] leading-snug">{{ c.label }}</span>
        <span class="truncate font-mono text-[11.5px] text-muted-foreground" :title="c.url">
          {{ c.method }} {{ c.url }} → expect {{ c.expect }}
        </span>
        <span class="text-xs leading-snug text-muted-foreground">
          {{ c.why }}
          <Link v-if="c.source" :href="slotHref(blueprintId, c.source)" class="ml-1 text-primary hover:underline" preserve-scroll>open</Link>
        </span>
      </div>
      <button v-if="editable" type="button" aria-label="Remove check" class="rounded p-1 text-slate-400 hover:bg-red-50 hover:text-red-600" @click="remove(c.id)">
        <Trash2 class="size-3.5" />
      </button>
    </div>
    <p v-if="!checks.length" class="py-2 text-sm text-muted-foreground">
      No checks yet. A check is a request the sandbox should or shouldn't be able to make.
    </p>
    <template v-if="editable">
      <button
        v-if="!adding"
        type="button"
        class="mt-1 inline-flex items-center gap-1 self-start rounded px-1.5 py-1 text-[13px] font-medium text-primary hover:bg-primary/5"
        @click="adding = true"
      >
        <Plus class="size-3.5" /> Add a check
      </button>
      <form v-else class="mt-1 flex flex-col gap-2 rounded-lg border bg-slate-50 p-3" @submit.prevent="add">
        <input v-model="form.label" :class="field" placeholder="What it proves, e.g. Read the acme/shop repo" aria-label="Check label" />
        <div class="flex gap-2">
          <select v-model="form.method" :class="field + ' w-24'" aria-label="Method">
            <option v-for="m in ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE']" :key="m">{{ m }}</option>
          </select>
          <input v-model="form.url" :class="field + ' min-w-0 flex-1 font-mono'" placeholder="https://api.github.com/repos/acme/shop" aria-label="URL" required />
        </div>
        <div class="flex items-center gap-3 text-[13px]">
          <span class="text-slate-600">Expect</span>
          <label class="flex items-center gap-1"><input v-model="form.expect" type="radio" value="reach" class="accent-primary" /> reach</label>
          <label class="flex items-center gap-1"><input v-model="form.expect" type="radio" value="blocked" class="accent-primary" /> blocked</label>
          <span class="flex-1" />
          <button type="button" class="rounded-md px-2.5 py-1 text-slate-600 hover:bg-muted" @click="adding = false">Cancel</button>
          <button type="submit" class="rounded-md bg-primary px-2.5 py-1 font-medium text-primary-foreground">Add</button>
        </div>
        <p v-if="error" role="alert" class="text-xs text-red-700">{{ error }}</p>
      </form>
    </template>
  </section>
</template>
