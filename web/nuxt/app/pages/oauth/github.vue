<script setup lang="ts">
/**
 * GitHub OAuth 回调页。
 * GitHub 会跳回 /oauth/github?code=xxx&state=yyy，这里把 code 换成本地会话。
 */
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { status, loadStatus, login: setUser } = useUserStore()
const { success, showError } = useAppToast()

const prompt = ref(t('oauth.processing'))

onMounted(async () => {
  await loadStatus()
  const code = (route.query.code as string) || ''
  const state = (route.query.state as string) || ''
  if (!code) {
    prompt.value = t('oauth.redirect_login')
    await router.push('/login')
    return
  }
  await exchange(code, state, 0)
})

/**
 * 换票失败时按次数退避重试，最后回落到设置页（可能是在尝试绑定而非登录）。
 * 与原实现一致：count 为 0 时直接放弃。
 */
async function exchange(code: string, state: string, count: number) {
  try {
    const res = await api.get<import('~/utils/types').User>('/api/oauth/github', {
      query: { code, state },
      silent: true,
    })
    if (res.message === 'bind') {
      success(t('oauth.bind_success'))
      await router.push('/setting')
      return
    }
    if (res.data) setUser(res.data)
    success(t('oauth.login_success'))
    await router.push('/')
  } catch (error: any) {
    showError(error)
    if (count === 0) {
      prompt.value = t('oauth.redirect_login')
      await router.push('/setting')
      return
    }
    const next = count + 1
    prompt.value = t('oauth.retry', { count: next })
    await new Promise((resolve) => setTimeout(resolve, next * 2000))
    await exchange(code, state, next)
  }
}
</script>

<template>
  <div class="flex min-h-[300px] flex-col items-center justify-center gap-3">
    <UIcon name="i-heroicons-arrow-path" class="size-8 animate-spin text-[var(--oa-brand)]" />
    <p class="text-gray-600">{{ prompt }}</p>
  </div>
</template>
