<script setup lang="ts">
/**
 * 系统设置。
 *
 * 四个标签页与原实现一致：
 * - personal：个人信息 / 账号绑定 / 注销账户（所有登录用户可见）
 * - operation：额度、倍率、日志、监控、通用（root）
 * - system：服务器地址、登录注册、邮箱限制、SMTP、OAuth、Turnstile（root）
 * - other：公告、系统名称与 Logo、内容设置（root）
 *
 * 非 root 用户只渲染「个人设置」，其余标签页不出现（后端也会拒绝访问 Option 接口）。
 */
import { ROLE_ROOT } from '~/utils/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { user, isRoot, loadStatus } = useUserStore()

const tabs = computed(() => {
  const list = [{ key: 'personal', label: t('setting.tabs.personal'), icon: 'i-heroicons-user' }]
  if (isRoot.value) {
    list.push(
      { key: 'operation', label: t('setting.tabs.operation'), icon: 'i-heroicons-chart-bar' },
      { key: 'system', label: t('setting.tabs.system'), icon: 'i-heroicons-cog-6-tooth' },
      { key: 'other', label: t('setting.tabs.other'), icon: 'i-heroicons-squares-2x2' },
    )
  }
  return list
})

/** 用 ?tab= 同步标签页，刷新后停留在同一页 */
const activeTab = computed({
  get: () => {
    const requested = String(route.query.tab || 'personal')
    return tabs.value.some((tab) => tab.key === requested) ? requested : 'personal'
  },
  set: (value: string) => {
    router.replace({ query: { ...route.query, tab: value } })
  },
})

onMounted(() => loadStatus())
</script>

<template>
  <div>
    <PageCard :title="t('setting.title')">
      <UTabs
        v-model="activeTab"
        :items="tabs"
        value-key="key"
        class="mb-6"
      >
        <template #content="{ item }">
          <SettingPersonal v-if="item.key === 'personal'" />
          <SettingOperation v-else-if="item.key === 'operation'" />
          <SettingSystem v-else-if="item.key === 'system'" />
          <SettingOther v-else-if="item.key === 'other'" />
        </template>
      </UTabs>
    </PageCard>
  </div>
</template>
