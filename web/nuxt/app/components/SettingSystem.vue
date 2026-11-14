<script setup lang="ts">
/**
 * 系统设置（root）：服务器地址、登录注册开关、邮箱域名白名单、SMTP、
 * GitHub / 飞书 / 微信 / Turnstile 配置。
 *
 * 注意 SMTPToken、GitHubClientSecret 等敏感项后端不会下发（GetOptions 会过滤
 * 以 Token / Secret 结尾的键），所以这些输入框留空即表示「不修改」。
 */
const { t } = useI18n()
const { saving, optionMap, loadOptions, getString, getBool, getNumber, updateOption, updateOptions }
  = useOptions()

onMounted(loadOptions)

const general = reactive({ serverAddress: '' })

const login = reactive({
  passwordLogin: true,
  passwordRegister: true,
  emailVerification: false,
  githubOAuth: false,
  wechatLogin: false,
  registration: true,
  turnstile: false,
  oidc: false,
})

const emailDomain = reactive({ enabled: false, domains: [] as string[] })
const newDomain = ref('')

const smtp = reactive({ server: '', port: 587, account: '', from: '', token: '' })
const github = reactive({ clientId: '', clientSecret: '' })
const lark = reactive({ clientId: '', clientSecret: '' })
const wechat = reactive({ serverAddress: '', token: '', qrcode: '' })
const turnstile = reactive({ siteKey: '', secretKey: '' })

// Option 是异步加载的，加载完成后把值灌入各表单
watch(optionMap, () => {
    general.serverAddress = getString('ServerAddress')
    login.passwordLogin = getBool('PasswordLoginEnabled', true)
    login.passwordRegister = getBool('PasswordRegisterEnabled', true)
    login.emailVerification = getBool('EmailVerificationEnabled')
    login.githubOAuth = getBool('GitHubOAuthEnabled')
    login.wechatLogin = getBool('WeChatAuthEnabled')
    login.registration = getBool('RegisterEnabled', true)
    login.turnstile = getBool('TurnstileCheckEnabled')
    login.oidc = getBool('OidcEnabled')

    emailDomain.enabled = getBool('EmailDomainRestrictionEnabled')
    emailDomain.domains = getString('EmailDomainWhitelist')
      .split(',')
      .filter(Boolean)

    smtp.server = getString('SMTPServer')
    smtp.port = getNumber('SMTPPort', 587)
    smtp.account = getString('SMTPAccount')
    smtp.from = getString('SMTPFrom')

    github.clientId = getString('GitHubClientId')
    lark.clientId = getString('LarkClientId')
    wechat.serverAddress = getString('WeChatServerAddress')
    wechat.qrcode = getString('WeChatAccountQRCodeImageURL')
    turnstile.siteKey = getString('TurnstileSiteKey')
})

/**
 * 开关类配置直接落库（与原实现一致：切换即保存）。
 * 后端会对 GitHub OAuth / 微信登录 / Turnstile / 邮箱限制做前置校验，
 * 失败时 useOptions 会自动回滚开关状态。
 */
const loginToggles: Array<{ key: string, label: string, model: 'passwordLogin' | 'passwordRegister' | 'emailVerification' | 'githubOAuth' | 'wechatLogin' | 'registration' | 'turnstile' | 'oidc' }> = [
  { key: 'PasswordLoginEnabled', label: 'setting.system.login.password_login', model: 'passwordLogin' },
  { key: 'PasswordRegisterEnabled', label: 'setting.system.login.password_register', model: 'passwordRegister' },
  { key: 'EmailVerificationEnabled', label: 'setting.system.login.email_verification', model: 'emailVerification' },
  { key: 'GitHubOAuthEnabled', label: 'setting.system.login.github_oauth', model: 'githubOAuth' },
  { key: 'WeChatAuthEnabled', label: 'setting.system.login.wechat_login', model: 'wechatLogin' },
  { key: 'RegisterEnabled', label: 'setting.system.login.registration', model: 'registration' },
  { key: 'TurnstileCheckEnabled', label: 'setting.system.login.turnstile', model: 'turnstile' },
  { key: 'OidcEnabled', label: 'setting.system.login.oidc', model: 'oidc' },
]

async function onToggle(key: string, model: keyof typeof login) {
  await updateOption(key, login[model] as boolean)
}

function addDomain() {
  const domain = newDomain.value.trim()
  if (!domain) return
  if (!emailDomain.domains.includes(domain)) emailDomain.domains = [...emailDomain.domains, domain]
  newDomain.value = ''
}

function removeDomain(domain: string) {
  emailDomain.domains = emailDomain.domains.filter((item) => item !== domain)
}

/** OAuth 回调地址提示需要用到服务器地址 */
const callbackBase = computed(() => general.serverAddress || window.location.origin)
</script>

