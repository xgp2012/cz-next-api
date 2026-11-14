<script setup lang="ts">
/**
 * 新建 / 编辑用户。
 *
 * - 新建：POST /api/user/（管理员）
 * - 编辑他人：PUT /api/user/（管理员）
 * - 编辑自己：PUT /api/user/self
 *
 * 注意后端校验：Username max=12，Password min=8 max=20，DisplayName max=20。
 * 密码留空表示「不修改」，后端会用占位值绕过校验。
 */
import type { User } from '~/utils/types'
import { ROLE_ADMIN, ROLE_ROOT } from '~/utils/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { user: currentUser, isAdmin, loadStatus, quotaConfig, setUser } = useUserStore()
const { success, showError, warning } = useAppToast()

const userId = computed(() => (route.params.id ? Number(route.params.id) : 0))
const isEdit = computed(() => userId.value > 0)
const isSelf = computed(() => isEdit.value && currentUser.value?.id === userId.value)
const loading = ref(false)

const inputs = reactive({
  id: 0,
  username: '',
  password: '',
  display_name: '',
  group: '',
  quota: 0,
  role: 1,
  github_id: '',
  wechat_id: '',
  email: '',
})

const groupOptions = ref<{ label: string, value: string }[]>([])

const roleOptions = computed(() => {
  const options = [{ label: t('user.table.role_types.normal'), value: 1 }]
  // 只有 root 能创建管理员；管理员最多创建普通用户（后端同样限制）
  if ((currentUser.value?.role ?? 0) >= ROLE_ROOT) {
    options.push({ label: t('user.table.role_types.admin'), value: ROLE_ADMIN })
    options.push({ label: t('user.table.role_types.super_admin'), value: ROLE_ROOT })
  } else if ((currentUser.value?.role ?? 0) >= ROLE_ADMIN) {
    options.push({ label: t('user.table.role_types.admin'), value: ROLE_ADMIN })
  }
  return options
})

onMounted(async () => {
  loadStatus()
  await fetchGroups()
  if (isEdit.value) {
    await loadUser()
  }
})

async function fetchGroups() {
  try {
    const res = await api.get<string[]>('/api/group/')
    groupOptions.value = (res.data || []).map((group) => ({ label: group, value: group }))
  } catch {
    groupOptions.value = [{ label: 'default', value: 'default' }]
  }
}

async function loadUser() {
  loading.value = true
  try {
    const res = await api.get<User>(isSelf.value ? '/api/user/self' : `/api/user/${userId.value}`)
    const data = res.data
    if (!data) return
    Object.assign(inputs, {
      id: data.id,
      username: data.username,
      password: '',
      display_name: data.display_name || '',
      group: data.group || 'default',
      quota: data.quota,
      role: data.role,
      github_id: data.github_id || '',
      wechat_id: data.wechat_id || '',
      email: data.email || '',
    })
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (!inputs.username) {
    warning(t('user.edit.username'))
    return
  }
  if (inputs.password && (inputs.password.length < 8 || inputs.password.length > 20)) {
    warning(t('messages.error.password_length'))
    return
  }
  loading.value = true
  try {
    const payload: Record<string, any> = {
      username: inputs.username,
      password: inputs.password,
      display_name: inputs.display_name,
      group: inputs.group,
    }

    if (isEdit.value) {
      if (isSelf.value) {
        // 自己改自己：不能改角色和额度
        await api.put('/api/user/self', payload)
        success(t('user.messages.update_success'))
        // 刷新本地登录态
        const me = await api.get<User>('/api/user/self')
        if (me.data) setUser(me.data)
      } else {
        payload.id = inputs.id
        payload.quota = inputs.quota
        payload.role = inputs.role
        await api.put('/api/user/', payload)
        success(t('user.messages.update_success'))
      }
    } else {
      payload.quota = inputs.quota
      payload.role = inputs.role
      await api.post('/api/user/', payload)
      success(t('user.messages.create_success'))
    }
    await router.push('/user')
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

function cancel() {
  router.push('/user')
}
</script>

<template>
  <div>
    <PageCard :title="isEdit ? t('user.edit.title') : t('user.add.title')">
      <UForm :state="inputs" :loading="loading" class="space-y-4" @submit="submit">
        <div class="grid gap-4 md:grid-cols-2">
          <UFormField :label="t('user.edit.username')" name="username" required>
            <UInput v-model="inputs.username" :placeholder="t('user.edit.username_placeholder')" class="w-full" />
          </UFormField>

          <UFormField :label="t('user.edit.password')" name="password">
            <UInput
              v-model="inputs.password"
              type="password"
              :placeholder="t('user.edit.password_placeholder')"
              autocomplete="new-password"
              class="w-full"
            />
          </UFormField>

          <UFormField :label="t('user.edit.display_name')" name="display_name">
            <UInput
              v-model="inputs.display_name"
              :placeholder="t('user.edit.display_name_placeholder')"
              class="w-full"
            />
          </UFormField>

          <UFormField :label="t('user.edit.group')" name="group">
            <USelect
              v-model="inputs.group"
              :items="groupOptions"
              :placeholder="t('user.edit.group_placeholder')"
              class="w-full"
            />
          </UFormField>

          <!-- 额度与角色只对「管理员编辑他人」可见；自己不能给自己提权 -->
          <template v-if="!isSelf">
            <UFormField :label="t('user.edit.quota')" name="quota">
              <UInputNumber v-model="inputs.quota" class="w-full" />
              <p class="mt-1 text-xs text-gray-500">
                当前展示方式下约等于 {{ renderQuota(inputs.quota, quotaConfig) }}
              </p>
            </UFormField>

            <UFormField :label="t('user.table.role_text')" name="role">
              <USelect v-model="inputs.role" :items="roleOptions" class="w-full" />
            </UFormField>
          </template>
        </div>

        <div class="grid gap-4 md:grid-cols-3">
          <UFormField :label="t('user.edit.github_id')">
            <UInput
              :model-value="inputs.github_id"
              :placeholder="t('user.edit.github_id_placeholder')"
              readonly
              class="w-full"
            />
          </UFormField>
          <UFormField :label="t('user.edit.wechat_id')">
            <UInput
              :model-value="inputs.wechat_id"
              :placeholder="t('user.edit.wechat_id_placeholder')"
              readonly
              class="w-full"
            />
          </UFormField>
          <UFormField :label="t('user.edit.email')">
            <UInput
              :model-value="inputs.email"
              :placeholder="t('user.edit.email_placeholder')"
              readonly
              class="w-full"
            />
          </UFormField>
        </div>

        <p class="text-xs text-gray-500">{{ t('user.edit.group_addition') }}</p>

        <div class="flex justify-end gap-2 pt-2">
          <UButton color="neutral" variant="ghost" @click="cancel">
            {{ t('user.edit.buttons.cancel') }}
          </UButton>
          <UButton type="submit" color="primary" :loading="loading">
            {{ t('user.edit.buttons.submit') }}
          </UButton>
        </div>
      </UForm>
    </PageCard>
  </div>
</template>
