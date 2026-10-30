<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { KeyRound, Mail, UserRound } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { cancelled, emailCode, guestAvailable, passkeysSupported, signInAsGuest, signInWithCode, signInWithPasskey } from '@/lib/passkey'

// Sign-in without passwords (the Cloudflare edition): an emailed code or
// link, or a passkey; or, where GUEST_PASSPHRASE is set, a shared guest
// passphrase. Success reloads the app, which then finds the session.
const props = defineProps<{ next: string; passkey: boolean }>()

const email = ref('')
const code = ref('')
const sentTo = ref('')
const busy = ref(false)
const error = ref('')
const codeInput = ref<HTMLInputElement | null>(null)
const showPasskey = computed(() => props.passkey && passkeysSupported())
const guestOn = ref(false)
const asGuest = ref(false)
const passphrase = ref('')
const passphraseInput = ref<HTMLInputElement | null>(null)

onMounted(async () => {
  guestOn.value = await guestAvailable().catch(() => false)
})

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

async function openGuest() {
  asGuest.value = true
  error.value = ''
  await nextTick()
  passphraseInput.value?.focus()
}

function guest() {
  return run(async () => window.location.assign(await signInAsGuest(passphrase.value, props.next)))
}

function passkey() {
  return run(async () => window.location.assign(await signInWithPasskey(props.next)))
}
</script>

<template>
  <div class="space-y-4 rounded-xl border bg-background p-6 shadow-xs">
    <form v-if="asGuest" class="space-y-4" @submit.prevent="guest">
      <div class="space-y-1.5">
        <label for="passphrase" class="text-sm font-medium">Guest passphrase</label>
        <input
          id="passphrase"
          ref="passphraseInput"
          v-model="passphrase"
          type="password"
          autocomplete="off"
          required
          :class="inputClass"
          aria-describedby="login-error"
        />
      </div>
      <Button type="submit" class="w-full" :disabled="busy"><UserRound /> Sign in as a guest</Button>
      <button type="button" class="text-xs text-primary hover:underline" @click="asGuest = false">Sign in with your email instead</button>
    </form>

    <form v-else-if="!sentTo" class="space-y-4" @submit.prevent="send">
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

    <template v-if="!sentTo && !asGuest && (showPasskey || guestOn)">
      <div class="flex items-center gap-3 text-xs text-muted-foreground"><span class="h-px flex-1 bg-border" />or<span class="h-px flex-1 bg-border" /></div>
      <Button v-if="showPasskey" type="button" variant="outline" class="w-full" :disabled="busy" @click="passkey"><KeyRound /> Sign in with a passkey</Button>
      <Button v-if="guestOn" type="button" variant="ghost" class="w-full" :disabled="busy" @click="openGuest"><UserRound /> Sign in as a guest</Button>
    </template>
  </div>
</template>
