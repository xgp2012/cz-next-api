<script setup lang="ts">
/**
 * 状态徽章的统一封装：把后端的状态码映射成 @nuxt/ui 的 Badge 颜色。
 * 后端状态码定义见 model/*.go 与附录 B。
 */
const props = withDefaults(
  defineProps<{
    color?: 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'error' | 'neutral'
    label?: string | number
    variant?: 'soft' | 'subtle' | 'outline' | 'solid'
  }>(),
  { color: 'neutral', variant: 'subtle' },
)
</script>

<template>
  <UBadge :color="props.color" :variant="props.variant" size="sm">
    <slot>{{ props.label }}</slot>
  </UBadge>
</template>
