<script setup lang="ts">
/**
 * 运营设置（root）。
 * 覆盖 quota/ratio/log/monitor/general 五组配置，全部走 Option 接口。
 */
const { t } = useI18n()
// 日志清理需要独立的提示与错误反馈（不走 useOptions 的 saving 流程）
const { success, showError } = useAppToast()
const {
  optionMap,
  loading,
  saving,
  loadOptions,
  getString,
  getBool,
  getNumber,
  updateOption,
  updateOptions,
} = useOptions()

onMounted(loadOptions)

/**
 * Option 值是异步加载的，加载完成后统一灌入各表单。
 * 用 watch 而不是在模板里调用同步函数，避免每次渲染都重置用户正在编辑的输入。
 */
watch(optionMap, () => {
  syncQuota()
  syncRatio()
  syncMonitor()
  syncGeneral()
})

// ---------------------------------------------------------------------------
// 额度
// ---------------------------------------------------------------------------
const quota = reactive({
  newUser: 0,
  preConsume: 0,
  inviterReward: 0,
  inviteeReward: 0,
})

function syncQuota() {
  quota.newUser = getNumber('QuotaForNewUser')
  quota.preConsume = getNumber('PreConsumedQuota')
  quota.inviterReward = getNumber('QuotaForInviter')
  quota.inviteeReward = getNumber('QuotaForInvitee')
}

// ---------------------------------------------------------------------------
// 倍率（JSON 文本）
// ---------------------------------------------------------------------------
const ratio = reactive({ model: '', completion: '', group: '' })

function syncRatio() {
  ratio.model = prettyJSON(getString('ModelRatio'))
  ratio.completion = prettyJSON(getString('CompletionRatio'))
  ratio.group = prettyJSON(getString('GroupRatio'))
}

/** 后端存的是紧凑 JSON，这里格式化便于编辑；非法 JSON 时原样显示 */
function prettyJSON(raw: string): string {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

async function saveRatio() {
  const checks: Array<[string, string, string]> = [
    ['ModelRatio', ratio.model, t('setting.operation.ratio.model_invalid')],
    ['CompletionRatio', ratio.completion, t('setting.operation.ratio.completion_invalid')],
    ['GroupRatio', ratio.group, t('setting.operation.ratio.group_invalid')],
  ]
  for (const [key, value, message] of checks) {
    if (value && !verifyJSON(value)) {
      showError(message)
      return
    }
    void key
  }
  await updateOptions([
    ['ModelRatio', compactJSON(ratio.model)],
    ['CompletionRatio', compactJSON(ratio.completion)],
    ['GroupRatio', compactJSON(ratio.group)],
  ])
}

function compactJSON(raw: string): string {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw))
  } catch {
    return raw
  }
}

// ---------------------------------------------------------------------------
// 监控
// ---------------------------------------------------------------------------
const monitor = reactive({ threshold: 0, remind: 0, autoDisable: false, autoEnable: false })

function syncMonitor() {
  monitor.threshold = getNumber('ChannelDisableThreshold')
  monitor.remind = getNumber('QuotaRemindThreshold')
  monitor.autoDisable = getBool('AutomaticDisableChannelEnabled')
  monitor.autoEnable = getBool('AutomaticEnableChannelEnabled')
}

// ---------------------------------------------------------------------------
// 通用
// ---------------------------------------------------------------------------
const general = reactive({
  topupLink: '',
  chatLink: '',
  quotaPerUnit: 500000,
  retryTimes: 0,
  displayInCurrency: false,
  displayTokenStat: false,
  approximateToken: false,
})

function syncGeneral() {
  general.topupLink = getString('TopUpLink')
  general.chatLink = getString('ChatLink')
  general.quotaPerUnit = getNumber('QuotaPerUnit', 500000)
  general.retryTimes = getNumber('RetryTimes')
  general.displayInCurrency = getBool('DisplayInCurrencyEnabled')
  general.displayTokenStat = getBool('DisplayTokenStatEnabled')
  general.approximateToken = getBool('ApproximateTokenEnabled')
}

