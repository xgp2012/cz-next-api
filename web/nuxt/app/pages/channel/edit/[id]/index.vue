<script setup lang="ts">
/**
 * 新建 / 编辑渠道。
 *
 * 这个页面是原实现里最复杂的表单（681 行 JSX），需要处理：
 * - 61 种渠道类型，不同类型要显示不同的额外配置（Azure / AWS / 讯飞 / 云雀 / 智谱 / FastGPT ...）
 * - 批量创建：密钥按行拆分，每行创建一个渠道
 * - 模型多选 + 自定义模型 + 模型重定向（JSON）
 * - 分组多选（来自 /api/group/）
 * - 高级配置序列化到 channel.config（JSON），与后端 model.Channel.LoadConfig 对应
 */
import type { Channel, SelectOption } from '~/utils/types'
import { CHANNEL_OPTIONS, getChannelOption } from '~/constants/channel'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { success, showError, warning } = useAppToast()

const channelId = computed(() => (route.params.id ? Number(route.params.id) : 0))
const isEdit = computed(() => channelId.value > 0)
const loading = ref(isEdit.value)
const batch = ref(false)

interface ChannelConfig {
  region: string
  sk: string
  ak: string
  user_id: string
  vertex_ai_project_id: string
  vertex_ai_region: string
  vertex_ai_adc: string
  api_version: string
  plugin: string
  library_id: string
  account_id: string
}

const config = reactive<ChannelConfig>({
  region: '',
  sk: '',
  ak: '',
  user_id: '',
  vertex_ai_project_id: '',
  vertex_ai_region: '',
  vertex_ai_adc: '',
  api_version: '',
  plugin: '',
  library_id: '',
  account_id: '',
})

/** 表单初始值：与后端 model.Channel 字段对齐 */
const inputs = reactive({
  id: 0,
  name: '',
  type: 1,
  key: '',
  base_url: '',
  other: '',
  model_mapping: '',
  system_prompt: '',
  models: [] as string[],
  groups: ['default'] as string[],
  priority: 0,
  weight: 0,
})

const originModelOptions = ref<SelectOption[]>([])
const groupOptions = ref<SelectOption[]>([])
const basicModels = ref<string[]>([])
const customModel = ref('')

const channelModels = computed(() => readChannelModelsCache())

/** 渠道类型下拉：合并 description 便于辨认 */
const typeOptions = computed(() =>
  CHANNEL_OPTIONS.map((option) => ({
    value: option.value as number,
    label: option.text as string,
    description: option.description as string | undefined,
  })),
)

const currentTip = computed(() => {
  const option = getChannelOption(inputs.type)
  return (option?.tip as string) || ''
})

/** 密钥输入框的提示语随渠道类型变化（智谱/星火/FastGPT/腾讯 的密钥格式不同） */
const keyPlaceholder = computed(() => {
  switch (inputs.type) {
    case 15:
      return t('channel.edit.key_prompts.zhipu')
    case 18:
      return t('channel.edit.key_prompts.spark')
    case 22:
      return t('channel.edit.key_prompts.fastgpt')
    case 23:
      return t('channel.edit.key_prompts.tencent')
    default:
      return t('channel.edit.key_prompts.default')
  }
})

// ---------------------------------------------------------------------------
// 初始化
// ---------------------------------------------------------------------------
onMounted(async () => {
  await Promise.all([fetchModels(), fetchGroups()])
  if (isEdit.value) {
    await loadChannel()
  } else {
    applyTypeDefaults(inputs.type)
  }
  loading.value = false
})

async function fetchModels() {
  try {
    const res = await api.get<{ id: string }[]>('/api/channel/models')
    const list = res.data || []
    originModelOptions.value = list.map((model) => ({
      key: model.id,
      text: model.id,
      value: model.id,
    }))
  } catch (error) {
    showError(error)
  }
}

