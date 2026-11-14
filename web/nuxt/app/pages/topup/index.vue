<script setup lang="ts">
/**
 * 充值中心。
 *
 * 两条路径：
 * 1. 在线充值：读取 Option "TopUpLink"，跳转到管理员配置的充值链接；
 *    链接为空时给出「超级管理员未设置充值链接」的提示。
 * 2. 兑换码充值：POST /api/user/topup { key }，成功返回本次充值的额度。
 */
const { t } = useI18n()
const { user, quotaPerUnit, displayInCurrency, quotaConfig, restoreUser } = useUserStore()
const { success, showError, warning } = useAppToast()

const redeemCode = ref('')
const submitting = ref(false)
const topUpLink = ref('')

onMounted(async () => {
  // 充值链接存在 Option 表里，只有 root 能读 /api/option/，
  // 所以这里优先用缓存值，普通用户看不到按钮。
  try {
    const res = await api.get<any>('/api/option/')
    const list = Array.isArray(res.data) ? res.data : []
    const link = list.find((item: any) => item.key === 'TopUpLink')
    if (link) {
      topUpLink.value = link.value || ''
      localStorage.setItem('top_up_link', topUpLink.value)
    }
  } catch {
    topUpLink.value = localStorage.getItem('top_up_link') || ''
  }
})

async function pasteCode() {
  try {
    const text = await navigator.clipboard.readText()
    redeemCode.value = text.trim()
  } catch {
    warning(t('topup.redeem_code.paste_error'))
  }
}

async function redeem() {
  if (!redeemCode.value) {
    warning(t('topup.redeem_code.empty_code'))
    return
  }
  submitting.value = true
  try {
    const res = await api.post<number>('/api/user/topup', { key: redeemCode.value })
    const added = res.data ?? 0
    success(`${t('topup.redeem_code.success')} +${renderQuota(added, quotaConfig.value)}`)
    redeemCode.value = ''
    // 重新拉取用户信息，同步最新额度
    await restoreUser()
  } catch (error) {
    showError(error)
  } finally {
    submitting.value = false
  }
}

function goTopUp() {
  if (!topUpLink.value) {
    warning(t('topup.redeem_code.no_link'))
    return
  }
  window.open(topUpLink.value, '_blank', 'noopener')
}
</script>

<template>
  <div>
    <PageCard :title="t('topup.title')">
      <div class="grid gap-6 md:grid-cols-2">
        <!-- 在线充值 -->
        <UCard>
          <template #header>
            <div class="font-semibold">{{ t('topup.get_code.title') }}</div>
          </template>
          <div class="space-y-3">
            <div class="text-sm text-gray-500">{{ t('topup.get_code.current_quota') }}</div>
            <div class="text-2xl font-semibold">
              {{ renderQuota(user?.quota ?? 0, quotaConfig) }}
            </div>
            <p v-if="displayInCurrency" class="text-xs text-gray-400">
              1 美元 = {{ renderNumber(quotaPerUnit) }} 额度
            </p>
            <UButton color="primary" icon="i-heroicons-shopping-cart" block @click="goTopUp">
              {{ t('topup.get_code.button') }}
            </UButton>
            <p v-if="!topUpLink" class="text-xs text-gray-400">
              {{ t('topup.redeem_code.no_link') }}
            </p>
          </div>
        </UCard>

        <!-- 兑换码 -->
        <UCard>
          <template #header>
            <div class="font-semibold">{{ t('topup.redeem_code.title') }}</div>
          </template>
          <div class="space-y-3">
            <UInput
              v-model="redeemCode"
              :placeholder="t('topup.redeem_code.placeholder')"
              class="w-full"
              @keyup.enter="redeem"
            />
            <div class="flex gap-2">
              <UButton color="neutral" variant="outline" icon="i-heroicons-clipboard" @click="pasteCode">
                {{ t('topup.redeem_code.paste') }}
              </UButton>
              <UButton color="primary" :loading="submitting" class="flex-1" @click="redeem">
                {{ submitting ? t('topup.redeem_code.submitting') : t('topup.redeem_code.submit') }}
              </UButton>
            </div>
          </div>
        </UCard>
      </div>
    </PageCard>
  </div>
</template>
