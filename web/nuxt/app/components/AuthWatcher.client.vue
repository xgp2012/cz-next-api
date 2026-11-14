<script setup lang="ts">
/**
 * 未登录时自动跳转 /login。
 * 放在 AuthGuard 里做跳转会让模板再渲染一帧，这里用独立的客户端插件做副作用更干净。
 */
const { isLogin, hydrated, restoreUser, loadStatus } = useUserStore()
const route = useRoute()

const PUBLIC_ROUTES = new Set([
  '/',
  '/about',
  '/login',
  '/register',
  '/reset',
  '/user/reset',
  '/oauth/github',
  '/oauth/lark',
  '/chat',
  // 价格墙是公开页：未登录的访客也要能在注册前看到定价，
  // 否则转化漏斗的第一步会被守卫直接掐断（后端 /api/pricing 也不需要鉴权）。
  '/pricing',
])

onMounted(() => {
  if (!hydrated.value) restoreUser()
  loadStatus()

  // 首次挂载时如果目标是受保护页面且未登录，回登录页
  if (!isLogin.value && !PUBLIC_ROUTES.has(route.path)) {
    navigateTo('/login')
  }
})

// 后续的路由切换也要校验（SPA 内部跳转不触发整页刷新）
watch(
  () => route.path,
  (path) => {
    if (!hydrated.value) return
    if (!isLogin.value && !PUBLIC_ROUTES.has(path)) {
      navigateTo('/login')
    }
  },
)
</script>

<template>
  <div />
</template>
