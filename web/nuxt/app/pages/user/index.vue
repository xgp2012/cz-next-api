<script setup lang="ts">
/**
 * 用户管理（仅管理员），列表与分页。
 *
 * 后端权限约束（controller/user.go）：
 * - 不能查看/删除与自己同级或更高等级的用户；
 * - root 可绕过「同级」限制。
 * 所以按钮的 disabled 状态要按当前登录者的角色计算。
 */
import type { User } from '~/utils/types'
import { ROLE_ADMIN, ROLE_ROOT } from '~/utils/types'
import { ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { user: currentUser, quotaConfig, loadStatus } = useUserStore()
const { success, showError } = useAppToast()

const searchKeyword = ref('')
const searching = ref(false)
const searchMode = ref(false)
const orderBy = ref('')

const orderOptions = computed(() => [
  { label: t('user.table.sort.default'), value: '' },
  { label: t('user.table.sort.by_quota'), value: 'quota' },
  { label: t('user.table.sort.by_used_quota'), value: 'used_quota' },
  { label: t('user.table.sort.by_request_count'), value: 'request_count' },
])

const {
  rows: users,
  loading,
  activePage,
  totalPages,
  loadPage,
  refresh,
  onPageChange,
} = usePagedTable({
  fetcher: async (startIdx) => {
    const res = await api.get<User[]>('/api/user/', {
      query: { p: startIdx, order: orderBy.value },
    })
    return res.data || []
  },
})

onMounted(() => {
  loadStatus()
  loadPage(0)
})

watch(orderBy, () => {
  if (!searchMode.value) refresh()
})

async function searchUsers() {
  if (!searchKeyword.value) {
    searchMode.value = false
    await refresh()
    return
  }
  searching.value = true
  try {
    const res = await api.get<User[]>('/api/user/search', { query: { keyword: searchKeyword.value } })
    users.value = res.data || []
    activePage.value = 1
    searchMode.value = true
  } catch (error) {
    showError(error)
  } finally {
    searching.value = false
  }
}

/** 能否管理目标用户：严格高于对方角色，root 例外 */
function canManage(target: User): boolean {
  if (!currentUser.value) return false
  if (currentUser.value.role >= ROLE_ROOT) return true
  return currentUser.value.role > target.role
}

const confirmDeleteId = ref<number | null>(null)

async function deleteUser(target: User) {
  try {
    await api.delete(`/api/user/${target.id}`)
    target.status = 3 // UserStatusDeleted
    success(t('user.messages.operation_success'))
  } catch (error) {
    showError(error)
  } finally {
    confirmDeleteId.value = null
  }
}

/** 状态切换：POST /api/user/manage { id, status } */
async function toggleUserStatus(target: User) {
  const nextStatus = target.status === 1 ? 2 : 1
  try {
    const res = await api.post<User>('/api/user/manage', { id: target.id, status: nextStatus })
    target.status = res.data?.status ?? nextStatus
    success(t('user.messages.operation_success'))
  } catch (error) {
    showError(error)
  }
}

function userStatus(target: User) {
  switch (target.status) {
    case 1:
      return { color: 'success' as const, label: t('user.table.status_types.activated') }
    case 2:
      return { color: 'error' as const, label: t('user.table.status_types.banned') }
    case 3:
      return { color: 'neutral' as const, label: '已删除' }
    default:
      return { color: 'neutral' as const, label: t('user.table.status_types.unknown') }
  }
}

function roleLabel(role: number) {
  if (role >= ROLE_ROOT) return t('user.table.role_types.super_admin')
  if (role >= ROLE_ADMIN) return t('user.table.role_types.admin')
  return t('user.table.role_types.normal')
}

function roleColor(role: number) {
  if (role >= ROLE_ROOT) return 'error' as const
  if (role >= ROLE_ADMIN) return 'warning' as const
  return 'neutral' as const
}

const pageUsers = computed(() => {
  const start = (activePage.value - 1) * ITEMS_PER_PAGE
  return users.value.slice(start, start + ITEMS_PER_PAGE)
})
</script>

<template>
  <div>
    <PageCard :title="t('user.title')">
      <template #actions>
        <UButton to="/user/add" icon="i-heroicons-user-plus" color="primary">
          {{ t('user.buttons.add') }}
        </UButton>
      </template>

      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UInput
          v-model="searchKeyword"
          icon="i-heroicons-magnifying-glass"
          :placeholder="t('user.search')"
          class="min-w-56 flex-1"
          @keyup.enter="searchUsers"
        />
        <UButton color="neutral" variant="outline" :loading="searching" @click="searchUsers">
          {{ t('common.search') }}
        </UButton>
        <USelect v-model="orderBy" :items="orderOptions" class="w-44" />
      </div>

      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">{{ t('user.table.id') }}</th>
              <th class="p-2">{{ t('user.table.username') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('user.table.group') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('user.table.role_text') }}</th>
              <th class="p-2">{{ t('user.table.status_text') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('user.table.remaining_quota') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('user.table.used_quota') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('user.table.request_count') }}</th>
              <th class="p-2 text-right">{{ t('user.table.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pageUsers" :key="item.id" class="border-b border-gray-100 hover:bg-gray-50">
              <td class="p-2 text-gray-500">{{ item.id }}</td>
              <td class="p-2">
                <div class="font-medium">{{ item.username }}</div>
                <div class="text-xs text-gray-400">
                  {{ item.display_name }}<span v-if="item.email"> · {{ item.email }}</span>
                </div>
              </td>
              <td class="oa-hide-on-mobile p-2">
                <UBadge color="neutral" variant="subtle" size="sm">{{ item.group || 'default' }}</UBadge>
              </td>
              <td class="oa-hide-on-mobile p-2">
                <UBadge :color="roleColor(item.role)" variant="subtle" size="sm">
                  {{ roleLabel(item.role) }}
                </UBadge>
              </td>
              <td class="p-2">
                <StatusBadge :color="userStatus(item).color" :label="userStatus(item).label" />
              </td>
              <td class="oa-hide-on-mobile p-2">{{ renderQuota(item.quota, quotaConfig) }}</td>
              <td class="oa-hide-on-mobile p-2">{{ renderQuota(item.used_quota, quotaConfig) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ item.request_count }}</td>
              <td class="p-2">
                <div class="flex flex-wrap justify-end gap-1">
                  <UButton
                    size="xs"
                    color="neutral"
                    variant="soft"
                    :to="`/user/edit/${item.id}`"
                    :disabled="!canManage(item) && item.id !== currentUser?.id"
                  >
                    {{ t('user.buttons.edit') }}
                  </UButton>
                  <UButton
                    size="xs"
                    :color="item.status === 1 ? 'error' : 'success'"
                    variant="soft"
                    :disabled="!canManage(item)"
                    @click="toggleUserStatus(item)"
                  >
                    {{ item.status === 1 ? t('user.buttons.disable') : t('user.buttons.enable') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="error"
                    variant="ghost"
                    :disabled="!canManage(item)"
                    @click="confirmDeleteId = item.id"
                  >
                    {{ t('user.buttons.delete') }}
                  </UButton>
                </div>

                <UModal
                  :open="confirmDeleteId === item.id"
                  :title="t('user.buttons.delete_user')"
                  @update:open="(v) => (confirmDeleteId = v ? item.id : null)"
                >
                  <template #body>
                    <p class="text-sm text-gray-600">
                      确定要删除用户「{{ item.username }}」吗？该操作不可恢复。
                    </p>
                  </template>
                  <template #footer>
                    <div class="flex w-full justify-end gap-2">
                      <UButton color="neutral" variant="ghost" @click="confirmDeleteId = null">
                        {{ t('common.cancel') }}
                      </UButton>
                      <UButton color="error" @click="deleteUser(item)">{{ t('common.confirm') }}</UButton>
                    </div>
                  </template>
                </UModal>
              </td>
            </tr>
            <tr v-if="pageUsers.length === 0">
              <td colspan="9" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading') : t('common.none') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="mt-4 flex flex-wrap items-center justify-between gap-2">
        <UButton color="neutral" variant="outline" size="sm" :loading="loading" @click="refresh">
          {{ t('common.refresh') }}
        </UButton>
        <UPagination
          :page="activePage"
          :total="totalPages * ITEMS_PER_PAGE"
          :items-per-page="ITEMS_PER_PAGE"
          size="sm"
          @update:page="onPageChange"
        />
      </div>
    </PageCard>
  </div>
</template>
