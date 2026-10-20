<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Head, Link, useForm } from '@inertiajs/vue3'
import { ArrowLeft, Sparkles } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'

const props = defineProps<{ teams: { id: string; name: string }[] }>()

const form = useForm({
  teamId: props.teams[0]?.id ?? '',
  displayName: '',
  slug: '',
  kind: 'workload',
  description: '',
  goal: '',
})

// The slug follows the name until someone edits it.
const slugTouched = ref(false)
const suggested = computed(() =>
  form.displayName
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64),
)
watch(suggested, (v) => {
  if (!slugTouched.value) form.slug = v
})

const formError = computed(() => (form.errors as Record<string, string>).form || form.errors.teamId)

function submit() {
  form.post('/blueprints')
}

const kinds = [
  { key: 'workload', label: 'Workload', help: 'Runs an agent: Claude Code, Codex, your own image.' },
  { key: 'mixin', label: 'Mixin', help: 'Adds network rules, credentials or setup to another blueprint.' },
]
const field =
  'h-10 w-full rounded-lg border bg-background px-3 text-[14px] focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/15'
</script>

<template>
  <Head title="New blueprint" />
  <AppShell>
    <main class="mx-auto w-full max-w-2xl px-4 py-8">
      <Link href="/blueprints" class="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft class="size-3.5" /> Blueprints
      </Link>
      <h1 class="text-2xl font-semibold tracking-tight">New blueprint</h1>
      <p class="mt-1 text-sm text-muted-foreground">Start with the goal. The blueprint opens as an outline you fill in, and the checks tell you when it's done.</p>

      <p v-if="!teams.length" role="alert" class="mt-6 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
        You're not on a team that can create blueprints. Ask a team admin for the contributor role.
      </p>

      <form v-else class="mt-6 flex flex-col gap-5 rounded-xl border bg-background p-6" @submit.prevent="submit">
        <label class="flex flex-col gap-1.5 text-sm font-medium">
          What should the agent be able to do?
          <textarea
            v-model="form.goal"
            rows="4"
            autofocus
            placeholder="A Claude Code sandbox for our Laravel monorepo. It runs the tests, pushes branches to GitHub and installs packages from our private Packagist. It must never reach production or post to Slack."
            class="rounded-lg border bg-background p-3 text-[14px] leading-relaxed font-normal focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/15"
          />
          <span class="flex items-center gap-1.5 text-xs font-normal text-muted-foreground">
            <Sparkles class="size-3.5 text-ai" /> Soon the AI planner turns this into an outline and checks. For now you add slots yourself.
          </span>
        </label>

        <div class="grid gap-5 sm:grid-cols-2">
          <label class="flex flex-col gap-1.5 text-sm font-medium">
            Name
            <input v-model="form.displayName" required :class="field" placeholder="Acme Shop" />
          </label>
          <label class="flex flex-col gap-1.5 text-sm font-medium">
            Slug
            <input v-model="form.slug" :class="field + ' font-mono'" placeholder="acme-shop" @input="slugTouched = true" />
            <span class="text-xs font-normal text-muted-foreground">Used in the YAML and image name.</span>
          </label>
        </div>

        <fieldset class="flex flex-col gap-2">
          <legend class="mb-1.5 text-sm font-medium">Kind</legend>
          <div class="grid gap-2 sm:grid-cols-2">
            <label
              v-for="k in kinds"
              :key="k.key"
              class="flex cursor-pointer flex-col gap-0.5 rounded-lg border p-3 has-checked:border-primary has-checked:bg-primary/5"
            >
              <span class="flex items-center gap-2 text-sm font-medium">
                <input v-model="form.kind" type="radio" :value="k.key" class="accent-primary" /> {{ k.label }}
              </span>
              <span class="pl-5 text-xs text-muted-foreground">{{ k.help }}</span>
            </label>
          </div>
        </fieldset>

        <label v-if="teams.length > 1" class="flex flex-col gap-1.5 text-sm font-medium">
          Owner team
          <select v-model="form.teamId" :class="field">
            <option v-for="t in teams" :key="t.id" :value="t.id">{{ t.name }}</option>
          </select>
        </label>

        <label class="flex flex-col gap-1.5 text-sm font-medium">
          Short description <span class="font-normal text-muted-foreground">(optional)</span>
          <input v-model="form.description" :class="field" placeholder="Claude Code for the acme/shop monorepo" />
        </label>

        <p v-if="formError" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
          {{ formError }}
        </p>

        <div class="flex items-center justify-end gap-2">
          <Link href="/blueprints" class="rounded-lg px-4 py-2 text-sm text-slate-600 hover:bg-muted">Cancel</Link>
          <button type="submit" :disabled="form.processing" class="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60">
            Create blueprint
          </button>
        </div>
      </form>
    </main>
  </AppShell>
</template>
