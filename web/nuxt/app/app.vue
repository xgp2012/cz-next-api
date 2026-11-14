<script setup lang="ts">
/**
 * 应用根组件。
 *
 * 职责：
 * 1. 挂载 UApp（@nuxt/ui 的 Provider 根，Toast/Tooltip/Overlay 都依赖它）
 * 2. 客户端首次挂载时恢复身份 + 拉取系统状态（/api/status）
 * 3. 根据系统名称 / Logo 动态改写 document.title 与 favicon
 */
const { restoreUser, loadStatus, systemName, logo, hydrated } = useUserStore()

onMounted(() => {
  restoreUser()
  if (!hydrated.value) hydrated.value = true
  loadStatus()
})

useHead({
  titleTemplate: () => systemName.value,
  link: [
    {
      rel: 'icon',
      // 系统 Logo 由管理员在后台配置，可能随时变化
      href: () => logo.value || '/logo.png',
    },
  ],
})
</script>

<template>
  <UApp>
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </UApp>
</template>
