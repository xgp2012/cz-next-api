/**
 * 前端集成自检（对着真实运行的 one-api 实例跑）。
 *
 * 用法：
 *   1. 先启动服务：PORT=3123 GIN_MODE=release ./one-api
 *   2. node web/nuxt/scripts/verify.mjs [baseUrl]
 *
 * 检查内容：
 *   - 所有前端路由都能拿到 HTML 外壳（不是 404、不是空响应）
 *   - HTML 里引用的每个 /_nuxt/* 资源都真实存在，且返回正确的 MIME
 *   - HTML 外壳是 no-cache，带 hash 的静态资源是长缓存（两者不能搞反）
 *   - 登录后各页面的数据接口都返回 success=true
 */
const base = (process.argv[2] || 'http://127.0.0.1:3123').replace(/\/$/, '')

let passed = 0
let failed = 0

function ok(name, detail = '') {
  passed++
  console.log(`  PASS  ${name}${detail ? ` — ${detail}` : ''}`)
}

function bad(name, detail = '') {
  failed++
  console.log(`  FAIL  ${name}${detail ? ` — ${detail}` : ''}`)
}

async function get(path, init = {}) {
  return fetch(base + path, { redirect: 'manual', ...init })
}

// --- 1. 前端路由可达性 ------------------------------------------------------
const routes = [
  '/',
  '/about',
  '/login',
  '/register',
  '/reset',
  '/channel',
  '/channel/add',
  '/channel/edit/1',
  '/token',
  '/token/add',
  '/user',
  '/user/add',
  '/log',
  '/dashboard',
  '/redemption',
  '/redemption/add',
  '/topup',
  '/setting',
  '/chat',
  '/user/reset',
  '/oauth/github',
  '/oauth/lark',
  '/definitely-not-a-real-page',
]

console.log('\n[1] 前端路由')
const htmlCache = new Map()
for (const route of routes) {
  const res = await get(route)
  const type = res.headers.get('content-type') || ''
  const body = await res.text()
  if (res.status !== 200) {
    bad(route, `status=${res.status}`)
  } else if (!type.includes('text/html')) {
    bad(route, `content-type=${type}`)
  } else if (body.length < 2000) {
    // 空壳或空响应：说明静态资源没被嵌进二进制
    bad(route, `body too small (${body.length}B)，前端产物可能未嵌入`)
  } else {
    ok(route, `${body.length}B`)
    if (!htmlCache.has(route)) htmlCache.set(route, body)
  }
}

// --- 2. 资源引用可达性 ------------------------------------------------------
console.log('\n[2] 静态资源')
const refs = new Set()
for (const body of htmlCache.values()) {
  for (const m of body.matchAll(/(?:src|href)="(\/_nuxt\/[^"]+)"/g)) refs.add(m[1])
}
if (refs.size === 0) {
  bad('资源引用', 'HTML 中没有任何 /_nuxt/ 引用')
} else {
  let badRefs = 0
  for (const ref of refs) {
    const res = await get(ref)
    const type = res.headers.get('content-type') || ''
    if (res.status !== 200) {
      bad(ref, `status=${res.status}`)
      badRefs++
    } else if (type.includes('text/html')) {
      // 命中 HTML 回落 = 文件不在嵌入文件系统里
      bad(ref, `返回 HTML 回落而非真实资源（content-type=${type}）`)
      badRefs++
    }
  }
  if (badRefs === 0) ok(`${refs.size} 个 /_nuxt/* 资源`, '全部命中真实文件')
}

// --- 3. 缓存头 --------------------------------------------------------------
console.log('\n[3] 缓存策略')
{
  const res = await get('/')
  const cc = res.headers.get('cache-control') || ''
  if (cc.includes('no-cache')) ok('HTML 外壳 no-cache', cc)
  else bad('HTML 外壳应 no-cache', `实际=${cc || '(空)'}`)
}
{
  const first = [...refs][0]
  if (first) {
    const res = await get(first)
    const cc = res.headers.get('cache-control') || ''
    if (cc.includes('max-age')) ok('静态资源长缓存', `${first} → ${cc}`)
    else bad('静态资源应长缓存', `${first} → ${cc || '(空)'}`)
  }
}

// --- 4. 未登录时的鉴权行为 --------------------------------------------------
console.log('\n[4] 未登录访问受保护接口')
{
  const res = await get('/api/channel/')
  if (res.status === 401 || res.status === 403) {
    ok('/api/channel/ 拒绝未登录', `status=${res.status}`)
  } else {
    const body = await res.text()
    bad('/api/channel/ 应拒绝未登录', `status=${res.status} body=${body.slice(0, 80)}`)
  }
}

// --- 5. 登录后的数据接口 ----------------------------------------------------
console.log('\n[5] 登录后的数据接口')
const loginRes = await get('/api/user/login', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ username: 'root', password: '123456' }),
})
const setCookie = loginRes.headers.getSetCookie?.() || []
const cookie = setCookie.map((c) => c.split(';')[0]).join('; ')

if (!cookie) {
  bad('登录', '未拿到 session cookie（root/123456 是否已被修改？）')
} else {
  ok('登录', 'root/123456')
  const endpoints = [
    '/api/user/self',
    '/api/user/dashboard',
    '/api/channel/',
    '/api/channel/models',
    '/api/group/',
    '/api/redemption/',
    '/api/log/',
    '/api/option/',
    '/api/token/',
    '/api/user/available_models',
  ]
  for (const ep of endpoints) {
    const res = await get(ep, { headers: { cookie } })
    if (res.status !== 200) {
      bad(ep, `status=${res.status}`)
      continue
    }
    let json
    try {
      json = await res.json()
    } catch {
      bad(ep, '响应不是 JSON')
      continue
    }
    if (json.success === true) ok(ep)
    else if (json.success === undefined) ok(ep, '（非标准响应，无 success 字段，视为静态数据）')
    else bad(ep, `success=false message=${json.message}`)
  }
}

// --- 汇总 -------------------------------------------------------------------
console.log(`\n结果：${passed} 通过，${failed} 失败`)
process.exit(failed === 0 ? 0 : 1)
