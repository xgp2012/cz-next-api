<script setup lang="ts">
/**
 * 首页。
 *
 * 行为与旧实现保持一致：
 * - 管理员可通过 Option "HomePageContent" 覆盖首页：值是 https:// 开头的链接时用 iframe，
 *   否则按 Markdown 渲染；未配置则展示「欢迎 + 系统状态」两栏卡片。
 * - 进入首页时若公告（Option "Notice"）有变化，弹一次通知（用 localStorage 去重）。
 *
 * 预渲染注意：所有内容依赖后端，因此只在客户端拉取，避免把构建机的网络错误烤进 HTML。
 */
const { t } = useI18n()
const { status, isLogin, loadStatus, statusLoaded } = useUserStore()
const { showError, notice: showNotice } = useAppToast()

const homePageContent = ref('')
const loaded = ref(false)
const bannerHtml = ref('')

const isIframe = computed(() => homePageContent.value.startsWith('https://'))
const showDefaultContent = computed(() => loaded.value && homePageContent.value === '')

async function loadHomePageContent() {
  if (!import.meta.client) return
  // 缓存优先，避免白屏
  homePageContent.value = localStorage.getItem('home_page_content') || ''
  try {
    const res = await api.get<string>('/api/home_page_content')
    const content = res.data || ''
    const html = content.startsWith('https://') ? content : renderMarkdown(content)
    homePageContent.value = html
    localStorage.setItem('home_page_content', html)
  } catch {
    homePageContent.value = t('home.loading_failed')
  } finally {
    loaded.value = true
  }
}

async function loadNotice() {
  if (!import.meta.client) return
  try {
    const res = await api.get<string>('/api/notice')
    const content = res.data || ''
    const old = localStorage.getItem('notice')
    if (content && content !== old) {
      bannerHtml.value = renderMarkdown(content)
      showNotice(renderMarkdown(content), true)
      localStorage.setItem('notice', content)
    }
  } catch {
    // 公告失败不打扰用户
  }
}

onMounted(() => {
  loadStatus()
  loadNotice()
  loadHomePageContent()
})

const startTime = computed(() => (status.value?.start_time ? timestamp2string(status.value.start_time) : '-'))

interface ConfigFlag {
  label: string
  icon: string
  enabled: boolean | undefined
}

const configFlags = computed<ConfigFlag[]>(() => [
  {
    label: t('home.system_status.config.email_verify'),
    icon: 'i-heroicons-envelope',
    enabled: status.value?.email_verification,
  },
  {
    label: t('home.system_status.config.github_oauth'),
    icon: 'i-simple-icons-github',
    enabled: status.value?.github_oauth,
  },
  {
    label: t('home.system_status.config.wechat_login'),
    icon: 'i-simple-icons-wechat',
    enabled: status.value?.wechat_login,
  },
  {
    label: t('home.system_status.config.turnstile'),
    icon: 'i-heroicons-shield-check',
    enabled: status.value?.turnstile_check,
  },
])
</script>

<template>
  <div>
    <!-- 后端配置了首页内容 -->
    <template v-if="loaded && isIframe">
      <iframe :src="homePageContent" class="h-screen w-full border-0" />
    </template>
    <div v-else-if="loaded && homePageContent" class="oa-prose text-lg" v-html="homePageContent" />

    <!-- 默认首页：欢迎 + 系统状态 -->
    <div v-else-if="showDefaultContent" class="flex flex-col gap-4">
      <PageCard :title="t('home.welcome.title')">
        <p class="leading-relaxed text-gray-700">{{ t('home.welcome.description') }}</p>
        <p v-if="!isLogin" class="mt-2 text-gray-600">{{ t('home.welcome.login_notice') }}</p>
        <div class="mt-4 flex gap-2">
          <UButton v-if="!isLogin" to="/login" color="primary">
            {{ t('header.login') }}
          </UButton>
          <UButton v-if="!isLogin" to="/register" color="neutral" variant="outline">
            {{ t('header.register') }}
          </UButton>
          <UButton v-if="isLogin" to="/token" color="primary">
            {{ t('header.token') }}
          </UButton>
        </div>
      </PageCard>

      <PageCard :title="t('home.system_status.title')">
        <div class="grid gap-4 md:grid-cols-2">
          <div class="rounded-lg border border-gray-200 p-4">
            <h3 class="mb-3 text-base font-semibold text-gray-700">
              {{ t('home.system_status.info.title') }}
            </h3>
            <dl class="space-y-2 text-sm">
              <div class="flex items-center gap-2">
                <UIcon name="i-heroicons-information-circle" class="size-4 text-gray-400" />
                <dt class="font-medium">{{ t('home.system_status.info.name') }}</dt>
                <dd>{{ status?.system_name || '-' }}</dd>
              </div>
              <div class="flex items-center gap-2">
                <UIcon name="i-heroicons-code-bracket" class="size-4 text-gray-400" />
                <dt class="font-medium">{{ t('home.system_status.info.version') }}</dt>
                <dd>{{ status?.version || 'unknown' }}</dd>
              </div>
              <div class="flex items-center gap-2">
                <UIcon name="i-simple-icons-github" class="size-4 text-gray-400" />
                <dt class="font-medium">{{ t('home.system_status.info.source') }}</dt>
                <dd>
                  <a
                    href="https://github.com/songquanpeng/one-api"
                    target="_blank"
                    rel="noopener"
                    class="text-[var(--oa-brand)] hover:underline"
                  >{{ t('home.system_status.info.source_link') }}</a>
                </dd>
              </div>
              <div class="flex items-center gap-2">
                <UIcon name="i-heroicons-clock" class="size-4 text-gray-400" />
                <dt class="font-medium">{{ t('home.system_status.info.start_time') }}</dt>
                <dd>{{ startTime }}</dd>
              </div>
            </dl>
          </div>

          <div class="rounded-lg border border-gray-200 p-4">
            <h3 class="mb-3 text-base font-semibold text-gray-700">
              {{ t('home.system_status.config.title') }}
            </h3>
            <dl class="space-y-2 text-sm">
              <div v-for="flag in configFlags" :key="flag.label" class="flex items-center gap-2">
                <UIcon :name="flag.icon" class="size-4 text-gray-400" />
                <dt class="font-medium">{{ flag.label }}</dt>
                <dd
                  class="font-medium"
                  :class="flag.enabled ? 'text-green-600' : 'text-red-600'"
                >
                  {{ flag.enabled ? t('home.system_status.config.enabled') : t('home.system_status.config.disabled') }}
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </PageCard>
    </div>

    <div v-else class="flex justify-center py-16">
      <UIcon name="i-heroicons-arrow-path" class="size-6 animate-spin text-gray-400" />
    </div>
  </div>
</template>
