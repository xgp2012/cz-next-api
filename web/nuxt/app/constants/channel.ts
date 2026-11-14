import type { SelectOption } from '~/utils/types'

/**
 * 渠道类型枚举的中文展示名 + 徽章色 + 提示。
 * 与 Go 侧 relay/channeltype/define.go 的编号严格一一对应。
 * ⚠️ 新增渠道类型时，这里必须同步增加，否则后台下拉框选不到。
 *
 * color 使用 @nuxt/ui 的语义色名，渲染时映射到 Badge color。
 */
export const CHANNEL_OPTIONS: SelectOption[] = [
  { key: 1, text: 'OpenAI', value: 1, color: 'success' },
  {
    key: 50,
    text: 'OpenAI 兼容',
    value: 50,
    color: 'success',
    description: 'OpenAI 兼容渠道，支持设置 Base URL',
  },
  { key: 14, text: 'Anthropic', value: 14, color: 'neutral' },
  { key: 33, text: 'AWS', value: 33, color: 'neutral' },
  { key: 3, text: 'Azure', value: 3, color: 'info' },
  { key: 11, text: 'PaLM2', value: 11, color: 'warning' },
  { key: 24, text: 'Gemini', value: 24, color: 'warning' },
  {
    key: 51,
    text: 'Gemini (OpenAI)',
    value: 51,
    color: 'warning',
    description: 'Gemini OpenAI 兼容格式',
  },
  { key: 28, text: 'Mistral AI', value: 28, color: 'warning' },
  { key: 41, text: 'Novita', value: 41, color: 'secondary' },
  {
    key: 40,
    text: '字节火山引擎',
    value: 40,
    color: 'info',
    description: '原字节跳动豆包',
  },
  {
    key: 15,
    text: '百度文心千帆',
    value: 15,
    color: 'info',
    tip: '请前往<a href="https://console.bce.baidu.com/qianfan/ais/console/applicationConsole/application/v1" target="_blank">此处</a>获取 AK（API Key）以及 SK（Secret Key），注意，V2 版本接口请使用 <strong>百度文心千帆 V2 </strong>渠道类型',
  },
  {
    key: 47,
    text: '百度文心千帆 V2',
    value: 47,
    color: 'info',
    tip: '请前往<a href="https://console.bce.baidu.com/iam/#/iam/apikey/list" target="_blank">此处</a>获取 API Key，注意本渠道仅支持<a target="_blank" href="https://cloud.baidu.com/doc/WENXINWORKSHOP/s/em4tsqo3v">推理服务 V2</a>相关模型',
  },
  {
    key: 17,
    text: '阿里通义千问',
    value: 17,
    color: 'warning',
    tip: '如需使用阿里云百炼，请使用<strong>阿里云百炼</strong>渠道',
  },
  { key: 49, text: '阿里云百炼', value: 49, color: 'warning' },
  {
    key: 18,
    text: '讯飞星火认知',
    value: 18,
    color: 'info',
    tip: '本渠道基于讯飞 WebSocket 版本 API，如需 HTTP 版本，请使用<strong>讯飞星火认知 V2</strong>渠道',
  },
  {
    key: 48,
    text: '讯飞星火认知 V2',
    value: 48,
    color: 'info',
    tip: 'HTTP 版本的讯飞接口，前往<a href="https://console.xfyun.cn/services/cbm" target="_blank">此处</a>获取 HTTP 服务接口认证密钥',
  },
  { key: 16, text: '智谱 ChatGLM', value: 16, color: 'secondary' },
  { key: 19, text: '360 智脑', value: 19, color: 'info' },
  { key: 25, text: 'Moonshot AI', value: 25, color: 'neutral' },
  { key: 23, text: '腾讯混元', value: 23, color: 'info' },
  { key: 26, text: '百川大模型', value: 26, color: 'warning' },
  { key: 27, text: 'MiniMax', value: 27, color: 'error' },
  { key: 29, text: 'Groq', value: 29, color: 'warning' },
  { key: 30, text: 'Ollama', value: 30, color: 'neutral' },
  { key: 31, text: '零一万物', value: 31, color: 'success' },
  { key: 32, text: '阶跃星辰', value: 32, color: 'info' },
  { key: 34, text: 'Coze', value: 34, color: 'info' },
  { key: 35, text: 'Cohere', value: 35, color: 'info' },
  { key: 36, text: 'DeepSeek', value: 36, color: 'neutral' },
  { key: 37, text: 'Cloudflare', value: 37, color: 'warning' },
  { key: 38, text: 'DeepL', value: 38, color: 'neutral' },
  { key: 39, text: 'together.ai', value: 39, color: 'info' },
  { key: 42, text: 'VertexAI', value: 42, color: 'info' },
  { key: 43, text: 'Proxy', value: 43, color: 'info' },
  { key: 44, text: 'SiliconFlow', value: 44, color: 'info' },
  { key: 45, text: 'xAI', value: 45, color: 'info' },
  { key: 46, text: 'Replicate', value: 46, color: 'info' },
  {
    key: 8,
    text: '自定义渠道',
    value: 8,
    color: 'error',
    tip: '不推荐使用，请使用 <strong>OpenAI 兼容</strong>渠道类型。注意，这里所需要填入的代理地址仅会在实际请求时替换域名部分，如果你想填入 OpenAI SDK 中所要求的 Base URL，请使用 OpenAI 兼容渠道类型',
    description: '不推荐使用，请使用 OpenAI 兼容渠道类型',
  },
  { key: 22, text: '知识库：FastGPT', value: 22, color: 'info' },
  { key: 21, text: '知识库：AI Proxy', value: 21, color: 'secondary' },
  { key: 20, text: 'OpenRouter', value: 20, color: 'neutral' },
  { key: 2, text: '代理：API2D', value: 2, color: 'info' },
  { key: 5, text: '代理：OpenAI-SB', value: 5, color: 'warning' },
  { key: 7, text: '代理：OhMyGPT', value: 7, color: 'secondary' },
  { key: 10, text: '代理：AI Proxy', value: 10, color: 'secondary' },
  { key: 4, text: '代理：CloseAI', value: 4, color: 'info' },
  { key: 6, text: '代理：OpenAI Max', value: 6, color: 'secondary' },
  { key: 9, text: '代理：AI.LS', value: 9, color: 'warning' },
  { key: 12, text: '代理：API2GPT', value: 12, color: 'info' },
  { key: 13, text: '代理：AIGC2D', value: 13, color: 'secondary' },
]

/** channelType -> 选项的索引，避免每次渲染都线性查找 */
export const CHANNEL_MAP: Record<number, SelectOption> = CHANNEL_OPTIONS.reduce(
  (acc, option) => {
    acc[option.value as number] = option
    return acc
  },
  {} as Record<number, SelectOption>,
)

export function getChannelOption(type: number): SelectOption | undefined {
  return CHANNEL_MAP[type]
}

/** 分页常量：必须与后端 constant 保持一致（controller 里按 10 条分页） */
export const ITEMS_PER_PAGE = 10