<template>
  <div class="space-y-6">
    <!-- 服务器地址 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.system.general.title') }}</div>
      </template>
      <UFormField :label="t('setting.system.general.server_address')">
        <UInput
          v-model="general.serverAddress"
          :placeholder="t('setting.system.general.server_address_placeholder')"
          class="w-full"
        />
      </UFormField>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="updateOption('ServerAddress', general.serverAddress)"
      >
        {{ t('setting.system.general.buttons.update') }}
      </UButton>
    </UCard>

    <!-- 登录注册 -->
    <UCard>
      <template #header>
        <div class="font-semibold">{{ t('setting.system.login.title') }}</div>
      </template>
      <div class="flex flex-col gap-3">
        <UCheckbox
          v-for="toggle in loginToggles"
          :key="toggle.key"
          v-model="login[toggle.model]"
          :label="t(toggle.label)"
          @update:model-value="onToggle(toggle.key, toggle.model)"
        />
      </div>
    </UCard>

    <!-- 邮箱域名白名单 -->
    <UCard>
      <template #header>
        <div>
          <div class="font-semibold">{{ t('setting.system.email_restriction.title') }}</div>
          <div class="text-xs text-gray-500">{{ t('setting.system.email_restriction.subtitle') }}</div>
        </div>
      </template>
      <UCheckbox v-model="emailDomain.enabled" :label="t('setting.system.email_restriction.enable')" />
      <div class="mt-4">
        <div class="mb-2 text-sm text-gray-600">
          {{ t('setting.system.email_restriction.allowed_domains') }}
        </div>
        <div class="mb-2 flex flex-wrap gap-2">
          <UBadge
            v-for="domain in emailDomain.domains"
            :key="domain"
            color="neutral"
            variant="subtle"
            class="cursor-pointer"
            @click="removeDomain(domain)"
          >
            {{ domain }} ✕
          </UBadge>
          <span v-if="emailDomain.domains.length === 0" class="text-sm text-gray-400">
            {{ t('common.none') }}
          </span>
        </div>
        <div class="flex flex-wrap gap-2">
          <UInput
            v-model="newDomain"
            :placeholder="t('setting.system.email_restriction.add_domain_placeholder')"
            class="w-64"
            @keyup.enter="addDomain"
          />
          <UButton color="neutral" variant="outline" @click="addDomain">
            {{ t('setting.system.email_restriction.buttons.fill') }}
          </UButton>
        </div>
      </div>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="
          updateOptions([
            ['EmailDomainRestrictionEnabled', emailDomain.enabled],
            ['EmailDomainWhitelist', emailDomain.domains.join(',')],
          ])
        "
      >
        {{ t('setting.system.email_restriction.buttons.save') }}
      </UButton>
    </UCard>

    <!-- SMTP -->
    <UCard>
      <template #header>
        <div>
          <div class="font-semibold">{{ t('setting.system.smtp.title') }}</div>
          <div class="text-xs text-gray-500">{{ t('setting.system.smtp.subtitle') }}</div>
        </div>
      </template>
      <div class="grid gap-4 md:grid-cols-2">
        <UFormField :label="t('setting.system.smtp.server')">
          <UInput
            v-model="smtp.server"
            :placeholder="t('setting.system.smtp.server_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.smtp.port')">
          <UInputNumber v-model="smtp.port" class="w-full" />
        </UFormField>
        <UFormField :label="t('setting.system.smtp.account')">
          <UInput
            v-model="smtp.account"
            :placeholder="t('setting.system.smtp.account_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.smtp.from')">
          <UInput
            v-model="smtp.from"
            :placeholder="t('setting.system.smtp.from_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.smtp.token')">
          <UInput
            v-model="smtp.token"
            type="password"
            :placeholder="t('setting.system.smtp.token_placeholder')"
            class="w-full"
          />
        </UFormField>
      </div>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="
          updateOptions([
            ['SMTPServer', smtp.server],
            ['SMTPPort', smtp.port],
            ['SMTPAccount', smtp.account],
            ['SMTPFrom', smtp.from],
            ['SMTPToken', smtp.token],
          ])
        "
      >
        {{ t('setting.system.smtp.buttons.save') }}
      </UButton>
    </UCard>

    <!-- GitHub OAuth -->
    <UCard>
      <template #header>
        <div>
          <div class="font-semibold">{{ t('setting.system.github.title') }}</div>
          <div class="text-xs text-gray-500">{{ t('setting.system.github.subtitle') }}</div>
        </div>
      </template>
      <UAlert
        color="info"
        variant="subtle"
        icon="i-heroicons-information-circle"
        class="mb-4"
      >
        <template #description>
          <a
            href="https://github.com/settings/applications/new"
            target="_blank"
            rel="noopener"
            class="text-[var(--oa-brand)] hover:underline"
          >{{ t('setting.system.github.manage_link') }}</a>
          {{ t('setting.system.github.manage_text') }}
          <div class="mt-1 text-xs">
            {{
              t('setting.system.github.url_notice', {
                server_url: callbackBase,
                callback_url: `${callbackBase}/oauth/github`,
              })
            }}
          </div>
        </template>
      </UAlert>
      <div class="grid gap-4 md:grid-cols-2">
        <UFormField :label="t('setting.system.github.client_id')">
          <UInput
            v-model="github.clientId"
            :placeholder="t('setting.system.github.client_id_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.github.client_secret')">
          <UInput
            v-model="github.clientSecret"
            type="password"
            :placeholder="t('setting.system.github.client_secret_placeholder')"
            class="w-full"
          />
        </UFormField>
      </div>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="
          updateOptions([
            ['GitHubClientId', github.clientId],
            ['GitHubClientSecret', github.clientSecret],
          ])
        "
      >
        {{ t('setting.system.github.buttons.save') }}
      </UButton>
    </UCard>

    <!-- 飞书 OAuth -->
    <UCard>
      <template #header>
        <div>
          <div class="font-semibold">{{ t('setting.system.lark.title') }}</div>
          <div class="text-xs text-gray-500">{{ t('setting.system.lark.subtitle') }}</div>
        </div>
      </template>
      <UAlert color="info" variant="subtle" icon="i-heroicons-information-circle" class="mb-4">
        <template #description>
          <a
            href="https://open.feishu.cn/app"
            target="_blank"
            rel="noopener"
            class="text-[var(--oa-brand)] hover:underline"
          >{{ t('setting.system.lark.manage_link') }}</a>
          {{ t('setting.system.lark.manage_text') }}
          <div class="mt-1 text-xs">
            {{
              t('setting.system.lark.url_notice', {
                server_url: callbackBase,
                callback_url: `${callbackBase}/oauth/lark`,
              })
            }}
          </div>
        </template>
      </UAlert>
      <div class="grid gap-4 md:grid-cols-2">
        <UFormField :label="t('setting.system.lark.client_id')">
          <UInput
            v-model="lark.clientId"
            :placeholder="t('setting.system.lark.client_id_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.lark.client_secret')">
          <UInput
            v-model="lark.clientSecret"
            type="password"
            :placeholder="t('setting.system.lark.client_secret_placeholder')"
            class="w-full"
          />
        </UFormField>
      </div>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="
          updateOptions([
            ['LarkClientId', lark.clientId],
            ['LarkClientSecret', lark.clientSecret],
          ])
        "
      >
        {{ t('setting.system.lark.buttons.save') }}
      </UButton>
    </UCard>

    <!-- WeChat Server -->
    <UCard>
      <template #header>
        <div>
          <div class="font-semibold">{{ t('setting.system.wechat.title') }}</div>
          <div class="text-xs text-gray-500">{{ t('setting.system.wechat.subtitle') }}</div>
        </div>
      </template>
      <div class="grid gap-4 md:grid-cols-2">
        <UFormField :label="t('setting.system.wechat.server_address')">
          <UInput
            v-model="wechat.serverAddress"
            :placeholder="t('setting.system.wechat.server_address_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.wechat.token')">
          <UInput
            v-model="wechat.token"
            type="password"
            :placeholder="t('setting.system.wechat.token_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.wechat.qrcode')">
          <UInput
            v-model="wechat.qrcode"
            :placeholder="t('setting.system.wechat.qrcode_placeholder')"
            class="w-full"
          />
        </UFormField>
      </div>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="
          updateOptions([
            ['WeChatServerAddress', wechat.serverAddress],
            ['WeChatServerToken', wechat.token],
            ['WeChatAccountQRCodeImageURL', wechat.qrcode],
          ])
        "
      >
        {{ t('setting.system.wechat.buttons.save') }}
      </UButton>
    </UCard>

    <!-- Turnstile -->
    <UCard>
      <template #header>
        <div>
          <div class="font-semibold">{{ t('setting.system.turnstile.title') }}</div>
          <div class="text-xs text-gray-500">{{ t('setting.system.turnstile.subtitle') }}</div>
        </div>
      </template>
      <UAlert color="info" variant="subtle" icon="i-heroicons-information-circle" class="mb-4">
        <template #description>
          <a
            href="https://dash.cloudflare.com/"
            target="_blank"
            rel="noopener"
            class="text-[var(--oa-brand)] hover:underline"
          >{{ t('setting.system.turnstile.manage_link') }}</a>
          {{ t('setting.system.turnstile.manage_text') }}
        </template>
      </UAlert>
      <div class="grid gap-4 md:grid-cols-2">
        <UFormField :label="t('setting.system.turnstile.site_key')">
          <UInput
            v-model="turnstile.siteKey"
            :placeholder="t('setting.system.turnstile.site_key_placeholder')"
            class="w-full"
          />
        </UFormField>
        <UFormField :label="t('setting.system.turnstile.secret_key')">
          <UInput
            v-model="turnstile.secretKey"
            type="password"
            :placeholder="t('setting.system.turnstile.secret_key_placeholder')"
            class="w-full"
          />
        </UFormField>
      </div>
      <UButton
        class="mt-4"
        color="primary"
        :loading="saving"
        @click="
          updateOptions([
            ['TurnstileSiteKey', turnstile.siteKey],
            ['TurnstileSecretKey', turnstile.secretKey],
          ])
        "
      >
        {{ t('setting.system.turnstile.buttons.save') }}
      </UButton>
    </UCard>
  </div>
</template>
