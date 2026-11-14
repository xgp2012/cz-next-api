<script setup lang="ts">
/**
 * 密码重置确认页。
 *
 * 后端发送的邮件链接形如：
 *   {ServerAddress}/user/reset?email=xxx&token=yyy
 * 注意后端使用的是 /user/reset（不带 /api 前缀），
 * 而旧前端的 React 路由是 /user/reset 由 PasswordResetConfirm 承载。
 * 这里保持同一路径，直接放在 pages/user/reset.vue。
 *
 * 提交 POST /api/user/reset { email, token } 后，后端会生成新密码并在 data 里返回。
 */
const { t } = useI18n()
const route = useRoute()
const { logo } = useUserStore()
const { success, showError, warning } = useAppToast()

const email = ref('')
const token = ref('')
const newPassword = ref('')
const loading = ref(false)
const disableButton = ref(false)

onMounted(() => {
  email.value = (route.query.email as string) || ''
  token.value = (route.query.token as string) || ''
  if (!email.value || !token.value) {
    warning(t('auth.reset.confirm.notice'))
  }
})

async function handleSubmit() {
  if (!email.value || !token.value) return
  loading.value = true
  try {
    const res = await api.post<string>('/api/user/reset', { email: email.value, token: token.value })
    newPassword.value = res.data || ''
    if (newPassword.value) {
      await copyText(newPassword.value)
      success(t('messages.notice.password_copied', { password: newPassword.value }))
    }
    disableButton.value = true
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
        <h1 class="text-2xl font-semibold text-gray-800">{{ t('auth.reset.confirm.title') }}</h1>
      </div>

      <UForm :state="{ newPassword }" class="space-y-4" @submit="handleSubmit">
        <UInput
          v-model="newPassword"
          :placeholder="t('auth.reset.confirm.new_password')"
          size="lg"
          readonly
          icon="i-heroicons-key"
          class="w-full"
          @focus="newPassword && copyText(newPassword)"
        />

        <UButton
          type="submit"
          size="lg"
          block
          :loading="loading"
          :disabled="disableButton"
          color="primary"
        >
          {{
            disableButton
              ? t('auth.reset.confirm.button_disabled')
              : t('auth.reset.confirm.button')
          }}
        </UButton>
      </UForm>

      <p v-if="newPassword" class="mt-4 text-sm text-gray-500">
        {{ t('auth.reset.confirm.notice') }}
      </p>

      <p class="mt-4 text-center text-sm text-gray-500">
        <NuxtLink to="/login" class="text-[var(--oa-brand)] hover:underline">
          {{ t('auth.register.login') }}
        </NuxtLink>
      </p>
    </PageCard>
  </div>
</template>
