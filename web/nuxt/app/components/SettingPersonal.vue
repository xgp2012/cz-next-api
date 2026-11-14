<script setup lang="ts">
/**
 * 个人设置。
 * - 更新个人信息：PUT /api/user/self
 * - 生成系统访问令牌：GET /api/user/token（注意是管理用途，不是 API Key）
 * - 复制邀请链接：GET /api/user/aff
 * - 账号绑定：微信 / GitHub / 邮箱 / 飞书
 * - 注销账户：DELETE /api/user/self
 */
import type { User } from '~/utils/types'
import { ROLE_ROOT } from '~/utils/types'

const { t } = useI18n()
const router = useRouter()
const { user, status, setUser, logout } = useUserStore()
const { success, showError, warning } = useAppToast()

// ---------------------------------------------------------------------------
// 通用设置
// ---------------------------------------------------------------------------
const profile = reactive({ username: '', display_name: '', password: '' })
const saving = ref(false)

watch(
  user,
  (value) => {
    if (value) {
      profile.username = value.username
      profile.display_name = value.display_name || ''
    }
  },
  { immediate: true },
)

const systemToken = ref('')

async function updateProfile() {
  if (profile.password && (profile.password.length < 8 || profile.password.length > 20)) {
    warning('密码长度需在 8 到 20 位之间')
    return
  }
  saving.value = true
  try {
    await api.put('/api/user/self', {
      username: profile.username,
      display_name: profile.display_name,
      password: profile.password,
    })
    const res = await api.get<User>('/api/user/self')
    if (res.data) setUser(res.data)
    profile.password = ''
    success(t('setting.messages.update_success', '个人信息更新成功'))
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

async function generateSystemToken() {
  try {
    const res = await api.get<string>('/api/user/token')
    systemToken.value = res.data || ''
    if (systemToken.value && user.value) {
      // 令牌保存在 access_token 字段，同步到本地以显示
      setUser({ ...user.value, access_token: systemToken.value })
    }
    if (systemToken.value) {
      await copyText(systemToken.value)
      success(t('setting.personal.general.system_token_copied'))
    }
  } catch (error) {
    showError(error)
  }
}

async function copyAffLink() {
  try {
    const res = await api.get<string>('/api/user/aff')
    const code = res.data || ''
    if (!code) return
    const link = `${window.location.origin}/register?aff=${code}`
    await copyText(link)
    success(t('setting.personal.general.aff_copied'))
  } catch (error) {
    showError(error)
  }
}

// ---------------------------------------------------------------------------
// 账号绑定
// ---------------------------------------------------------------------------
const bindEmail = reactive({ email: '', code: '', countdown: 0 })
const showEmailBind = ref(false)
let countdownTimer: ReturnType<typeof setInterval> | null = null

function startCountdown() {
  bindEmail.countdown = 60
  countdownTimer = setInterval(() => {
    bindEmail.countdown -= 1
    if (bindEmail.countdown <= 0 && countdownTimer) {
      clearInterval(countdownTimer)
      countdownTimer = null
    }
  }, 1000)
}

onUnmounted(() => {
  if (countdownTimer) clearInterval(countdownTimer)
})

async function sendEmailCode() {
  if (!bindEmail.email) {
    warning('请输入邮箱地址')
    return
  }
  try {
    await api.get('/api/verification', { query: { email: bindEmail.email } })
    success(t('setting.personal.binding.email_code_sent'))
    startCountdown()
  } catch (error) {
    showError(error)
  }
}

async function confirmEmailBind() {
  if (!bindEmail.email || !bindEmail.code) {
    warning('请输入邮箱地址与验证码')
    return
  }
  try {
    await api.get('/api/user/email/bind', {
      query: { email: bindEmail.email, code: bindEmail.code },
    })
    const res = await api.get<User>('/api/user/self')
    if (res.data) setUser(res.data)
    showEmailBind.value = false
    success(t('setting.personal.binding.email_success'))
  } catch (error) {
    showError(error)
  }
}

/** 邮箱绑定回调：后端把结果放进 URL query，这里读取后提示 */
onMounted(() => {
  const query = route_query()
  if (query.get('email_bind') === 'success') {
    success(t('setting.personal.binding.email_success'))
  }
})

function route_query() {
  return new URLSearchParams(window.location.search)
}

// ---------------------------------------------------------------------------
// 微信绑定
// ---------------------------------------------------------------------------
const showWeChatBind = ref(false)
const wechatCode = ref('')

function openWeChatBind() {
  if (!status.value?.wechat_qrcode) {
    warning('管理员未配置微信公众号二维码')
    return
  }
  showWeChatBind.value = true
}

async function bindWeChat() {
  if (!wechatCode.value) {
    warning('请输入验证码')
    return
  }
  try {
    await api.get('/api/oauth/wechat/bind', { query: { code: wechatCode.value } })
    const res = await api.get<User>('/api/user/self')
    if (res.data) setUser(res.data)
    showWeChatBind.value = false
    wechatCode.value = ''
    success(t('setting.personal.binding.wechat_success'))
  } catch (error) {
    showError(error)
  }
}

/** GitHub / 飞书 绑定：走后端 OAuth 重定向，带上 bind 标记 */
function bindOAuth(provider: 'github' | 'lark' | 'oidc') {
  const serverAddress = status.value?.server_address || window.location.origin
  const redirect = `${serverAddress}/oauth/${provider}?bind=true`
  window.location.href = `/api/oauth/${provider}?redirect=${encodeURIComponent(redirect)}`
}

// ---------------------------------------------------------------------------
// 注销账户
// ---------------------------------------------------------------------------
const showDeleteAccount = ref(false)
const deleteConfirmText = ref('')

async function deleteAccount() {
  if (deleteConfirmText.value !== user.value?.username) {
    warning(t('setting.personal.delete_account.confirm_failed'))
    return
  }
  try {
    await api.delete('/api/user/self')
    success(t('setting.personal.general.deleted'))
    await logout()
    await router.push('/login')
  } catch (error) {
    showError(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <!-- 通用设置 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.personal.general.title') }}</div>
      </template>
      <div class="grid gap-4 md:grid-cols-2">
        <UFormField :label="t('user.edit.username')">
          <UInput v-model="profile.username" class="w-full" />
        </UFormField>
        <UFormField :label="t('user.edit.display_name')">
          <UInput v-model="profile.display_name" class="w-full" />
        </UFormField>
        <UFormField :label="t('user.edit.password')">
          <UInput
            v-model="profile.password"
            type="password"
            autocomplete="new-password"
            :placeholder="t('user.edit.password_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField label="角色">
          <UInput :model-value="user?.role === 100 ? '超级管理员' : user?.role === 10 ? '管理员' : '普通用户'" readonly class="w-full" />
        </UFormField>
      </div>

      <div class="mt-4 flex flex-wrap gap-2">
        <UButton color="primary" :loading="saving" @click="updateProfile">
          {{ t('setting.personal.general.buttons.update_profile') }}
        </UButton>
        <UButton color="neutral" variant="outline" icon="i-heroicons-key" @click="generateSystemToken">
          {{ t('setting.personal.general.buttons.generate_token') }}
        </UButton>
        <UButton color="neutral" variant="outline" icon="i-heroicons-link" @click="copyAffLink">
          {{ t('setting.personal.general.buttons.copy_invite') }}
        </UButton>
      </div>

      <UAlert
        v-if="systemToken"
        class="mt-4"
        color="warning"
        variant="subtle"
        icon="i-heroicons-exclamation-triangle"
        :title="t('setting.personal.general.system_token_notice')"
      >
        <template #description>
          <code class="break-all">{{ systemToken }}</code>
        </template>
      </UAlert>
    </UCard>

    <!-- 账号绑定 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.personal.binding.title') }}</div>
      </template>

      <div class="space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-100 p-3">
          <div class="text-sm">
            <span class="text-gray-500">{{ t('user.edit.github_id') }}：</span>
            <span v-if="user?.github_id">{{ user.github_id }}</span>
            <span v-else class="text-gray-400">{{ t('user.table.unbound_email') }}</span>
          </div>
          <UButton
            v-if="!user?.github_id"
            size="sm"
            color="neutral"
            variant="soft"
            @click="bindOAuth('github')"
          >
            {{ t('setting.personal.binding.buttons.bind_github') }}
          </UButton>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-100 p-3">
          <div class="text-sm">
            <span class="text-gray-500">{{ t('user.edit.email') }}：</span>
            <span v-if="user?.email">{{ user.email }}</span>
            <span v-else class="text-gray-400">{{ t('user.table.unbound_email') }}</span>
          </div>
          <UButton
            v-if="!user?.email"
            size="sm"
            color="neutral"
            variant="soft"
            @click="showEmailBind = !showEmailBind"
          >
            {{ t('setting.personal.binding.buttons.bind_email') }}
          </UButton>
        </div>

        <div
          v-if="showEmailBind"
          class="flex flex-wrap items-end gap-2 rounded-lg bg-gray-50 p-3"
        >
          <UFormField :label="t('setting.personal.binding.email.email_placeholder')">
            <UInput v-model="bindEmail.email" class="w-64" />
          </UFormField>
          <UFormField :label="t('setting.personal.binding.email.code_placeholder')">
            <UInput v-model="bindEmail.code" class="w-32" />
          </UFormField>
          <UButton
            color="neutral"
            variant="outline"
            :disabled="bindEmail.countdown > 0"
            @click="sendEmailCode"
          >
            {{
              bindEmail.countdown > 0
                ? t('setting.personal.binding.email.get_code_retry', { countdown: bindEmail.countdown })
                : t('setting.personal.binding.email.get_code')
            }}
          </UButton>
          <UButton color="primary" @click="confirmEmailBind">
            {{ t('setting.personal.binding.email.bind') }}
          </UButton>
        </div>

        <div
          v-if="status?.wechat_login"
          class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-100 p-3"
        >
          <div class="text-sm">
            <span class="text-gray-500">{{ t('user.edit.wechat_id') }}：</span>
            <span v-if="user?.wechat_id">{{ user.wechat_id }}</span>
            <span v-else class="text-gray-400">{{ t('user.table.unbound_email') }}</span>
          </div>
          <UButton
            v-if="!user?.wechat_id"
            size="sm"
            color="neutral"
            variant="soft"
            @click="openWeChatBind"
          >
            {{ t('setting.personal.binding.buttons.bind_wechat') }}
          </UButton>
        </div>

        <div
          v-if="status?.lark_client_id"
          class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-100 p-3"
        >
          <div class="text-sm">
            <span class="text-gray-500">飞书账户：</span>
            <span v-if="user?.lark_id">{{ user.lark_id }}</span>
            <span v-else class="text-gray-400">{{ t('user.table.unbound_email') }}</span>
          </div>
          <UButton
            v-if="!user?.lark_id"
            size="sm"
            color="neutral"
            variant="soft"
            @click="bindOAuth('lark')"
          >
            {{ t('setting.personal.binding.buttons.bind_lark') }}
          </UButton>
        </div>
      </div>
    </UCard>

    <!-- 危险操作 -->
    <UCard>
      <template #header>
        <div class="font-semibold text-red-600">{{ t('setting.personal.delete_account.title') }}</div>
      </template>
      <UAlert
        color="error"
        variant="subtle"
        icon="i-heroicons-exclamation-triangle"
        :description="t('setting.personal.delete_account.warning')"
      />
      <UButton class="mt-3" color="error" variant="soft" @click="showDeleteAccount = true">
        {{ t('setting.personal.general.buttons.delete_account') }}
      </UButton>

      <UModal
        v-model:open="showDeleteAccount"
        :title="t('setting.personal.delete_account.title')"
      >
        <template #body>
          <p class="mb-3 text-sm text-gray-600">
            {{ t('setting.personal.delete_account.warning') }}
          </p>
          <UInput
            v-model="deleteConfirmText"
            :placeholder="
              t('setting.personal.delete_account.confirm_placeholder', { username: user?.username })
            "
            class="w-full"
          />
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" @click="showDeleteAccount = false">
              {{ t('setting.personal.delete_account.buttons.cancel') }}
            </UButton>
            <UButton color="error" @click="deleteAccount">
              {{ t('setting.personal.delete_account.buttons.confirm') }}
            </UButton>
          </div>
        </template>
      </UModal>
    </UCard>

    <!-- 微信绑定弹窗 -->
    <UModal v-model:open="showWeChatBind" :title="t('setting.personal.binding.wechat.title')">
      <template #body>
        <div class="space-y-3 text-center">
          <p class="text-sm text-gray-600">{{ t('setting.personal.binding.wechat.description') }}</p>
          <img
            v-if="status?.wechat_qrcode"
            :src="status.wechat_qrcode"
            alt="WeChat QR Code"
            class="mx-auto h-48 w-48"
          >
          <UFormField :label="t('setting.personal.binding.wechat.verification_code')">
            <UInput v-model="wechatCode" class="w-full" />
          </UFormField>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="showWeChatBind = false">取消</UButton>
          <UButton color="primary" @click="bindWeChat">
            {{ t('setting.personal.binding.wechat.bind') }}
          </UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
