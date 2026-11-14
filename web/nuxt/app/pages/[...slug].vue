<script setup lang="ts">
const { t } = useI18n()
</script>

<template>
  <UAlert
    class="mx-auto max-w-lg"
    color="error"
    variant="subtle"
    icon="i-heroicons-magnifying-glass"
    :title="t('errors.not_found.title')"
    :description="t('errors.not_found.description')"
  />
</template>
