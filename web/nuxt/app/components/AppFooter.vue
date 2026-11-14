<script setup lang="ts">
/**
 * 页脚。
 * 后端可通过 Option "Footer" 下发自定义 HTML，留空则回落到默认版权信息。
 * 注意：footer_html 来自管理员配置，属于可信内容（与旧实现同样按 HTML 内联渲染）。
 */
const { t } = useI18n()
const { systemName, footerHtml } = useUserStore()
const config = useRuntimeConfig()
const version = config.public.appVersion as string
</script>

<template>
  <footer class="border-t border-gray-100 bg-white py-6">
    <div class="oa-container text-center text-[1.05rem] text-gray-500">
      <div v-if="footerHtml" class="oa-prose" v-html="footerHtml" />
      <div v-else>
        <a
          href="https://github.com/songquanpeng/one-api"
          target="_blank"
          rel="noopener"
          class="text-[var(--oa-brand)] hover:underline"
        >{{ systemName }} {{ version }}</a>
        {{ t('footer.built_by') }}
        <a
          href="https://github.com/songquanpeng"
          target="_blank"
          rel="noopener"
          class="text-[var(--oa-brand)] hover:underline"
        >{{ t('footer.built_by_name') }}</a>
        {{ t('footer.license') }}
        <a
          href="https://opensource.org/licenses/mit-license.php"
          target="_blank"
          rel="noopener"
          class="text-[var(--oa-brand)] hover:underline"
        >{{ t('footer.mit') }}</a>
      </div>
    </div>
  </footer>
</template>
