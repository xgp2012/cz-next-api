<script setup lang="ts">
/**
 * 关于页。
 * 内容来自 Option "About"：链接则 iframe，否则 Markdown；未配置时展示默认说明。
 */
const { t } = useI18n()
const about = ref('')
const loaded = ref(false)

const isIframe = computed(() => about.value.startsWith('https://'))

onMounted(async () => {
  about.value = localStorage.getItem('about') || ''
  try {
    const res = await api.get<string>('/api/about')
    const content = res.data || ''
    const html = content.startsWith('https://') ? content : renderMarkdown(content)
    about.value = html
    localStorage.setItem('about', html)
  } catch {
    about.value = t('about.loading_failed')
  } finally {
    loaded.value = true
  }
})
</script>

<template>
  <div>
    <iframe v-if="loaded && isIframe" :src="about" class="h-screen w-full border-0" />

    <PageCard v-else-if="loaded && about">
      <div class="oa-prose text-lg" v-html="about" />
    </PageCard>

    <PageCard v-else-if="loaded" :title="t('about.title')">
      <p class="text-gray-700">{{ t('about.description') }}</p>
      <p class="mt-2 text-gray-700">
        {{ t('about.repository') }}
        <a
          href="https://github.com/songquanpeng/one-api"
          target="_blank"
          rel="noopener"
          class="text-[var(--oa-brand)] hover:underline"
        >https://github.com/songquanpeng/one-api</a>
      </p>
    </PageCard>

    <div v-else class="flex justify-center py-16">
      <UIcon name="i-heroicons-arrow-path" class="size-6 animate-spin text-gray-400" />
    </div>
  </div>
</template>
