<template>
  <div class="oa-main flex flex-col">
    <AppHeader />
    <AuthWatcher />
    <main class="oa-container flex-1 py-4">
      <slot />
    </main>
    <AppFooter />
  </div>
</template>
