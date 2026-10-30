<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { KeyRound, Mail } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { cancelled, emailCode, passkeysSupported, signInWithCode, signInWithPasskey } from '@/lib/passkey'

// Sign-in without passwords (the Cloudflare edition): an emailed code or
// link, or a passkey. Success reloads the app, which then finds the session.
const props = defineProps<{ next: string; passkey: boolean }>()

const email = ref('')
const code = ref('')
const sentTo = ref('')
const busy = ref(false)
const error = ref('')
const codeInput = ref<HTMLInputElement | null>(null)
const showPasskey = computed(() => props.passkey && passkeysSupported())

const inputClass = 'h-10 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50'

async function run(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await fn()
  } catch (e) {
    if (!cancelled(e)) error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}

function send() {
  return run(async () => {
    await emailCode(email.value, props.next)
    sentTo.value = email.value.trim().toLowerCase()
    code.value = ''
    await nextTick()
    codeInput.value?.focus()
  })
}

function verify() {
  return run(async () => window.location.assign(await signInWithCode(sentTo.value, code.value)))
}

function passkey() {
  return run(async () => window.location.assign(await signInWithPasskey(props.next)))
}
</script>

<template>
  <div class="space-y-4 rounded-xl border bg-background p-6 shadow-xs">
    <form v-if="!sentTo" class="space-y-4" @submit.prevent="send">
      <div class="space-y-1.5">
        <label for="email" class="text-sm font-medium">Email</label>
        <input
          id="email"
          v-model="email"
          type="email"
          autocomplete="username webauthn"
          required
          autofocus
          :class="inputClass"
          :aria-invalid="!!error"
          aria-describedby="login-error"
        />
      </div>
      <Button type="submit" class="w-full" :disabled="busy"><Mail /> Email me a sign-in code</Button>
    </form>

    <form v-else class="space-y-4" @submit.prevent="verify">
      <p class="text-sm text-slate-600">
        We sent a code and a sign-in link to <strong class="text-foreground">{{ sentTo }}</strong>. Enter the code, or open the link on this device.
      </p>
      <div class="space-y-1.5">
        <label for="code" class="text-sm font-medium">Code</label>
        <input
          id="code"
          ref="codeInput"
          v-model="code"
          inputmode="numeric"
          autocomplete="one-time-code"
          pattern="[0-9 ]{6,7}"
          maxlength="7"
          required
          :class="[inputClass, 'font-mono tracking-[0.3em]']"
          aria-describedby="login-error"
        />
      </div>
      <Button type="submit" class="w-full" :disabled="busy">Sign in</Button>
      <div class="flex justify-between text-xs">
        <button type="button" class="text-primary hover:underline" @click="sentTo = ''">Use a different email</button>
        <button type="button" class="text-primary hover:underline" :disabled="busy" @click="(email = sentTo), send()">Send a new code</button>
      </div>
    </form>

    <p v-if="error" id="login-error" role="alert" class="text-sm text-destructive">{{ error }}</p>

    <template v-if="showPasskey && !sentTo">
      <div class="flex items-center gap-3 text-xs text-muted-foreground"><span class="h-px flex-1 bg-border" />or<span class="h-px flex-1 bg-border" /></div>
      <Button type="button" variant="outline" class="w-full" :disabled="busy" @click="passkey"><KeyRound /> Sign in with a passkey</Button>
    </template>
  </div>
</template>
