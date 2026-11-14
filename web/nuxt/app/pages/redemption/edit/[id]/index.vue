<script setup lang="ts">
/**
 * 新建 / 编辑兑换码。
 *
 * 新建时可指定生成数量（后端会批量创建 N 条并返回它们的 key 列表），
 * 这是原实现里的「批量生成」入口。
 */
import type { Redemption } from '~/utils/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { quotaConfig } = useUserStore()
const { success, showError } = useAppToast()

const redemptionId = computed(() => (route.params.id ? Number(route.params.id) : 0))
const isEdit = computed(() => redemptionId.value > 0)
const loading = ref(false)

const inputs = reactive({
  id: 0,
  name: '',
  quota: 100000,
  count: 1,
})

onMounted(async () => {
  if (!isEdit.value) return
  loading.value = true
  try {
    const res = await api.get<Redemption>(`/api/redemption/${redemptionId.value}`)
    const data = res.data
    if (!data) return
    Object.assign(inputs, { id: data.id, name: data.name, quota: data.quota })
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
})

async function submit() {
  loading.value = true
  try {
    if (isEdit.value) {
      await api.put('/api/redemption/', { id: inputs.id, name: inputs.name, quota: inputs.quota })
      success(t('redemption.messages.update_success'))
    } else {
      const res = await api.post<string[]>('/api/redemption/', {
        name: inputs.name,
        quota: inputs.quota,
        count: inputs.count,
      })
      const keys = res.data || []
      // 批量生成的 key 直接给出，方便管理员立刻分发
      if (keys.length > 1) {
        const ok = await copyText(keys.join('\n'))
        if (ok) success(`已生成 ${keys.length} 个兑换码，并复制到剪贴板`)
        else success(`已生成 ${keys.length} 个兑换码`)
      } else {
        success(t('redemption.messages.create_success'))
      }
    }
    await router.push('/redemption')
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

function cancel() {
  router.push('/redemption')
}
</script>

<template>
  <div>
    <PageCard :title="isEdit ? t('redemption.edit.title_edit') : t('redemption.edit.title_create')">
      <UForm :state="inputs" :loading="loading" class="max-w-xl space-y-4" @submit="submit">
        <UFormField :label="t('redemption.edit.name')" name="name">
          <UInput v-model="inputs.name" :placeholder="t('redemption.edit.name_placeholder')" class="w-full" />
        </UFormField>

        <UFormField :label="t('redemption.edit.quota')" name="quota">
          <UInputNumber v-model="inputs.quota" class="w-full" />
          <p class="mt-1 text-xs text-gray-500">
            当前展示方式下约等于 {{ renderQuota(inputs.quota, quotaConfig) }}
          </p>
        </UFormField>

        <UFormField v-if="!isEdit" label="生成数量" name="count">
          <UInputNumber v-model="inputs.count" :min="1" :max="100" class="w-full" />
          <p class="mt-1 text-xs text-gray-500">一次最多生成 100 个，相同的名称会自动加序号后缀。</p>
        </UFormField>

        <div class="flex justify-end gap-2 pt-2">
          <UButton color="neutral" variant="ghost" @click="cancel">
            {{ t('redemption.edit.buttons.cancel') }}
          </UButton>
          <UButton type="submit" color="primary" :loading="loading">
            {{ t('redemption.edit.buttons.submit') }}
          </UButton>
        </div>
      </UForm>
    </PageCard>
  </div>
</template>
