<script setup lang="ts">
/**
 * 飞书 Lark OAuth 回调页。
 * 与 GitHub 的区别仅在于交换接口：GET /api/oauth/lark。
 */
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { loadStatus, login: setUser } = useUserStore()
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

async function exchange(code: string, state: string, count: number) {
  try {
    const res = await api.get<import('~/utils/types').User>('/api/oauth/lark', {
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
