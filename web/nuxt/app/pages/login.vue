<script setup lang="ts">
/**
 * 登录页。
 *
 * 支持的登录方式：
 * - 用户名/密码（POST /api/user/login）
 * - GitHub OAuth（跳转 github.com，回跳 /oauth/github）
 * - 飞书 Lark OAuth（回跳 /oauth/lark）
 * - 微信扫码（打开 Modal 输入验证码，GET /api/oauth/wechat?code=）
 *
 * 默认口令 root/123456 登录后会额外提示立即改密，并跳到改密页（与旧行为一致）。
 */
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { status, loadStatus, systemName, logo, login: setUser } = useUserStore()
const { success, showError, warning } = useAppToast()

const inputs = reactive({
  username: '',
  password: '',
  wechat_verification_code: '',
})
const loading = ref(false)
const showWeChatModal = ref(false)

onMounted(async () => {
  loadStatus()
  if (route.query.expired) {
    showError(t('messages.error.login_expired'))
  }
})

async function handleSubmit() {
  if (!inputs.username || !inputs.password) {
    warning(t('auth.login.username'))
    return
  }
  loading.value = true
  try {
    const res = await api.post<import('~/utils/types').User>('/api/user/login', {
      username: inputs.username,
      password: inputs.password,
    })
    if (res.data) setUser(res.data)
    success(t('messages.success.login'))

    const isDefaultRoot = inputs.username === 'root' && inputs.password === '123456'
    if (isDefaultRoot) {
      warning(t('messages.error.root_password'))
      await router.push(`/user/edit/${res.data?.id ?? 1}`)
    } else {
      await router.push('/token')
    }
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

async function handleWeChatLogin() {
  const code = inputs.wechat_verification_code
  if (!code) return
  try {
    const res = await api.get<import('~/utils/types').User>('/api/oauth/wechat', { query: { code } })
    if (res.data) setUser(res.data)
    success(t('messages.success.login'))
    showWeChatModal.value = false
    await router.push('/')
  } catch (error) {
    showError(error)
  }
}

/** OAuth 跳转需要先向服务端换取 state 防 CSRF */
async function startOAuth(provider: 'github' | 'lark') {
  try {
    const res = await api.get<string>('/api/oauth/state')
    const state = res.data || ''
    if (!state) return
    if (provider === 'github') {
      const clientId = status.value?.github_client_id || ''
      window.open(
        `https://github.com/login/oauth/authorize?client_id=${clientId}&state=${state}&scope=user:email`,
        '_blank',
      )
    } else {
      const clientId = status.value?.lark_client_id || ''
      const redirectUri = `${window.location.origin}/oauth/lark`
      window.open(
        `https://open.feishu.cn/open-apis/authen/v1/index?redirect_uri=${redirectUri}&app_id=${clientId}&state=${state}`,
        '_blank',
      )
    }
  } catch (error) {
    showError(error)
  }
}
</script>

<template>
  <div class="mx-auto mt-12 w-full max-w-[450px]">
    <PageCard>
      <div class="mb-8 flex flex-col items-center gap-3">
        <img :src="logo" alt="logo" class="h-12 object-contain">
        <h1 class="text-2xl font-semibold text-gray-800">{{ t('auth.login.title') }}</h1>
      </div>

      <UForm :state="inputs" class="space-y-4" @submit="handleSubmit">
        <UFormField name="username">
          <UInput
            v-model="inputs.username"
            icon="i-heroicons-user"
            size="lg"
            :placeholder="t('auth.login.username')"
            autocomplete="username"
            class="w-full"
          />
        </UFormField>

        <UFormField name="password">
          <UInput
            v-model="inputs.password"
            type="password"
            icon="i-heroicons-lock-closed"
            size="lg"
            :placeholder="t('auth.login.password')"
            autocomplete="current-password"
            class="w-full"
          />
        </UFormField>

        <UButton type="submit" size="lg" block :loading="loading" color="primary">
          {{ t('auth.login.button') }}
        </UButton>
      </UForm>

      <div class="mt-4 flex justify-between text-sm text-gray-500">
        <span>
          {{ t('auth.login.forgot_password') }}
          <NuxtLink to="/reset" class="text-[var(--oa-brand)] hover:underline">
            {{ t('auth.login.reset_password') }}
          </NuxtLink>
        </span>
        <span>
          {{ t('auth.login.no_account') }}
          <NuxtLink to="/register" class="text-[var(--oa-brand)] hover:underline">
            {{ t('auth.login.register') }}
          </NuxtLink>
        </span>
      </div>

      <USeparator class="my-5" :label="t('auth.login.other_methods')" />

      <div class="flex flex-wrap justify-center gap-2">
        <UButton
          v-if="status?.github_oauth"
          icon="i-simple-icons-github"
          color="neutral"
          variant="outline"
          @click="startOAuth('github')"
        >
          GitHub
        </UButton>
        <UButton
          v-if="status?.lark_client_id"
          color="neutral"
          variant="outline"
          @click="startOAuth('lark')"
        >
          <template #leading>
            <img src="/lark.svg" alt="lark" class="size-4">
          </template>
          飞书
        </UButton>
        <UButton
          v-if="status?.wechat_login"
          icon="i-simple-icons-wechat"
          color="neutral"
          variant="outline"
          @click="showWeChatModal = true"
        >
          微信
        </UButton>
      </div>
    </PageCard>

    <UModal v-model:open="showWeChatModal" :title="'微信登录'">
      <template #body>
        <p class="mb-3 text-sm text-gray-600">{{ t('auth.login.wechat.scan_tip') }}</p>
        <img
          v-if="status?.wechat_qrcode"
          :src="status.wechat_qrcode"
          alt="wechat qrcode"
          class="mx-auto mb-4 h-48 w-48 object-contain"
        >
        <UInput
          v-model="inputs.wechat_verification_code"
          :placeholder="t('auth.login.wechat.code_placeholder')"
          class="w-full"
        />
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="showWeChatModal = false">
            {{ t('common.cancel') }}
          </UButton>
          <UButton color="primary" @click="handleWeChatLogin">
            {{ t('common.confirm') }}
          </UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
