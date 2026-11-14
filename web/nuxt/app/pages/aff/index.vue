<script setup lang="ts">
/**
 * 推广中心（邀请分成）。
 *
 * 与旧版「一次性邀请奖励」的差异（model/aff.go 的注释写得很清楚）：
 * 这里是**持续分成** —— 被邀请人每次消费都会按比例产生一笔佣金流水，
 * 累加到 pending 池，邀请人再一键「转正」到钱包余额。
 *
 * 端点：
 *   GET  /api/user/aff               → { aff_code, aff_link, invite_count,
 *                                        pending_quota, history_quota,
 *                                        commission_rate, min_transfer, enabled }
 *   GET  /api/user/aff/commissions?p=→ 佣金流水（游标分页，与其它列表一致）
 *   GET  /api/user/aff/invitees      → 我邀请的用户及其累计贡献
 *   POST /api/user/aff_transfer      → 把 pending 全部转入钱包，返回本次转出额度
 *
 * 注意 GET /api/user/aff 与 GET /api/user/aff/ 都能命中后端（gin 的
 * group 重定向），这里按产品约定用不带尾斜杠的写法。
 */
import type { AffCommission, AffInfo, AffInvitee } from '~/utils/types'
import { ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { quotaConfig, quotaPerUnit, restoreUser } = useUserStore()
const { success, showError, warning } = useAppToast()

const loading = ref(false)
const transferring = ref(false)
const info = ref<AffInfo | null>(null)

/**
 * 转正门槛。
 * 后端默认是「1 美元等值额度」（model.MinAffTransferQuota），接口会下发
 * min_transfer；接口未下发时按 quota_per_unit 兜底，与后端语义保持一致。
 */
const minTransfer = computed(() => {
  const fromServer = Number(info.value?.min_transfer ?? 0)
  if (fromServer > 0) return fromServer
  return Number(quotaPerUnit.value || 500000) || 500000
})

const pendingQuota = computed(() => Number(info.value?.pending_quota ?? 0))
const historyQuota = computed(() => Number(info.value?.history_quota ?? 0))
const canTransfer = computed(() => pendingQuota.value > 0 && pendingQuota.value >= minTransfer.value)

/** 距离可转正还差多少 */
const transferGap = computed(() => Math.max(0, minTransfer.value - pendingQuota.value))

async function loadInfo() {
  loading.value = true
  try {
    const res = await api.get<AffInfo>('/api/user/aff')
    info.value = res.data || null
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

const {
  rows: commissions,
  loading: commissionsLoading,
  activePage,
  totalPages,
  loadPage,
  refresh: refreshCommissions,
  onPageChange,
} = usePagedTable({
  fetcher: async (startIdx) => {
    const res = await api.get<AffCommission[]>('/api/user/aff/commissions', { query: { p: startIdx } })
    return res.data || []
  },
})

onMounted(async () => {
  await loadInfo()
  await loadPage(0)
  await loadInvitees()
})

/** 我邀请的用户及其贡献（独立加载，失败不阻塞主流程） */
const invitees = ref<AffInvitee[]>([])
async function loadInvitees() {
  try {
    const res = await api.get<AffInvitee[]>('/api/user/aff/invitees')
    invitees.value = res.data || []
  } catch {
    invitees.value = []
  }
}

const visibleCommissions = computed(() => {
  const start = (activePage.value - 1) * ITEMS_PER_PAGE
  return commissions.value.slice(start, start + ITEMS_PER_PAGE)
})

/** 按贡献佣金从高到低排序：让"谁带来的价值最大"一眼可见 */
const sortedInvitees = computed(() =>
  [...invitees.value].sort((a, b) => (b.commission_quota ?? 0) - (a.commission_quota ?? 0)),
)

/** 被邀请人展示名：后端可能给用户名，缺失时回退成 id */
function inviteeName(item: AffCommission): string {
  return item.invitee_name || item.invitee_username || `用户 #${item.invitee_id}`
}

function commissionStatus(item: AffCommission) {
  return affCommissionStatus(item.status)
}

/** 来源类型展示：消费分成 / 充值分成 */
function sourceLabel(item: AffCommission): string {
  if (item.source_type === 'topup') return '充值分成'
  return '消费分成'
}

async function copyLink() {
  const link = info.value?.aff_link
  if (!link) return
  const ok = await copyText(link)
  if (ok) success(t('aff.messages.copied', '邀请链接已复制到剪贴板'))
  else showError('复制失败，请检查浏览器剪贴板权限')
}

async function transfer() {
  if (!canTransfer.value) {
    warning(
      pendingQuota.value <= 0
        ? t('aff.messages.nothing_to_transfer', '暂无可结算的推广收益')
        : `推广收益需满 ${renderQuota(minTransfer.value, quotaConfig.value)} 才能转出`,
    )
    return
  }
  transferring.value = true
  try {
    const res = await api.post<number>('/api/user/aff_transfer')
    const transferred = res.data ?? 0
    success(`${t('aff.messages.transfer_success', '推广收益已转入钱包')} +${renderQuota(transferred, quotaConfig.value)}`)
    // 余额变了，同步顶栏与钱包页显示
    await restoreUser()
    await loadInfo()
    await refreshCommissions()
  } catch (error) {
    showError(error)
  } finally {
    transferring.value = false
  }
}

const rateLabel = computed(() => renderRate(info.value?.commission_rate))
</script>

<template>
  <div class="space-y-4">
    <!-- ------------------------------------------------------------------ -->
    <!-- 概览 + 邀请链接                                                     -->
    <!-- ------------------------------------------------------------------ -->
    <PageCard :title="t('aff.title', '推广中心')" :subtitle="t('aff.subtitle', '邀请好友注册，好友每次消费你都能获得持续分成')">
      <template #actions>
        <UButton color="neutral" variant="outline" icon="i-heroicons-arrow-path" :loading="loading" @click="loadInfo">
          {{ t('common.refresh', '刷新') }}
        </UButton>
      </template>

      <UAlert
        v-if="info && info.enabled === false"
        class="mb-4"
        color="warning"
        variant="subtle"
        icon="i-heroicons-information-circle"
        title="分成功能未开启"
        description="当前站点未启用消费分成，邀请人数仍会统计，但不会产生佣金流水。"
      />

      <div class="grid gap-4 md:grid-cols-3">
        <UCard>
          <div class="text-sm text-gray-500">{{ t('aff.cards.pending', '待结算收益') }}</div>
          <div class="mt-1 text-2xl font-semibold">{{ renderQuota(pendingQuota, quotaConfig) }}</div>
          <p class="mt-1 text-xs text-gray-400">
            满 {{ renderQuota(minTransfer, quotaConfig) }} 可转出
            <span v-if="transferGap > 0">，还差 {{ renderQuota(transferGap, quotaConfig) }}</span>
          </p>
        </UCard>
        <UCard>
          <div class="text-sm text-gray-500">{{ t('aff.cards.history', '历史累计收益') }}</div>
          <div class="mt-1 text-2xl font-semibold">{{ renderQuota(historyQuota, quotaConfig) }}</div>
          <p class="mt-1 text-xs text-gray-400">历史累计只增不减，已含已转出的部分</p>
        </UCard>
        <UCard>
          <div class="text-sm text-gray-500">{{ t('aff.cards.invites', '成功邀请') }}</div>
          <div class="mt-1 text-2xl font-semibold">{{ renderNumber(info?.invite_count ?? 0) }} 人</div>
          <p class="mt-1 text-xs text-gray-400">当前分成比例：{{ rateLabel }}</p>
        </UCard>
      </div>

      <div class="mt-4 space-y-3">
        <UFormField :label="t('aff.link.label', '我的邀请链接')" help="好友通过该链接注册后会自动绑定邀请关系">
          <div class="flex flex-wrap gap-2">
            <UInput :model-value="info?.aff_link || ''" readonly class="min-w-64 flex-1" />
            <UButton color="primary" icon="i-heroicons-clipboard" :disabled="!info?.aff_link" @click="copyLink">
              {{ t('aff.link.copy', '复制链接') }}
            </UButton>
          </div>
        </UFormField>
        <div class="flex flex-wrap items-center gap-2 text-xs text-gray-500">
          <span>邀请码：<code>{{ info?.aff_code || '—' }}</code></span>
          <UButton
            v-if="info?.aff_code"
            size="xs"
            color="neutral"
            variant="ghost"
            icon="i-heroicons-document-duplicate"
            @click="copyText(info?.aff_code || '')"
          >
            复制邀请码
          </UButton>
        </div>

        <UButton
          color="primary"
          icon="i-heroicons-banknotes"
          :loading="transferring"
          :disabled="!canTransfer"
          @click="transfer"
        >
          {{ t('aff.buttons.transfer', '转出到钱包余额') }}
        </UButton>
        <p v-if="!canTransfer" class="text-xs text-gray-400">
          {{ pendingQuota <= 0 ? '暂无可结算的推广收益。' : `待结算收益需满 ${renderQuota(minTransfer, quotaConfig)} 才能转出。` }}
        </p>
      </div>
    </PageCard>

    <!-- ------------------------------------------------------------------ -->
    <!-- 佣金流水                                                            -->
    <!-- ------------------------------------------------------------------ -->
    <PageCard :title="t('aff.commissions.title', '佣金流水')" :subtitle="t('aff.commissions.subtitle', '每一笔都对应被邀请人的一次真实消费，可追溯')">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">被邀请人</th>
              <th class="p-2">来源</th>
              <th class="p-2">消费额度</th>
              <th class="oa-hide-on-mobile p-2">分成比例</th>
              <th class="p-2">佣金</th>
              <th class="p-2">状态</th>
              <th class="oa-hide-on-mobile p-2">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in visibleCommissions" :key="item.id" class="border-b border-gray-100 hover:bg-gray-50">
              <td class="p-2">
                <div class="font-medium">{{ inviteeName(item) }}</div>
                <div class="text-xs text-gray-400">ID {{ item.invitee_id }}</div>
              </td>
              <td class="p-2">
                <UBadge
                  :color="item.source_type === 'topup' ? 'info' : 'primary'"
                  variant="subtle"
                  size="sm"
                >
                  {{ sourceLabel(item) }}
                </UBadge>
              </td>
              <td class="p-2 whitespace-nowrap">{{ renderQuota(item.source_quota, quotaConfig) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ renderRate(item.rate) }}</td>
              <td class="p-2 whitespace-nowrap font-medium">
                {{ renderQuota(item.commission_quota, quotaConfig) }}
              </td>
              <td class="p-2">
                <StatusBadge :color="commissionStatus(item).color" :label="commissionStatus(item).label" />
              </td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ timestamp2string(item.created_time) }}</td>
            </tr>
            <tr v-if="visibleCommissions.length === 0">
              <td colspan="7" class="p-8 text-center text-gray-400">
                {{ commissionsLoading ? t('common.loading', '加载中...') : t('common.none', '无') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="mt-4 flex flex-wrap items-center justify-between gap-2">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          :loading="commissionsLoading"
          @click="refreshCommissions"
        >
          {{ t('common.refresh', '刷新') }}
        </UButton>
        <UPagination
          :page="activePage"
          :total="totalPages * ITEMS_PER_PAGE"
          :items-per-page="ITEMS_PER_PAGE"
          size="sm"
          @update:page="onPageChange"
        />
      </div>
    </PageCard>

    <!-- 我邀请的用户：只看总数无法判断哪个渠道来的用户质量高 -->
    <PageCard
      :title="t('aff.invitees.title', '我邀请的用户')"
      :subtitle="t('aff.invitees.subtitle', '按贡献的佣金排序，帮你判断哪些渠道带来的用户更活跃')"
    >
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">用户</th>
              <th class="p-2">累计消费</th>
              <th class="p-2">贡献佣金</th>
              <th class="oa-hide-on-mobile p-2">笔数</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in sortedInvitees"
              :key="row.id"
              class="border-b border-gray-100 hover:bg-gray-50"
            >
              <td class="p-2 whitespace-nowrap">
                <span class="font-medium">{{ row.username || `用户 #${row.id}` }}</span>
                <span class="ml-2 text-xs text-gray-400">ID {{ row.id }}</span>
              </td>
              <td class="p-2 whitespace-nowrap text-gray-600">
                {{ renderQuota(row.quota ?? 0, quotaConfig) }}
              </td>
              <td class="p-2 whitespace-nowrap font-medium text-green-600">
                {{ renderQuota(row.commission_quota ?? 0, quotaConfig) }}
              </td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ row.commission_count ?? 0 }}</td>
            </tr>
            <tr v-if="sortedInvitees.length === 0">
              <td colspan="4" class="p-8 text-center text-gray-400">
                {{ t('aff.invitees.empty', '还没有邀请到用户，分享你的推广链接试试') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </PageCard>
  </div>
</template>
