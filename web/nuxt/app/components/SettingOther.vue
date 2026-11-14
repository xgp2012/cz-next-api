<script setup lang="ts">
/**
 * 其他设置（root）：公告、系统名称与 Logo、主题、内容设置（首页 / 关于 / 页脚）。
 *
 * 内容项都支持 Markdown 与 HTML；若以 http(s):// 开头，前端会把它当作
 * iframe 的 src（见首页与关于页的渲染逻辑）。
 */
const { t } = useI18n()
const { saving, optionMap, loadOptions, getString, updateOption, updateOptions } = useOptions()
const { applyStatus } = useUserStore()

onMounted(loadOptions)

const notice = ref('')
const system = reactive({ name: '', logo: '', theme: '' })
const content = reactive({ homepage: '', about: '', footer: '' })

watch(optionMap, () => {
  notice.value = getString('Notice')
  system.name = getString('SystemName')
  system.logo = getString('Logo')
  system.theme = getString('Theme', 'default')
  content.homepage = getString('HomePageContent')
  content.about = getString('About')
  content.footer = getString('Footer')
})

async function saveName() {
  if (!(await updateOption('SystemName', system.name))) return
  // 顶栏与页脚都读自本地缓存，更新后要同步一次
  applyStatus({ system_name: system.name })
}

async function saveLogo() {
  if (!(await updateOption('Logo', system.logo))) return
  applyStatus({ logo: system.logo })
}

/**
 * 主题取值必须落在后端 ValidThemes 内，否则 UpdateOption 会拒绝。
 * 该清单与 common/config/config.go 的 ValidThemes、web/THEMES 保持一致：
 * 前端已重构为 Nuxt 单主题实现，只有 default 存在构建产物。
 */
const themeOptions = [
  { label: 'default', value: 'default' },
]
</script>

<template>
  <div class="space-y-6">
    <!-- 公告 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.other.notice.title') }}</div>
      </template>
      <UFormField :label="t('setting.other.notice.content')">
        <UTextarea
          v-model="notice"
          :rows="5"
          :placeholder="t('setting.other.notice.content_placeholder')"
          class="w-full"
        />
      </UFormField>
      <UButton class="mt-4" color="primary" :loading="saving" @click="updateOption('Notice', notice)">
        {{ t('setting.other.notice.buttons.save') }}
      </UButton>
    </UCard>

    <!-- 系统名称 / Logo / 主题 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.other.system.title') }}</div>
      </template>
      <div class="grid gap-4 md:grid-cols-3">
        <UFormField :label="t('setting.other.system.name')">
          <UInput
            v-model="system.name"
            :placeholder="t('setting.other.system.name_placeholder')"
            class="w-full"
          />
          <UButton class="mt-2" color="primary" size="sm" :loading="saving" @click="saveName">
            {{ t('setting.other.system.buttons.save_name') }}
          </UButton>
        </UFormField>

        <UFormField :label="t('setting.other.system.logo')">
          <UInput
            v-model="system.logo"
            :placeholder="t('setting.other.system.logo_placeholder')"
            class="w-full"
          />
          <UButton class="mt-2" color="primary" size="sm" :loading="saving" @click="saveLogo">
            {{ t('setting.other.system.buttons.save_logo') }}
          </UButton>
        </UFormField>

        <UFormField :label="t('setting.other.system.theme.title')">
          <USelect v-model="system.theme" :items="themeOptions" class="w-full" />
          <p class="mt-1 text-xs text-gray-500">
            {{ t('setting.other.system.theme.link') }}：default
          </p>
          <UButton
            class="mt-2"
            color="primary"
            size="sm"
            :loading="saving"
            @click="updateOption('Theme', system.theme)"
          >
            {{ t('setting.other.system.buttons.save_theme') }}
          </UButton>
        </UFormField>
      </div>

      <!-- Logo 预览 -->
      <div v-if="system.logo" class="mt-4">
        <div class="mb-1 text-xs text-gray-500">预览</div>
        <img :src="system.logo" alt="logo" class="h-10 object-contain">
      </div>
    </UCard>

    <!-- 内容设置 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.other.content.title') }}</div>
      </template>
      <div class="space-y-4">
        <UFormField :label="t('setting.other.content.homepage.title')">
          <UTextarea
            v-model="content.homepage"
            :rows="5"
            :placeholder="t('setting.other.content.homepage.placeholder')"
            class="w-full"
          />
          <UButton
            class="mt-2"
            color="primary"
            size="sm"
            :loading="saving"
            @click="updateOption('HomePageContent', content.homepage)"
          >
            {{ t('setting.other.content.buttons.save_homepage') }}
          </UButton>
        </UFormField>

        <UFormField :label="t('setting.other.content.about.title')">
          <UTextarea
            v-model="content.about"
            :rows="5"
            :placeholder="t('setting.other.content.about.placeholder')"
            class="w-full"
          />
          <UButton
            class="mt-2"
            color="primary"
            size="sm"
            :loading="saving"
            @click="updateOption('About', content.about)"
          >
            {{ t('setting.other.content.buttons.save_about') }}
          </UButton>
        </UFormField>

        <UFormField :label="t('setting.other.content.footer.title')">
          <UTextarea
            v-model="content.footer"
            :rows="3"
            :placeholder="t('setting.other.content.footer.placeholder')"
            class="w-full"
          />
          <UButton
            class="mt-2"
            color="primary"
            size="sm"
            :loading="saving"
            @click="updateOption('Footer', content.footer)"
          >
            {{ t('setting.other.content.buttons.save_footer') }}
          </UButton>
        </UFormField>
      </div>
    </UCard>

    <!-- 版权提示 -->
    <UAlert
      color="warning"
      variant="subtle"
      icon="i-heroicons-information-circle"
      :description="t('setting.other.copyright.notice')"
    />
  </div>
</template>
