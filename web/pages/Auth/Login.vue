<script setup lang="ts">
import { Head, useForm, usePage } from '@inertiajs/vue3'
import { Boxes, LogIn } from '@lucide/vue'
import { Button } from '@/components/ui/button'

const props = defineProps<{ next: string; providers: { key: string; label: string }[] }>()

// The Cloudflare edition (auth.mode set) has no admin CLI.
const cloudflare = !!usePage<{ auth?: { mode?: string } }>().props.auth?.mode

const form = useForm({ email: '', password: '', next: props.next })

function submit() {
  form.post('/login', { onFinish: () => form.reset('password') })
}
</script>

<template>
  <Head title="Sign in" />
  <div class="flex min-h-screen items-center justify-center bg-[#F6F7F9] px-4">
    <div class="w-full max-w-sm space-y-6">
      <div class="flex flex-col items-center gap-3 text-center">
        <span class="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
          <Boxes class="size-6" />
        </span>
        <div>
          <h1 class="text-xl font-semibold tracking-tight">Sign in to Kit Maker</h1>
          <p class="text-sm text-muted-foreground">Build blueprints for Docker Sandboxes kits from reusable blocks.</p>
        </div>
      </div>

      <form class="space-y-4 rounded-xl border bg-background p-6 shadow-xs" @submit.prevent="submit">
        <div class="space-y-1.5">
          <label for="email" class="text-sm font-medium">Email</label>
          <input
            id="email"
            v-model="form.email"
            type="email"
            autocomplete="username"
            required
            autofocus
            class="h-10 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
            :aria-invalid="!!form.errors.email"
            aria-describedby="login-error"
          />
        </div>
        <div class="space-y-1.5">
          <label for="password" class="text-sm font-medium">Password</label>
          <input
            id="password"
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            required
            class="h-10 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        </div>
        <p v-if="form.errors.email" id="login-error" role="alert" class="text-sm text-destructive">
          {{ form.errors.email }}
        </p>
        <Button type="submit" class="w-full" :disabled="form.processing">
          <LogIn /> Sign in
        </Button>
      </form>

      <p class="text-center text-xs text-muted-foreground">
        <template v-if="cloudflare">Ask whoever runs this deployment to add your email to <code class="rounded bg-muted px-1">ALLOWED_EMAILS</code>.</template>
        <template v-else>Locked out? An admin can run <code class="rounded bg-muted px-1">kitmaker user set-password</code>.</template>
      </p>
    </div>
  </div>
</template>
