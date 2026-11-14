<script setup lang="ts">
/**
 * 登录态守卫的渲染层。
 *
 * 与旧 PrivateRoute 的差异：
 * - 旧实现在模块顶层读 localStorage，SSR 下会直接抛错；这里是客户端判断。
 * - 必须等 hydrated（localStorage 恢复完成）后再决定是否拦截，
 *   否则刷新页面时会闪一下 /login 再跳回来。
 * - 真正的跳转由 AuthWatcher（客户端插件）负责，这里只负责「不渲染受保护内容」。
 */
const { isLogin, hydrated, restoreUser } = useUserStore()

onMounted(() => {
  if (!hydrated.value) restoreUser()
})
</script>

<template>
  <slot v-if="isLogin" />
  <div v-else-if="!hydrated" class="flex justify-center py-16">
    <UIcon name="i-heroicons-arrow-path" class="size-6 animate-spin text-gray-400" />
  </div>
  <div v-else class="mx-auto max-w-md py-10">
    <UAlert
      color="warning"
      icon="i-heroicons-lock-closed"
      title="需要登录"
      description="未登录或登录已过期，正在跳转到登录页面..."
    />
  </div>
</template>