async function fetchGroups() {
  try {
    const res = await api.get<string[]>('/api/group/')
    const groups = res.data || []
    // 保证 default 一定在选项里
    if (!groups.includes('default')) groups.unshift('default')
    groupOptions.value = groups.map((group) => ({ key: group, text: group, value: group }))
  } catch (error) {
    showError(error)
  }
}

/** 切换渠道类型时，若模型列表为空则用该类型的默认模型填充 */
function applyTypeDefaults(type: number) {
  const defaults = channelModels.value[String(type)] || []
  basicModels.value = defaults
  if (inputs.models.length === 0) {
    inputs.models = [...defaults]
  }
}

watch(
  () => inputs.type,
  (type) => applyTypeDefaults(type),
)

async function loadChannel() {
  try {
    const res = await api.get<Channel>(`/api/channel/${channelId.value}`)
    const data = res.data
    if (!data) return
    Object.assign(inputs, {
      id: data.id,
      name: data.name,
      type: data.type,
      key: data.key,
      base_url: data.base_url || '',
      other: data.other || '',
      model_mapping:
        data.model_mapping && data.model_mapping !== ''
          ? JSON.stringify(JSON.parse(data.model_mapping), null, 2)
          : '',
      system_prompt: data.system_prompt || '',
      models: data.models ? data.models.split(',') : [],
      groups: data.group ? data.group.split(',') : [],
      priority: data.priority ?? 0,
      weight: data.weight ?? 0,
    })
    if (data.config) {
      try {
        Object.assign(config, JSON.parse(data.config))
      } catch {
        // 历史数据里 config 可能是空字符串
      }
    }
    basicModels.value = channelModels.value[String(data.type)] || []
  } catch (error) {
    showError(error)
  }
}

// ---------------------------------------------------------------------------
// 模型操作
// ---------------------------------------------------------------------------
const modelOptions = computed<SelectOption[]>(() => {
  const seen = new Set<string>()
  const result: SelectOption[] = []
  for (const option of originModelOptions.value) {
    if (seen.has(String(option.value))) continue
    seen.add(String(option.value))
    result.push(option)
  }
  // 用户自定义的模型也要出现在下拉里
  for (const model of inputs.models) {
    if (!seen.has(model)) {
      seen.add(model)
      result.push({ key: model, text: model, value: model })
    }
  }
  return result
})

function fillRelatedModels() {
  inputs.models = [...new Set([...inputs.models, ...basicModels.value])]
}

function fillAllModels() {
  inputs.models = originModelOptions.value.map((option) => String(option.value))
}

function clearModels() {
  inputs.models = []
}

function addCustomModel() {
  const name = customModel.value.trim()
  if (!name) return
  if (!inputs.models.includes(name)) inputs.models = [...inputs.models, name]
  customModel.value = ''
}

// ---------------------------------------------------------------------------
// 提交
// ---------------------------------------------------------------------------
function buildPayload(key?: string) {
  const payload: Record<string, any> = {
    name: inputs.name,
    type: inputs.type,
    key: key ?? inputs.key,
    base_url: inputs.base_url || '',
    other: inputs.other || '',
    models: inputs.models.join(','),
    group: inputs.groups.join(','),
    model_mapping: inputs.model_mapping || '',
    system_prompt: inputs.system_prompt || '',
    priority: inputs.priority,
    weight: inputs.weight,
    config: JSON.stringify(buildConfig()),
  }
  if (inputs.id) payload.id = inputs.id
  return payload
}

/** 只提交当前渠道类型真正用到的配置项，避免无关键污染 config */
function buildConfig(): Record<string, string> {
  const result: Record<string, string> = {}
  const assign = (key: keyof ChannelConfig) => {
    const value = config[key]
    if (value) result[toSnakeCase(key)] = value
  }

  switch (inputs.type) {
    case 33: // AWS
      assign('region')
      assign('ak')
      assign('sk')
      break
    case 3: // Azure
    case 18: // 讯飞（WebSocket 版需要版本号）
    case 24: // Gemini
      assign('api_version')
      break
    case 42: // VertexAI
      assign('vertex_ai_project_id')
      assign('vertex_ai_region')
      assign('vertex_ai_adc')
      break
    case 21: // AI Proxy（知识库）
      assign('library_id')
      break
    case 17: // 阿里通义千问（插件参数）
      assign('plugin')
      break
    case 37: // Cloudflare
      assign('account_id')
      break
    case 40: // 豆包
      assign('user_id')
      break
  }
  return result
}

