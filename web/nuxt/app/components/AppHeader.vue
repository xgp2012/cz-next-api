<script setup lang="ts">
/**
 * 顶部导航。
 *
 * 与旧 Header 的对应关系：
 * - 桌面端：Logo + 系统名 + 一组图标导航 + 语言切换 + 账号下拉
 * - 移动端：折叠为汉堡菜单（USlideover）
 * - 管理项（渠道/兑换/用户）只对 role >= 10 显示；聊天项只在后端配置了 chat_link 时出现
 */
const { t, locale, setLocale } = useI18n()
const { user, isLogin, isAdmin, systemName, logo, chatLink, logout: clearUser } = useUserStore()
const { success, showError } = useAppToast()
const router = useRouter()

interface NavItem {
  key: string
  to: string
  icon: string
  /** t() 的兜底文案：商业化相关的新页面在历史语言包里没有对应 key */
  label?: string
  admin?: boolean
  chat?: boolean
  /** 仅登录用户可见（钱包/订阅/推广这类需要身份的功能） */
  login?: boolean
}

const navItems = computed<NavItem[]>(() => {
  const items: NavItem[] = [
    { key: 'header.channel', to: '/channel', icon: 'i-heroicons-server-stack', admin: true },
  ]
  if (chatLink.value) {
    items.push({ key: 'header.chat', to: '/chat', icon: 'i-heroicons-chat-bubble-left-right', chat: true })
  }
  items.push(
    { key: 'header.token', to: '/token', icon: 'i-heroicons-key' },
    // ---- 商业化：价格墙公开（转化漏斗的第一步），钱包/订阅/推广需要登录 ----
    { key: 'header.pricing', to: '/pricing', icon: 'i-heroicons-tag', label: '价格' },
    { key: 'header.wallet', to: '/wallet', icon: 'i-heroicons-wallet', login: true, label: '钱包' },
    { key: 'header.subscription', to: '/subscription', icon: 'i-heroicons-credit-card', login: true, label: '订阅' },
    { key: 'header.aff', to: '/aff', icon: 'i-heroicons-megaphone', login: true, label: '推广' },
    { key: 'header.redemption', to: '/redemption', icon: 'i-heroicons-ticket', admin: true },
    { key: 'header.user', to: '/user', icon: 'i-heroicons-users', admin: true },
    { key: 'header.dashboard', to: '/dashboard', icon: 'i-heroicons-chart-bar' },
    // 营收看板与定价规则管理只对管理员出现
    { key: 'header.revenue', to: '/revenue', icon: 'i-heroicons-presentation-chart-line', admin: true, label: '营收' },
    {
      key: 'header.pricing_manage',
      to: '/pricing/manage',
      icon: 'i-heroicons-adjustments-horizontal',
      admin: true,
      label: '定价规则',
    },
    { key: 'header.topup', to: '/topup', icon: 'i-heroicons-shopping-cart' },
    { key: 'header.log', to: '/log', icon: 'i-heroicons-book-open' },
    { key: 'header.setting', to: '/setting', icon: 'i-heroicons-cog-6-tooth' },
    { key: 'header.about', to: '/about', icon: 'i-heroicons-information-circle' },
  )
  return items.filter((item) => (!item.admin || isAdmin.value) && (!item.login || isLogin.value))
})

const mobileOpen = ref(false)

const languages = [
  { label: '中文', value: 'zh' },
  { label: 'English', value: 'en' },
]

const currentLanguage = computed({
  get: () => locale.value,
  set: (value: string) => setLocale(value),
})

async function handleLogout() {
  mobileOpen.value = false
  try {
    await api.get('/api/user/logout', { silent: true })
  } catch {
    // 后端注销失败也要清本地态
  }
  clearUser()
  success(t('header.logout'))
  await router.push('/login')
}

function go(to: string) {
  mobileOpen.value = false
  router.push(to)
}
</script>

<template>
  <header
    class="fixed inset-x-0 top-0 z-40 h-14 border-b border-gray-100 bg-white shadow-[0_2px_12px_rgba(0,0,0,0.04)]"
  >
    <div class="oa-container flex h-full items-center gap-1">
      <!-- Logo + 系统名 -->
      <NuxtLink to="/" class="flex shrink-0 items-center gap-2 pr-3">
        <img :src="logo" alt="logo" class="h-7 w-7 object-contain">
        <span class="text-lg font-medium text-gray-800">{{ systemName }}</span>
      </NuxtLink>

      <!-- 桌面导航 -->
      <nav class="hidden flex-1 items-center gap-0.5 md:flex">
        <NuxtLink
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="flex items-center gap-1 rounded-md px-2 py-1.5 text-[15px] text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900"
          active-class="bg-gray-100 font-medium text-gray-900"
        >
          <UIcon :name="item.icon" class="size-4" />
          <span>{{ t(item.key, item.label || item.key) }}</span>
        </NuxtLink>
      </nav>

      <div class="flex flex-1 items-center justify-end gap-2 md:flex-none">
        <!-- 语言切换 -->
        <USelect
          v-model="currentLanguage"
          :items="languages"
          size="sm"
          class="w-24"
          icon="i-heroicons-language"
          value-key="value"
        />

        <!-- 账号 -->
        <template v-if="isLogin">
          <UDropdownMenu
            :items="[[{ label: t('header.logout'), icon: 'i-heroicons-arrow-right-on-rectangle', onSelect: handleLogout }]]"
          >
            <UButton color="neutral" variant="ghost" trailing-icon="i-heroicons-chevron-down">
              {{ user?.username }}
            </UButton>
          </UDropdownMenu>
        </template>
        <template v-else>
          <UButton to="/login" color="neutral" variant="ghost" size="sm">
            {{ t('header.login') }}
          </UButton>
        </template>

        <!-- 移动端菜单按钮 -->
        <UButton
          class="md:hidden"
          color="neutral"
          variant="ghost"
          icon="i-heroicons-bars-3"
          aria-label="menu"
          @click="mobileOpen = true"
        />
      </div>
    </div>
  </header>

  <USlideover v-model:open="mobileOpen" title="导航">
    <template #body>
      <nav class="flex flex-col gap-1">
        <UButton
          v-for="item in navItems"
          :key="item.to"
          :icon="item.icon"
          color="neutral"
          variant="ghost"
          block
          class="justify-start"
          @click="go(item.to)"
        >
          {{ t(item.key, item.label || item.key) }}
        </UButton>
        <USeparator class="my-2" />
        <UButton
          v-if="isLogin"
          icon="i-heroicons-arrow-right-on-rectangle"
          color="neutral"
          variant="ghost"
          block
          class="justify-start"
          @click="handleLogout"
        >
          {{ t('header.logout') }}
        </UButton>
        <template v-else>
          <UButton color="neutral" variant="ghost" block class="justify-start" @click="go('/login')">
            {{ t('header.login') }}
          </UButton>
          <UButton color="neutral" variant="ghost" block class="justify-start" @click="go('/register')">
            {{ t('header.register') }}
          </UButton>
        </template>
      </nav>
    </template>
  </USlideover>
</template>
