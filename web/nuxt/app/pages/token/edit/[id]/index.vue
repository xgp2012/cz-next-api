<script setup lang="ts">
/**
 * 新建 / 编辑令牌。
 *
 * 字段要点：
 * - expired_time: -1 表示永不过期（后端约定），UI 用「永不过期」开关表达
 * - unlimited_quota: 剩余额度无限制
 * - models: 允许访问的模型白名单，留空表示不限制
 * - subnet: IP 白名单，逗号分隔
 */
import type { Token } from '~/utils/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { quotaConfig } = useUserStore()
const { success, showError, warning } = useAppToast()

const tokenId = computed(() => (route.params.id ? Number(route.params.id) : 0))
const isEdit = computed(() => tokenId.value > 0)
const loading = ref(false)

const inputs = reactive({
  id: 0,
  name: '',
  remain_quota: 500000,
  unlimited_quota: false,
  expired_time: -1 as number,
  models: [] as string[],
  subnet: '',
  status: 1,
})

const neverExpire = ref(true)
const availableModels = ref<string[]>([])

const modelOptions = computed(() =>
  availableModels.value.map((model) => ({ label: model, value: model })),
)

onMounted(async () => {
  await fetchAvailableModels()
  if (isEdit.value) await loadToken()
})

/** 当前账号可用的模型（受分组与渠道影响） */
async function fetchAvailableModels() {
  try {
    const res = await api.get<string[]>('/api/user/available_models')
    availableModels.value = res.data || []
  } catch {
    // 普通用户也可能拿不到，忽略
  }
}

async function loadToken() {
  loading.value = true
  try {
    const res = await api.get<Token>(`/api/token/${tokenId.value}`)
    const data = res.data
    if (!data) return
    Object.assign(inputs, {
      id: data.id,
      name: data.name,
      remain_quota: data.remain_quota,
      unlimited_quota: data.unlimited_quota,
      expired_time: data.expired_time,
      subnet: data.subnet || '',
      status: data.status,
    })
    // 后端把 models 存成逗号分隔字符串
    if (typeof data.models === 'string' && data.models) {
      inputs.models = data.models.split(',')
    } else if (Array.isArray(data.models)) {
      inputs.models = data.models as string[]
    }
    neverExpire.value = data.expired_time === -1
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (!inputs.name) {
    warning(t('token.edit.name_placeholder'))
    return
  }
  loading.value = true
  try {
    const payload: Record<string, any> = {
      name: inputs.name,
      remain_quota: inputs.unlimited_quota ? 0 : inputs.remain_quota,
      unlimited_quota: inputs.unlimited_quota,
      expired_time: neverExpire.value ? -1 : inputs.expired_time,
      models: inputs.models.join(','),
      subnet: inputs.subnet,
    }

    if (isEdit.value) {
      payload.id = inputs.id
      await api.put('/api/token/', payload)
      success(t('token.messages.update_success'))
      await router.push('/token')
    } else {
      const res = await api.post<Token>('/api/token/', payload)
      success(t('token.messages.create_success'))
      // 新建后直接把 key 复制到剪贴板，省掉一次手动操作
      if (res.data?.key) {
        await copyText(`sk-${res.data.key}`)
        success(t('token.messages.copy_success'))
      }
      await router.push('/token')
    }
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

function cancel() {
  router.push('/token')
}

/** 前端 datetime-local 用的是秒级时间戳，后面统一转成 Unix 秒 */
const expireDate = computed({
  get: () => (inputs.expired_time > 0 ? timestamp2datetimeLocal(inputs.expired_time) : ''),
  set: (value: string) => {
    inputs.expired_time = value ? string2timestamp(value.replace('T', ' ')) : -1
  },
})
</script>

<template>
  <div>
    <PageCard :title="isEdit ? t('token.edit.title_edit') : t('token.edit.title_create')">
      <UForm :state="inputs" :loading="loading" class="max-w-2xl space-y-4" @submit="submit">
        <UFormField :label="t('token.edit.name')" name="name" required>
          <UInput v-model="inputs.name" :placeholder="t('token.edit.name_placeholder')" class="w-full" />
        </UFormField>

        <UFormField :label="t('token.edit.expired_time')" name="expired_time">
          <div class="flex flex-wrap items-center gap-2">
            <UCheckbox v-model="neverExpire" :label="t('token.edit.never_expires')" />
            <UInput
              v-if="!neverExpire"
              v-model="expireDate"
              type="datetime-local"
              class="w-60"
            />
          </div>
        </UFormField>

        <UFormField :label="t('token.edit.quota')" name="remain_quota">
          <div class="flex flex-wrap items-center gap-2">
            <UInputNumber v-model="inputs.remain_quota" :disabled="inputs.unlimited_quota" class="w-48" />
            <UCheckbox v-model="inputs.unlimited_quota" :label="t('token.edit.unlimited_quota')" />
          </div>
          <p class="mt-1 text-xs text-gray-500">
            当前展示方式下约等于 {{ renderQuota(inputs.remain_quota, quotaConfig) }}
          </p>
        </UFormField>

        <UFormField :label="t('token.edit.models')" name="models">
          <USelectMenu
            v-model="inputs.models"
            :items="modelOptions"
            multiple
            value-key="value"
            class="w-full"
            :placeholder="t('token.edit.models_placeholder')"
          />
          <p class="mt-1 text-xs text-gray-500">{{ t('token.edit.models_notice') }}</p>
        </UFormField>

        <UFormField :label="t('token.edit.subnet')" name="subnet">
          <UInput v-model="inputs.subnet" :placeholder="t('token.edit.subnet_placeholder')" class="w-full" />
        </UFormField>

        <div class="flex justify-end gap-2 pt-2">
          <UButton color="neutral" variant="ghost" @click="cancel">
            {{ t('token.edit.buttons.cancel') }}
          </UButton>
          <UButton type="submit" color="primary" :loading="loading">
            {{ t('token.edit.buttons.submit') }}
          </UButton>
        </div>
      </UForm>
    </PageCard>
  </div>
</template>
