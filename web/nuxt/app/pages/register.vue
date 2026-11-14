<script setup lang="ts">
/**
 * 注册页。
 *
 * 流程要点：
 * - 若后端开启邮箱验证（status.email_verification），必须先「获取验证码」并填写。
 * - 若开启 Turnstile 人机检查，表单里会多出验证码 token；这里用 Cloudflare 的脚本组件承载。
 * - 邀请码通过 ?aff= 传入，随 POST /api/user/register 一起提交。
 */
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { status, loadStatus, logo } = useUserStore()
const { success, showError, warning } = useAppToast()

const inputs = reactive({
  username: '',
  password: '',
  password2: '',
  email: '',
  verification_code: '',
})
const affCode = ref('')
const loading = ref(false)
const disableButton = ref(false)
const countdown = ref(30)
const turnstileToken = ref('')
const turnstileRef = ref<HTMLElement | null>(null)

let countdownTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  loadStatus()
  affCode.value = (route.query.aff as string) || ''
  renderTurnstile()
})

onBeforeUnmount(() => {
  if (countdownTimer) clearInterval(countdownTimer)
})

/** 动态加载 Cloudflare Turnstile，只在后端开启时才注入脚本 */
function renderTurnstile() {
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

async function sendVerificationCode() {
  if (!inputs.email) {
    warning(t('auth.register.email'))
    return
  }
  try {
    await api.get('/api/verification', {
      query: { email: inputs.email, turnstile: turnstileToken.value },
    })
    success(t('messages.success.verification_code'))
    disableButton.value = true
    countdownTimer = setInterval(() => {
      if (countdown.value <= 1) {
        if (countdownTimer) clearInterval(countdownTimer)
        disableButton.value = false
        countdown.value = 30
      } else {
        countdown.value -= 1
      }
    }, 1000)
  } catch (error) {
    showError(error)
  }
}

async function handleSubmit() {
  if (status.value?.turnstile_check && !turnstileToken.value) {
    warning(t('messages.error.turnstile_wait'))
    return
  }
  if (inputs.password.length < 8) {
    warning(t('messages.error.password_length'))
    return
  }
  if (inputs.password !== inputs.password2) {
    warning(t('messages.error.password_mismatch'))
    return
  }

  loading.value = true
  try {
    await api.post('/api/user/register', {
      username: inputs.username,
      password: inputs.password,
      email: inputs.email,
      verification_code: inputs.verification_code,
      aff_code: affCode.value,
      turnstile: turnstileToken.value,
    })
    success(t('messages.success.register'))
    await router.push('/login')
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
      <div class="mb-8 flex flex-col items-center gap-3">
        <img :src="logo" alt="logo" class="h-12 object-contain">
        <h1 class="text-2xl font-semibold text-gray-800">{{ t('auth.register.title') }}</h1>
      </div>

      <UForm :state="inputs" class="space-y-4" @submit="handleSubmit">
        <UInput
          v-model="inputs.username"
          icon="i-heroicons-user"
          size="lg"
          :placeholder="t('auth.register.username')"
          autocomplete="username"
          class="w-full"
        />

        <UInput
          v-model="inputs.password"
          type="password"
          icon="i-heroicons-lock-closed"
          size="lg"
          :placeholder="t('auth.register.password')"
          autocomplete="new-password"
          class="w-full"
        />

        <UInput
          v-model="inputs.password2"
          type="password"
          icon="i-heroicons-lock-closed"
          size="lg"
          :placeholder="t('auth.register.confirm_password')"
          autocomplete="new-password"
          class="w-full"
        />

        <UInput
          v-if="status?.email_verification"
          v-model="inputs.email"
          type="email"
          icon="i-heroicons-envelope"
          size="lg"
          :placeholder="t('auth.register.email')"
          autocomplete="email"
          class="w-full"
        />

        <div v-if="status?.email_verification" class="flex gap-2">
          <UInput
            v-model="inputs.verification_code"
            icon="i-heroicons-shield-check"
            size="lg"
            :placeholder="t('auth.register.verification_code')"
            class="flex-1"
          />
          <UButton
            color="neutral"
            variant="outline"
            size="lg"
            :disabled="disableButton"
            @click="sendVerificationCode"
          >
            {{
              disableButton
                ? t('auth.register.get_code_retry', { countdown })
                : t('auth.register.get_code')
            }}
          </UButton>
        </div>

        <div v-if="status?.turnstile_check" ref="turnstileRef" class="flex justify-center" />

        <UButton type="submit" size="lg" block :loading="loading" color="primary">
          {{ t('auth.register.button') }}
        </UButton>
      </UForm>

      <p class="mt-4 text-center text-sm text-gray-500">
        {{ t('auth.register.has_account') }}
        <NuxtLink to="/login" class="text-[var(--oa-brand)] hover:underline">
          {{ t('auth.register.login') }}
        </NuxtLink>
      </p>
    </PageCard>
  </div>
</template>
