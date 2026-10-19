<script setup lang="ts">
import { Plus, Trash2 } from '@lucide/vue'
import type { SlotDetail } from '@/lib/kit'

interface Inject {
  _k: string
  domain: string
  header?: string
  format?: string
  scheme?: string
  username?: string
}

defineProps<{ slot: SlotDetail; canEdit: boolean }>()
const model = defineModel<Record<string, any>>({ required: true })

model.value.apiKey ??= { name: '', proxyManaged: true, inject: [] }
model.value.phase ??= ['runtime']

function injects(): Inject[] {
  return (model.value.apiKey?.inject ?? []) as Inject[]
}
function togglePhase(p: string, on: boolean) {
  const set = new Set<string>(model.value.phase)
  if (on) set.add(p)
  else set.delete(p)
  model.value.phase = ['install', 'runtime'].filter((x) => set.has(x))
}
function setField(i: Inject, field: keyof Inject, v: string) {
  if (v) (i as any)[field] = v
  else delete (i as any)[field]
}
function addInject() {
  ;(model.value.apiKey.inject ??= []).push({ _k: Math.random().toString(16).slice(2, 12), domain: '', header: 'Authorization', format: 'Bearer %s' })
}
function removeInject(i: Inject) {
  const l = injects()
  l.splice(l.indexOf(i), 1)
}
const input =
  'h-9 w-full rounded-md border bg-background px-2.5 text-[13px] focus:border-primary focus:outline-none disabled:bg-muted/50'
</script>

<template>
  <div class="flex flex-col gap-4 rounded-lg border bg-background p-4">
    <div class="grid gap-4 @2xl:grid-cols-3">
      <label class="flex flex-col gap-1.5 text-[13px] font-medium">
        Service
        <input v-model="model.service" :disabled="!canEdit" :class="input + ' font-mono'" placeholder="github" />
        <span class="text-xs font-normal text-muted-foreground">Name of the secret in the sbx secret store.</span>
      </label>
      <fieldset class="flex flex-col gap-1.5 text-[13px]">
        <legend class="mb-1.5 font-medium">Phases</legend>
        <div class="flex gap-4 pt-1.5">
          <label v-for="p in ['install', 'runtime']" :key="p" class="flex items-center gap-1.5">
            <input
              type="checkbox"
              :checked="model.phase?.includes(p)"
              :disabled="!canEdit"
              class="accent-primary"
              @change="togglePhase(p, ($event.target as HTMLInputElement).checked)"
            />
            {{ p }}
          </label>
        </div>
      </fieldset>
      <label class="flex flex-col gap-1.5 text-[13px] font-medium">
        Env var in the sandbox
        <input v-model="model.apiKey.name" :disabled="!canEdit" :class="input + ' font-mono'" placeholder="GH_TOKEN" />
        <span class="text-xs font-normal text-muted-foreground">Holds a placeholder; the proxy adds the real value.</span>
      </label>
    </div>
    <div class="grid gap-4 @2xl:grid-cols-[1fr_auto_auto]">
      <label class="flex flex-col gap-1.5 text-[13px] font-medium">
        Description
        <input v-model="model.description" :disabled="!canEdit" :class="input" placeholder="What this credential is for" />
      </label>
      <label class="flex items-center gap-2 self-end pb-2 text-[13px]">
        <input v-model="model.apiKey.proxyManaged" type="checkbox" :disabled="!canEdit" class="accent-primary" /> Proxy-managed
      </label>
      <label class="flex items-center gap-2 self-end pb-2 text-[13px]">
        <input v-model="model.optional" type="checkbox" :disabled="!canEdit" class="accent-primary" /> Optional
      </label>
    </div>

    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between">
        <h3 class="text-sm font-semibold">Inject into requests</h3>
        <span class="text-xs text-muted-foreground">Each domain must also be allowed by a network block.</span>
      </div>
      <div class="overflow-x-auto rounded-lg border">
        <table class="w-full min-w-[560px] text-[13px]">
          <thead>
            <tr class="bg-slate-50 text-left text-slate-500">
              <th class="px-3 py-2 font-medium">Domain</th>
              <th class="px-3 py-2 font-medium">Header</th>
              <th class="px-3 py-2 font-medium">Format</th>
              <th class="px-3 py-2 font-medium">Scheme</th>
              <th class="px-3 py-2 font-medium">Username</th>
              <th class="w-10"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in injects()" :key="i._k" class="border-t">
              <td v-for="f in (['domain', 'header', 'format', 'scheme', 'username'] as const)" :key="f" class="px-2 py-1.5">
                <input
                  :value="i[f]"
                  :disabled="!canEdit"
                  :aria-label="f"
                  class="w-full rounded border-transparent bg-transparent px-1 py-1 font-mono text-[12.5px] hover:border-slate-200 focus:border-primary focus:outline-none"
                  @input="setField(i, f, ($event.target as HTMLInputElement).value)"
                  @change="setField(i, f, ($event.target as HTMLInputElement).value.trim())"
                />
              </td>
              <td class="px-2">
                <button v-if="canEdit" type="button" aria-label="Remove inject rule" class="rounded p-1.5 text-slate-400 hover:bg-red-50 hover:text-red-600" @click="removeInject(i)">
                  <Trash2 class="size-3.5" />
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <button v-if="canEdit" type="button" class="inline-flex items-center gap-1 self-start rounded px-2 py-1 text-[13px] font-medium text-primary hover:bg-primary/5" @click="addInject">
        <Plus class="size-3.5" /> Add inject rule
      </button>
    </div>
  </div>
</template>