const logConsumeEnabled = computed({
  get: () => getBool('LogConsumeEnabled'),
  set: (value: boolean) => updateOption('LogConsumeEnabled', value),
})

// ---------------------------------------------------------------------------
// 日志清理
// ---------------------------------------------------------------------------
const cleanTarget = ref('')

async function cleanLogs() {
  if (!cleanTarget.value) {
    showError('请选择要清理的目标时间')
    return
  }
  try {
    const timestamp = string2timestamp(cleanTarget.value)
    const res = await api.delete<number>('/api/log/', {
      query: { target_timestamp: timestamp },
    })
    success(t('setting.operation.log.cleaned', { count: res.data ?? 0 }))
  } catch (error) {
    showError(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <!-- 加载完成后把 Option 值灌进各表单 -->
    <div v-if="loading" class="py-8 text-center text-sm text-gray-400">{{ t('common.loading') }}</div>
    <template v-else>
      <!-- 额度设置 -->
      <UCard>
        <template #header>
          <div class="font-semibold">{{ t('setting.operation.quota.title') }}</div>
        </template>
        <div class="grid gap-4 md:grid-cols-2">
          <UFormField :label="t('setting.operation.quota.new_user')">
            <UInputNumber v-model="quota.newUser" class="w-full" />
          </UFormField>
          <UFormField :label="t('setting.operation.quota.pre_consume')">
            <UInputNumber v-model="quota.preConsume" class="w-full" />
            <p class="mt-1 text-xs text-gray-500">
              {{ t('setting.operation.quota.pre_consume_placeholder') }}
            </p>
          </UFormField>
          <UFormField :label="t('setting.operation.quota.inviter_reward')">
            <UInputNumber v-model="quota.inviterReward" class="w-full" />
          </UFormField>
          <UFormField :label="t('setting.operation.quota.invitee_reward')">
            <UInputNumber v-model="quota.inviteeReward" class="w-full" />
          </UFormField>
        </div>
        <UButton
          class="mt-4"
          color="primary"
          :loading="saving"
          @click="
            updateOptions([
              ['QuotaForNewUser', quota.newUser],
              ['PreConsumedQuota', quota.preConsume],
              ['QuotaForInviter', quota.inviterReward],
              ['QuotaForInvitee', quota.inviteeReward],
            ])
          "
        >
          {{ t('setting.operation.quota.buttons.save') }}
        </UButton>
      </UCard>

      <!-- 倍率设置 -->
      <UCard>
        <template #header>
          <div class="font-semibold">{{ t('setting.operation.ratio.title') }}</div>
        </template>
        <div class="space-y-4">
          <UFormField :label="t('setting.operation.ratio.model.title')">
            <UTextarea
              v-model="ratio.model"
              :rows="8"
              :placeholder="t('setting.operation.ratio.model.placeholder')"
              class="w-full font-mono text-xs"
            />
          </UFormField>
          <UFormField :label="t('setting.operation.ratio.completion.title')">
            <UTextarea
              v-model="ratio.completion"
              :rows="6"
              :placeholder="t('setting.operation.ratio.completion.placeholder')"
              class="w-full font-mono text-xs"
            />
          </UFormField>
          <UFormField :label="t('setting.operation.ratio.group.title')">
            <UTextarea
              v-model="ratio.group"
              :rows="5"
              :placeholder="t('setting.operation.ratio.group.placeholder')"
              class="w-full font-mono text-xs"
            />
          </UFormField>
        </div>
        <UButton class="mt-4" color="primary" :loading="saving" @click="saveRatio">
          {{ t('setting.operation.ratio.buttons.save') }}
        </UButton>
      </UCard>

      <!-- 日志设置 -->
      <UCard>
        <template #header>
          <div class="font-semibold">{{ t('setting.operation.log.title') }}</div>
        </template>
        <UCheckbox v-model="logConsumeEnabled" :label="t('setting.operation.log.enable_consume')" />
        <div class="mt-4 flex flex-wrap items-end gap-2">
          <UFormField :label="t('setting.operation.log.target_time')">
            <UInput v-model="cleanTarget" type="datetime-local" class="w-56" />
          </UFormField>
          <UButton color="error" variant="soft" @click="cleanLogs">
            {{ t('setting.operation.log.buttons.clean') }}
          </UButton>
        </div>
      </UCard>

      <!-- 监控设置 -->
      <UCard>
        <template #header>
          <div class="font-semibold">{{ t('setting.operation.monitor.title') }}</div>
        </template>
        <div class="grid gap-4 md:grid-cols-2">
          <UFormField :label="t('setting.operation.monitor.max_response_time')">
            <UInputNumber v-model="monitor.threshold" class="w-full" />
            <p class="mt-1 text-xs text-gray-500">
              {{ t('setting.operation.monitor.max_response_time_placeholder') }}
            </p>
          </UFormField>
          <UFormField :label="t('setting.operation.monitor.quota_reminder')">
            <UInputNumber v-model="monitor.remind" class="w-full" />
            <p class="mt-1 text-xs text-gray-500">
              {{ t('setting.operation.monitor.quota_reminder_placeholder') }}
            </p>
          </UFormField>
        </div>
        <div class="mt-3 flex flex-wrap gap-4">
          <UCheckbox v-model="monitor.autoDisable" :label="t('setting.operation.monitor.auto_disable')" />
          <UCheckbox v-model="monitor.autoEnable" :label="t('setting.operation.monitor.auto_enable')" />
        </div>
        <UButton
          class="mt-4"
          color="primary"
          :loading="saving"
          @click="
            updateOptions([
              ['ChannelDisableThreshold', monitor.threshold],
              ['QuotaRemindThreshold', monitor.remind],
              ['AutomaticDisableChannelEnabled', monitor.autoDisable],
              ['AutomaticEnableChannelEnabled', monitor.autoEnable],
            ])
          "
        >
          {{ t('setting.operation.monitor.buttons.save') }}
        </UButton>
      </UCard>

      <!-- 通用设置 -->
      <UCard>
        <template #header>
          <div class="font-semibold">{{ t('setting.operation.general.title') }}</div>
        </template>
        <div class="grid gap-4 md:grid-cols-2">
          <UFormField :label="t('setting.operation.general.topup_link')">
            <UInput
              v-model="general.topupLink"
              :placeholder="t('setting.operation.general.topup_link_placeholder')"
              class="w-full"
            />
          </UFormField>
          <UFormField :label="t('setting.operation.general.chat_link')">
            <UInput
              v-model="general.chatLink"
              :placeholder="t('setting.operation.general.chat_link_placeholder')"
              class="w-full"
            />
          </UFormField>
          <UFormField :label="t('setting.operation.general.quota_per_unit')">
            <UInputNumber v-model="general.quotaPerUnit" class="w-full" />
            <p class="mt-1 text-xs text-gray-500">
              {{ t('setting.operation.general.quota_per_unit_placeholder') }}
            </p>
          </UFormField>
          <UFormField :label="t('setting.operation.general.retry_times')">
            <UInputNumber v-model="general.retryTimes" class="w-full" />
          </UFormField>
        </div>
        <div class="mt-3 flex flex-col gap-2">
          <UCheckbox
            v-model="general.displayInCurrency"
            :label="t('setting.operation.general.display_in_currency')"
          />
          <UCheckbox
            v-model="general.displayTokenStat"
            :label="t('setting.operation.general.display_token_stat')"
          />
          <UCheckbox
            v-model="general.approximateToken"
            :label="t('setting.operation.general.approximate_token')"
          />
        </div>
        <UButton
          class="mt-4"
          color="primary"
          :loading="saving"
          @click="
            updateOptions([
              ['TopUpLink', general.topupLink],
              ['ChatLink', general.chatLink],
              ['QuotaPerUnit', general.quotaPerUnit],
              ['RetryTimes', general.retryTimes],
              ['DisplayInCurrencyEnabled', general.displayInCurrency],
              ['DisplayTokenStatEnabled', general.displayTokenStat],
              ['ApproximateTokenEnabled', general.approximateToken],
            ])
          "
        >
          {{ t('setting.operation.general.buttons.save') }}
        </UButton>
      </UCard>
    </template>
  </div>
</template>
