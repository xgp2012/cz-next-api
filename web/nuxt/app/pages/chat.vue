<script setup lang="ts">
/**
 * 内置聊天页：仅当后端配置了 Option "ChatLink" 时可用，
 * 用 iframe 承载外部 Chat 应用（例如 ChatGPT Next Web）。
 */
const { chatLink } = useUserStore()
</script>

<template>
  <div>
    <iframe v-if="chatLink" :src="chatLink" class="h-[85vh] w-full border-0" />
    <UAlert
      v-else
      class="mx-auto max-w-lg"
      color="info"
      icon="i-heroicons-information-circle"
      title="未配置聊天页面"
      description="请管理员在「运营设置 → 通用设置」中填写聊天页面链接。"
    />
  </div>
</template>