/** config 的 JSON 字段名与 Go 侧 channel.Config 的 tag 一致（snake_case） */
function toSnakeCase(key: string): string {
  return key.replace(/[A-Z]/g, (ch) => `_${ch.toLowerCase()}`)
}

async function submit() {
  if (!inputs.name) {
    warning(t('channel.edit.messages.name_required'))
    return
  }
  // 编辑渠道时后端不下发密钥：GetChannelById 用 Omit("key") 隐藏了 key，
  // 页面里该输入框是空的。GORM 的 Updates(struct) 会跳过零值字段，
  // 所以提交空 key 等于「保持原密钥不变」，是安全且必要的路径。
  // 只有在新建（非批量）时才必须填写。
  if (!isEdit.value && !batch.value && !inputs.key) {
    warning(t('channel.edit.messages.name_required'))
    return
  }
  if (inputs.models.length === 0) {
    warning(t('channel.edit.messages.models_required'))
    return
  }
  if (inputs.model_mapping && !verifyJSON(inputs.model_mapping)) {
    warning(t('channel.edit.messages.model_mapping_invalid'))
    return
  }
  // 模型重定向需要压缩成后端可解析的字符串
  if (inputs.model_mapping) {
    try {
      inputs.model_mapping = JSON.stringify(JSON.parse(inputs.model_mapping))
    } catch {
      warning(t('channel.edit.messages.model_mapping_invalid'))
      return
    }
  }

  loading.value = true
  try {
    if (batch.value && !isEdit.value) {
      const keys = inputs.key
        .split('\n')
        .map((k) => k.trim())
        .filter(Boolean)
      if (keys.length === 0) {
        warning(t('channel.edit.messages.name_required'))
        return
      }
      for (const key of keys) {
        await api.post('/api/channel/', buildPayload(key))
      }
      success(t('channel.edit.messages.create_success'))
    } else if (isEdit.value) {
      await api.put('/api/channel/', buildPayload())
      success(t('channel.edit.messages.update_success'))
    } else {
      await api.post('/api/channel/', buildPayload())
      success(t('channel.edit.messages.create_success'))
    }
    await router.push('/channel')
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

function cancel() {
  router.push('/channel')
}
</script>

<template>
  <div>
    <PageCard :title="isEdit ? t('channel.edit.title_edit') : t('channel.edit.title_create')">
      <UForm :state="inputs" :loading="loading" class="space-y-4" @submit="submit">
        <!-- 类型 / 名称 -->
        <div class="grid gap-4 md:grid-cols-2">
          <UFormField :label="t('channel.edit.type')" name="type">
            <USelect
              v-model="inputs.type"
              :items="typeOptions"
              class="w-full"
              :disabled="isEdit"
            />
          </UFormField>
          <UFormField :label="t('channel.edit.name')" name="name" required>
            <UInput
              v-model="inputs.name"
              :placeholder="t('channel.edit.name_placeholder')"
              class="w-full"
            />
          </UFormField>
        </div>

        <UAlert
          v-if="currentTip"
          color="info"
          variant="subtle"
          icon="i-heroicons-information-circle"
        >
          <div class="oa-prose text-sm" v-html="currentTip" />
        </UAlert>

        <!-- 分组 -->
        <UFormField :label="t('channel.edit.group')" name="groups">
          <USelectMenu
            v-model="inputs.groups"
            :items="groupOptions"
            multiple
            value-key="value"
            class="w-full"
            :placeholder="t('channel.edit.group_placeholder')"
          />
          <p class="mt-1 text-xs text-gray-500">{{ t('channel.edit.group_addition') }}</p>
        </UFormField>

        <!-- 密钥 -->
        <UFormField :label="t('channel.edit.key')" name="key" required>
          <UTextarea
            v-if="batch && !isEdit"
            v-model="inputs.key"
            :placeholder="t('channel.edit.batch_placeholder')"
            :rows="5"
            class="w-full"
          />
          <UInput
            v-else
            v-model="inputs.key"
            :placeholder="keyPlaceholder"
            autocomplete="new-password"
            class="w-full"
          />
          <UCheckbox
            v-if="!isEdit"
            v-model="batch"
            class="mt-2"
            :label="t('channel.edit.batch')"
          />
        </UFormField>

        <!-- Base URL / 代理 -->
        <UFormField :label="t('channel.edit.base_url')" name="base_url">
          <UInput
            v-model="inputs.base_url"
            :placeholder="t('channel.edit.base_url_placeholder')"
            class="w-full"
          />
        </UFormField>

        <UFormField :label="t('channel.edit.proxy_url')" name="other">
          <UInput
            v-model="inputs.other"
            :placeholder="t('channel.edit.proxy_url_placeholder')"
            class="w-full"
          />
        </UFormField>

        <!-- 渠道专属配置 -->
        <div v-if="[33, 3, 18, 24, 42, 21, 17, 37, 40].includes(inputs.type)" class="rounded-lg border border-gray-200 p-4">
          <h3 class="mb-3 text-sm font-semibold text-gray-700">
            {{ t('channel.edit.config_title') }}
          </h3>

          <!-- AWS -->
          <div v-if="inputs.type === 33" class="grid gap-3 md:grid-cols-3">
            <UFormField label="Region">
              <UInput v-model="config.region" :placeholder="t('channel.edit.aws_region_placeholder')" class="w-full" />
            </UFormField>
            <UFormField label="Access Key">
              <UInput v-model="config.ak" :placeholder="t('channel.edit.aws_ak_placeholder')" class="w-full" />
            </UFormField>
            <UFormField label="Secret Key">
              <UInput v-model="config.sk" :placeholder="t('channel.edit.aws_sk_placeholder')" class="w-full" />
            </UFormField>
          </div>

          <!-- Azure -->
          <div v-else-if="inputs.type === 3" class="grid gap-3 md:grid-cols-2">
            <UFormField label="API Version">
              <UInput
                v-model="config.api_version"
                :placeholder="t('channel.edit.azure_api_version_placeholder')"
                class="w-full"
              />
            </UFormField>
            <UFormField :label="t('channel.edit.proxy_url')">
              <UInput
                v-model="inputs.other"
                :placeholder="t('channel.edit.azure_endpoint_placeholder')"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- 讯飞 / Gemini：仅版本号 -->
          <div v-else-if="[18, 24].includes(inputs.type)">
            <UFormField :label="t('channel.edit.spark_version')">
              <UInput
                v-model="config.api_version"
                :placeholder="t('channel.edit.spark_version_placeholder')"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- VertexAI -->
          <div v-else-if="inputs.type === 42" class="space-y-3">
            <div class="grid gap-3 md:grid-cols-2">
              <UFormField :label="t('channel.edit.vertex_project_id')">
                <UInput
                  v-model="config.vertex_ai_project_id"
                  :placeholder="t('channel.edit.vertex_project_id_placeholder')"
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Region">
                <UInput
                  v-model="config.vertex_ai_region"
                  :placeholder="t('channel.edit.vertex_region_placeholder')"
                  class="w-full"
                />
              </UFormField>
            </div>
            <UFormField :label="t('channel.edit.vertex_credentials')">
              <UTextarea
                v-model="config.vertex_ai_adc"
                :placeholder="t('channel.edit.vertex_credentials_placeholder')"
                :rows="5"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- AI Proxy 知识库 -->
          <div v-else-if="inputs.type === 21">
            <UFormField :label="t('channel.edit.knowledge_id')">
              <UInput
                v-model="config.library_id"
                :placeholder="t('channel.edit.knowledge_id_placeholder')"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- 阿里通义千问插件 -->
          <div v-else-if="inputs.type === 17">
            <UFormField :label="t('channel.edit.plugin_param')">
              <UInput
                v-model="config.plugin"
                :placeholder="t('channel.edit.plugin_param_placeholder')"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- Cloudflare -->
          <div v-else-if="inputs.type === 37">
            <UFormField :label="t('channel.edit.cloudflare_account_id')">
              <UInput
                v-model="config.account_id"
                :placeholder="t('channel.edit.cloudflare_account_placeholder')"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- 豆包 -->
          <div v-else-if="inputs.type === 40">
            <UFormField :label="t('channel.edit.user_id')">
              <UInput
                v-model="config.user_id"
                :placeholder="t('channel.edit.user_id_placeholder')"
                class="w-full"
              />
            </UFormField>
          </div>
        </div>

        <!-- 模型 -->
        <UFormField :label="t('channel.edit.models')" name="models" required>
          <USelectMenu
            v-model="inputs.models"
            :items="modelOptions"
            multiple
            value-key="value"
            class="w-full"
            :placeholder="t('channel.edit.models_placeholder')"
          />
          <div class="mt-2 flex flex-wrap items-center gap-2">
            <UButton size="xs" color="neutral" variant="outline" @click="fillRelatedModels">
              {{ t('channel.edit.buttons.fill_models') }}
            </UButton>
            <UButton size="xs" color="neutral" variant="outline" @click="fillAllModels">
              {{ t('channel.edit.buttons.fill_all') }}
            </UButton>
            <UButton size="xs" color="neutral" variant="outline" @click="clearModels">
              {{ t('channel.edit.buttons.clear') }}
            </UButton>
            <UInput
              v-model="customModel"
              size="xs"
              :placeholder="t('channel.edit.buttons.custom_placeholder')"
              class="w-44"
              @keyup.enter="addCustomModel"
            />
            <UButton size="xs" color="primary" variant="soft" @click="addCustomModel">
              {{ t('channel.edit.buttons.add_custom') }}
            </UButton>
          </div>
          <p class="mt-2 text-xs text-gray-500">
            当前渠道类型默认包含 {{ basicModels.length }} 个模型
          </p>
        </UFormField>

        <!-- 模型重定向 -->
        <UFormField :label="t('channel.edit.model_mapping')" name="model_mapping">
          <UTextarea
            v-model="inputs.model_mapping"
            :placeholder="t('channel.edit.model_mapping_placeholder')"
            :rows="5"
            class="w-full font-mono text-xs"
          />
        </UFormField>

        <!-- 系统提示词 -->
        <UFormField :label="t('channel.edit.system_prompt')" name="system_prompt">
          <UTextarea
            v-model="inputs.system_prompt"
            :placeholder="t('channel.edit.system_prompt_placeholder')"
            :rows="3"
            class="w-full"
          />
        </UFormField>

        <!-- 优先级 / 权重 -->
        <div class="grid gap-4 md:grid-cols-2">
          <UFormField label="优先级" name="priority">
            <UInputNumber v-model="inputs.priority" class="w-full" />
          </UFormField>
          <UFormField label="权重">
            <UInputNumber v-model="inputs.weight" class="w-full" />
            <p class="mt-1 text-xs text-gray-500">
              注意：后端当前版本仅按「优先级」选渠道，权重字段已入库但未参与调度。
            </p>
          </UFormField>
        </div>

        <div class="flex justify-end gap-2 pt-2">
          <UButton color="neutral" variant="ghost" @click="cancel">
            {{ t('channel.edit.buttons.cancel') }}
          </UButton>
          <UButton type="submit" color="primary" :loading="loading">
            {{ t('channel.edit.buttons.submit') }}
          </UButton>
        </div>
      </UForm>
    </PageCard>
  </div>
</template>
