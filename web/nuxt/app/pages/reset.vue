<script setup lang="ts">
/**
 * 密码重置（发起页）。
 * 调用 GET /api/reset_password?email=xxx，后端会发送一封含重置链接的邮件。
 */
const { t } = useI18n()
const { status, loadStatus, logo } = useUserStore()
const { success, showError, warning } = useAppToast()

const email = ref('')
const loading = ref(false)
const disableButton = ref(false)
const countdown = ref(30)
const turnstileToken = ref('')
const turnstileRef = ref<HTMLElement | null>(null)

let timer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  loadStatus()
  initTurnstile()
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})

function initTurnstile() {
  if (!status.value?.turnstile_check || !import.meta.client) return
  if ((window as any).turnstile) {
    renderWidget()
    return
  }
  const script = document.createElement('script')
  script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
  script.async = true
  script.onload = () => renderWidget()
  document.head.appendChild(script)
}

function renderWidget() {
  const siteKey = status.value?.turnstile_site_key
  if (!siteKey || !turnstileRef.value) return
  ;(window as any).turnstile.render(turnstileRef.value, {
    sitekey: siteKey,
    callback: (token: string) => {
      turnstileToken.value = token
    },
  })
}

async function handleSubmit() {
  if (!email.value) {
    warning(t('auth.reset.email'))
    return
  }
  if (status.value?.turnstile_check && !turnstileToken.value) {
    warning(t('messages.error.turnstile_wait'))
    return
  }
  loading.value = true
  try {
    await api.get('/api/reset_password', {
      query: { email: email.value, turnstile: turnstileToken.value },
    })
    success(t('messages.success.password_reset'))
    disableButton.value = true
    timer = setInterval(() => {
      if (countdown.value <= 1) {
        if (timer) clearInterval(timer)
        disableButton.value = false
        countdown.value = 30
      } else {
        countdown.value -= 1
      }
    }, 1000)
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="mx-auto mt-12 w-full max-w-[450px]">
    <PageCard>
      <div class="mb-6 flex flex-col items-center gap-3">
        <img :src="logo" alt="logo" class="h-12 object-contain">
        <h1 class="text-2xl font-semibold text-gray-800">{{ t('auth.reset.title') }}</h1>
      </div>

      <UAlert
        class="mb-4"
        color="info"
        variant="subtle"
        icon="i-heroicons-information-circle"
        :description="t('auth.reset.notice')"
      />

      <UForm :state="{ email }" class="space-y-4" @submit="handleSubmit">
        <UInput
          v-model="email"
          type="email"
          icon="i-heroicons-envelope"
          size="lg"
          :placeholder="t('auth.reset.email')"
          autocomplete="email"
          class="w-full"
        />

        <div v-if="status?.turnstile_check" ref="turnstileRef" class="flex justify-center" />

        <UButton
          type="submit"
          size="lg"
          block
          :loading="loading"
          :disabled="disableButton"
          color="primary"
        >
          {{ disableButton ? t('auth.register.get_code_retry', { countdown }) : t('auth.reset.button') }}
        </UButton>
      </UForm>

      <p class="mt-4 text-center text-sm text-gray-500">
        <NuxtLink to="/login" class="text-[var(--oa-brand)] hover:underline">
          {{ t('auth.register.login') }}
        </NuxtLink>
      </p>
    </PageCard>
  </div>
</template>
