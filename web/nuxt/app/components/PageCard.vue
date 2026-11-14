<script setup lang="ts">
/**
 * 内容卡片：替代原来到处出现的 <Card fluid><Card.Content> 组合。
 */
defineProps<{
  title?: string
  subtitle?: string
}>()
</script>

<template>
  <section class="oa-card mb-4 p-5">
    <header v-if="title || $slots.header" class="mb-4 flex flex-wrap items-center justify-between gap-2">
      <div>
        <h2 class="text-xl font-semibold text-gray-800">
          <slot name="header">{{ title }}</slot>
        </h2>
        <p v-if="subtitle" class="mt-1 text-sm text-gray-500">{{ subtitle }}</p>
      </div>
      <slot name="actions" />
    </header>
    <slot />
  </section>
</template>
