/**
 * 统一 HTTP 层。
 *
 * 后端契约（不可更改的部分）：
 * 1. 控制面 /api/* 的成功/失败几乎都用 HTTP 200 + { success: boolean, message, data } 表达，
 *    只有鉴权/限流/panic 才会返回 401/403/429/500。所以业务判断必须看 success 字段。
 * 2. 登录态是 Cookie Session（gin-contrib/sessions），依赖浏览器自动带 cookie，
 *    因此前端不需要也不应该设置 Authorization 头。
 * 3. 请求地址极多以 `/` 结尾（如 /api/token/），这是 gin 的 group 重定向习惯，
 *    保持原样可以避免 307 往返。
 */
import type { ApiResponse } from '~/utils/types'

export interface RequestOptions {
  /** 查询参数，undefined/null/'' 会被自动丢弃 */
  query?: Record<string, string | number | boolean | undefined | null>
  /** 请求体，会被 JSON 序列化 */
  body?: any
  /** 为 true 时不弹出全局错误提示（调用方自行处理） */
  silent?: boolean
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
}

export class ApiError extends Error {
  statusCode: number
  constructor(message: string, statusCode: number) {
    super(message)
    this.name = 'ApiError'
    this.statusCode = statusCode
  }
}

function buildQuery(query?: RequestOptions['query']): string {
  if (!query) return ''
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) continue
    params.append(key, String(value))
  }
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

/**
 * 底层请求函数。
 *
 * SSR 期间不发起任何用户态请求：那会在构建机的 node 进程里执行，
 * 既拿不到用户的 Cookie，也会把构建期的网络错误写进预渲染 HTML。
 * 调用方应当把用户态数据放到 onMounted / useAsyncData(clientOnly) 里。
 */
export async function request<T = any>(
  path: string,
  options: RequestOptions = {},
): Promise<ApiResponse<T>> {
  const { query, body, method = 'GET' } = options
  const url = path + buildQuery(query)

  if (import.meta.server) {
    // 预渲染阶段：直接返回失败占位，由客户端挂载后重新拉取
    return { success: false, message: 'server-side request skipped', data: undefined }
  }

  let res: Response
  try {
    res = await $fetch.raw(url, {
      method,
      body: body === undefined ? undefined : body,
      credentials: 'include',
      headers: {
        Accept: 'application/json',
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      // 让所有 HTTP 状态码都走同一条处理路径，便于统一包装
      ignoreResponseError: true,
    } as any)
  } catch (error: any) {
    throw new ApiError(error?.message || '网络请求失败', 0)
  }

  const status = res.status

  // 非 200：后端只在异常路径使用（401 过期 / 403 无权限 / 429 限流 / 500）
  if (status !== 200) {
    let message = ''
    try {
      const data = (await res.json()) as ApiResponse
      message = data?.message || ''
    } catch {
      // 响应体不是 JSON，忽略
    }
    if (status === 401) {
      // 与旧前端保持一致：跳登录页并带 expired 标记
      if (!options.silent) {
        await navigateTo('/login?expired=true')
      }
      throw new ApiError(message || '未登录或登录已过期', status)
    }
    const fallback
      = status === 429
        ? '请求次数过多，请稍后再试！'
        : status === 500
          ? '服务器内部错误，请联系管理员！'
          : message || `请求失败（HTTP ${status}）`
    throw new ApiError(fallback, status)
  }

  let payload: ApiResponse<T>
  try {
    payload = (await res.json()) as ApiResponse<T>
  } catch {
    throw new ApiError('响应不是合法的 JSON', status)
  }

  // 业务层失败（success=false）：这是后端最常见的错误表达方式
  if (!payload || payload.success !== true) {
    throw new ApiError(payload?.message || '操作失败', status)
  }

  return payload
}

/** 语法糖：GET / POST / PUT / DELETE */
export const api = {
  get: <T = any>(path: string, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'GET' }),

  post: <T = any>(path: string, body?: any, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'POST', body }),

  put: <T = any>(path: string, body?: any, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'PUT', body }),

  delete: <T = any>(path: string, body?: any, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'DELETE', body }),
}
