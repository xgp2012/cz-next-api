# One API 商业化能力（Commercial）

本文档说明本项目在原有「聚合网关 + 额度计费」基础上新增的商业化子系统：
在线充值、套餐订阅、定价规则引擎、邀请分成、营收看板。

设计原则只有一条：**未配置任何商业项时，行为与改造前完全一致。**
没有支付网关就没有充值入口，没有套餐就没有订阅页，没有规则就按原有倍率计费。
因此升级不需要数据迁移，也不会改变任何既有账目。

---

## 0. 与 New API 的关系

参考了 [New API](https://github.com/QuantumNous/new-api)（One API 的主流商业二开分支）的能力清单，
但刻意在四个地方做得不一样——这四点也是本项目的差异化所在：

| 能力 | New API | 本项目 |
|---|---|---|
| 定价表达 | 9 组互斥 map（ModelRatio/ModelPrice/CacheRatio…），无法表达阶梯与分时 | **有序规则链 + 显式 tier**，支持按量/按次/阶梯/缓存/分时 |
| 订阅续费 | 明确**不支持**自动续费、无按比例退款 | **自动续费 + 宽限期 + 升级折算** |
| 邀请返利 | 一次性 pending 池，官方承认「不是分成」 | **持续分成**（按消费比例计提），有独立流水表可审计 |
| 金额口径 | 各网关对 Amount 解释不一致（Creem 直接当 quota） | **单一口径**：只有 `AmountCents → Quota` 一条入账路径 |

同时对 New API 的已知缺陷做了针对性加固：

- **回调幂等**：单事务 + `SELECT ... FOR UPDATE` + 状态机 + **条件 UPDATE**（`WHERE quota <= Max - ?`），
  并发重放不会重复入账，也不会越过钱包上限。
- **网关隔离**：A 网关的回调无法核销 B 网关的订单（`ErrTopUpProviderMismatch`）。
- **分组变更不踢会话**：升级订阅只改 `users.group`，不触碰会话凭据。
- **密钥不入库**：支付密钥只走环境变量，避免被 `/api/option` 明文回显。

---

## 1. 定价规则引擎

### 1.1 为什么不用倍率表

原有倍率体系（`ModelRatio` × `GroupRatio` × `CompletionRatio`）只能表达
「每 1000 tokens 线性单价 × 分组折扣」。真实商业场景还需要：

- **阶梯价**：前 100 万 tokens 按 A 价，超出部分按 B 价（大客户谈判常见）
- **按次价**：图片生成、语音合成这类与 token 无关的调用
- **缓存价**：Claude / GPT 的 prompt cache 命中与写入应当显著便宜
- **分时价**：夜间闲时折扣

这些用「9 组 map」表达不出来，所以引入了 `PricingRule`。

### 1.2 规则结构

```go
type PricingRule struct {
    // 匹配条件（留空 = 不限制）
    Model       string  // 支持 "gpt-4*" 通配
    Group       string  // 支持 "vip,svip" 多分组
    ChannelType int     // -1 或 0 = 任意渠道类型
    TimeWindows string  // JSON: [{"start_hour":22,"end_hour":6}]，支持跨零点

    // 计价
    BillingMode       string  // per_token | per_call | tiered_by_tokens
    InputRatio        float64 // 输入倍率
    OutputRatio       float64 // 输出倍率
    CacheHitRatio     float64 // 缓存命中价
    CacheWriteRatio   float64 // 缓存写入价
    PerCallPrice      float64 // 按次单价（美元）
    Tiers             string  // JSON: [{"tier":1,"up_to_tokens":1000000,...}]

    // 调度
    Priority int // 越大越优先
    Order    int // 同优先级内序号
}
```

### 1.3 匹配与计价流程

```
ResolvePrice(model, group, channelType)
        │
        ├── 命中 PricingRule（按 priority desc, order asc 取第一条）
        │       └── PriceUsage() → per_token / per_call / tiered
        │
        └── 未命中 → 回落旧倍率（ModelRatio × GroupRatio × CompletionRatio）
```

**关键保证**：结算用的是**预扣时算出的同一份价格快照**（`CachedPrice`）。
请求进行中管理员改价，不会导致同一次请求的预扣与结算对不上账。

### 1.4 阶梯计费的档位语义

`UpToTokens` 是该档的**累计上界（含）**，最后一档必须为 `0`（无上界）：

```json
[
  {"tier": 1, "up_to_tokens": 1000000, "input_ratio": 2.5, "output_ratio": 10},
  {"tier": 2, "up_to_tokens": 0,       "input_ratio": 2.0, "output_ratio": 8}
]
```

表示「用量 ≤100 万 tokens 时整体按 2.5/10 计价，超过则整体按 2.0/8」。
这是**整档计价**而非分段累加，便于与上游结算对齐。

### 1.5 操作入口

- 页面：`/pricing/manage`（管理员）— 表单化编辑 + 实时价格预览
- 接口：`GET|POST|PUT|DELETE /api/pricing_rules/`
- 价格墙：`/pricing`（公开），展示规则与未命中规则时使用的旧倍率

---

## 2. 在线充值（钱包）

### 2.1 金额口径（重要）

所有金额走**整数分**，杜绝浮点误差：

| 字段 | 含义 | 说明 |
|---|---|---|
| `AmountCents` | 用户购买的美元金额 | 100 = $1.00 |
| `MoneyCents` | 实际支付金额 | 可能因活动/折扣 ≠ Amount，仅用于对账展示 |
| `Quota` | 入账额度 | **服务端唯一计算**：`AmountCents/100 × QuotaPerUnit` |

入账只认 `Quota`，不存在按网关分叉的第二条路径。

### 2.2 订单状态机

```
          ┌─────────┐
  下单 ──►│ pending │──回调成功──► success（终态，重复回调幂等返回 success）
          └────┬────┘
               ├──超时──► expired
               └──失败──► failed
```

### 2.3 幂等与并发安全

`CompleteTopUp` 是整个系统最危险的函数，设计如下：

1. 单事务内 `SELECT ... FOR UPDATE` 锁定订单行（跨节点安全，不依赖进程内锁或 Redis）。
2. 状态已是 `success` → 返回 `alreadyDone`，调用方视为成功（网关停止重试）。
3. 状态非 `pending` → `ErrTopUpStatusInvalid`。
4. 加额度用条件 UPDATE：
   ```sql
   UPDATE users SET quota = quota + ? WHERE id = ? AND quota <= ? -- MaxWalletQuota - 本单额度
   ```
   两个并发回调不可能同时绕过上限；`RowsAffected == 0` 时区分「用户不存在」与「超限」。
5. 日志与缓存刷新在事务**提交后**执行，不影响事务原子性。

### 2.4 支付网关

**EPay 协议**（覆盖支付宝 / 微信 / 银行卡聚合通道，也可对接自建网关）：

| 环境变量 | 说明 |
|---|---|
| `PAY_ADDRESS` | 网关地址，下单跳到 `{PAY_ADDRESS}/submit.php` |
| `EPAY_ID` | 商户 PID |
| `EPAY_KEY` | 商户密钥（只参与签名，绝不写日志/返前端） |
| `PAY_METHODS` | JSON 数组：`[{"name":"支付宝","type":"alipay","color":"#1677ff","min_topup":10}]` |
| `AMOUNT_OPTIONS` | 快捷金额：`[10,20,50,100,200,500]` |
| `MIN_TOPUP` | 全局最小单笔（美元） |
| `MAX_WALLET_QUOTA` | 单用户钱包上限，0 = 不限 |
| `CUSTOM_CALLBACK_ADDRESS` | 覆盖回调基地址（反代/CDN 场景） |
| `TOPUP_ORDER_EXPIRE_MINUTES` | 订单有效期，默认 30 |

签名规则：参与字段按 ASCII 字典序拼 `k=v&k=v...`，末尾直接拼密钥，MD5 取小写十六进制。

**通用 Webhook 网关**（自建支付/海外网关）：
`POST /api/pay/notify`，头 `X-Pay-Signature` = `HMAC-SHA256(rawBody, WEBHOOK_SECRET)`，
用 `hmac.Equal` 常量时间比较；`WEBHOOK_SECRET` 为空时**整体拒绝**（不给空密钥留后门）。

### 2.5 接口

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/api/user/topup/info` | User | 可用支付方式、快捷金额、是否开启 |
| POST | `/api/user/amount` | User | 试算应付金额与到账额度 |
| POST | `/api/user/pay` | User | 创建订单，返回网关提交参数 |
| GET/POST | `/api/user/epay/notify` | 签名 | EPay 回调，返回纯文本 `success`/`fail` |
| POST | `/api/pay/notify` | 签名 | 通用 webhook 回调 |
| GET | `/api/user/topup/self` | User | 自己的订单（游标分页） |
| GET | `/api/user/topup` | Admin | 全部订单（可按 provider/status 过滤） |
| POST | `/api/user/topup/complete` | Admin | 手工结算（对账补偿） |

> 回调接口**不加**会话鉴权：调用方是网关而非登录用户，安全性完全由签名保证。

---

## 3. 套餐订阅

### 3.1 补齐 New API 的三项缺失

**① 自动续费**
订阅到期前从钱包余额扣费续期。扣费失败 → `renew_attempts++` → 进入宽限期 → 宽限结束仍未续上则到期。
多节点同时跑扫描也安全：靠条件 UPDATE，只有一个节点 `RowsAffected > 0`。

**② 宽限期**
`GracePeriodDays` 内额度仍可用、仍可补缴，避免服务瞬间中断（对已付费用户尤其重要）。

**③ 升级折算**
`Proration` 按剩余天数折算差价，从钱包结算。

### 3.2 数据模型

```go
SubscriptionPlan   // 套餐定义：价格、周期、总额度、重置周期、授予分组
UserSubscription   // 用户持有的一个周期实例：已用额度、起止、宽限、分组快照
```

周期用**日历加法**（`AddDate`）而非固定秒数，1 个月不会因大小月漂移。
额度重置支持 `never / daily / weekly / monthly`，重置点对齐到当天 00:00 / 周一 / 每月 1 日。

### 3.3 额度扣减顺序

`ConsumeSubscription` 按 `end_time` **升序**扣减 —— 先过期的先用，不浪费即将失效的额度。
`AmountTotal == 0` 视为不限量（仍计入 `used` 用于统计）。

### 3.4 计费偏好（每用户 4 档）

| 偏好 | 行为 |
|---|---|
| `subscription_first` | 先扣订阅，用尽后按 `AllowWalletOverflow` 决定是否溢出到钱包 |
| `wallet_first` | 先扣钱包 |
| `subscription_only` | 只扣订阅，用尽即拒绝 |
| `wallet_only` | 只扣钱包，订阅额度不参与 |

### 3.5 分组回落

订阅授予的分组在到期时回落到 `DowngradeGroup`（为空则 `PrevUserGroup`）。
**安全保护**：只有当用户当前分组仍等于本订阅授予的分组时才回落，
避免覆盖管理员手工调整；若用户还有其他生效订阅则不回落。

### 3.6 接口

| 方法 | 路径 | 鉴权 |
|---|---|---|
| GET | `/api/subscription/plans` | 公开 |
| GET | `/api/subscription/self` | User |
| POST | `/api/subscription/balance/pay` | User |
| PUT | `/api/subscription/self/preference` | User |
| PUT | `/api/subscription/self/auto_renew` | User |
| GET/POST/PUT/DELETE | `/api/subscription/plans/admin/` | Admin |
| POST | `/api/subscription/invalidate` | Admin |

---

## 4. 邀请分成

### 4.1 模式

被邀请人**每次消费**后，按 `AffCommissionRate`（默认 5%）给邀请人计提佣金：

```
被邀请人消费 100000 额度
        │
        ├──► AffCommission 流水（rate=0.05, commission=5000, status=pending）
        │
        └──► UserAffStats.pending_quota += 5000, history_quota += 5000
                    │
                    └── 邀请人点「转正」→ 转入钱包余额（最低 1 美元）
```

比例在**计提时快照**（`Rate` 字段），之后改配置不影响历史流水，可审计。

### 4.2 为什么用独立流水表

`UserAffStats` 只提供聚合读数（待结算/累计/邀请人数），真正的凭据是 `AffCommission` 每一行。
这样「这笔佣金是哪次消费来的」始终可追溯——这是 New API 的 pending 池做不到的。

### 4.3 配置

| 环境变量 | 说明 |
|---|---|
| `AFF_COMMISSION_ENABLED` | 总开关，默认关闭 |
| `AFF_COMMISSION_RATE` | 默认比例，0.05 = 5% |
| `AFF_COMMISSION_MIN_QUOTA` | 单笔低于该额度不计佣金，过滤碎流水 |
| `AFF_MIN_TRANSFER_QUOTA` | 转正门槛（单位：额度），`0` = 回落为 1 美元等值额度 |

`AFF_MIN_TRANSFER_QUOTA` 之所以可配置：固定 1 美元对小额分成很不友好。
按 5% 分成的站点，被邀请人要先消费满 20 美元才能攒够 1 美元佣金，
邀请人可能几个月看不到钱到账，直接打击推广积极性。不同客单价应各自设定。
配置为 `0` 时回落到默认值——**不是**把门槛变成 0，否则任何人都能把 1 额度转出去。

### 4.4 接口

| 方法 | 路径 | 鉴权 |
|---|---|---|
| GET | `/api/user/aff` | User — 邀请码、链接、邀请人数、待结算/累计 |
| GET | `/api/user/aff/commissions` | User — 佣金流水（分页） |
| GET | `/api/user/aff/invitees` | User — 我邀请的用户及其累计消费/贡献佣金 |
| POST | `/api/user/aff_transfer` | User — 待结算转钱包 |

`/invitees` 只回 `id` / `username` / `quota` / `commission_quota` / `commission_count`，
不含邮箱等联系方式——邀请关系不足以成为查看他人隐私的授权。
它解决的是「只看一个总数无法判断哪个渠道的用户质量高」这个问题。

---

## 5. 营收看板

`GET /api/admin/revenue?days=7`（`days` 取值 1–90，超出范围直接报错而非静默修正，
免得运营以为看到的是 90 天数据）

口径（集中说明，避免误读）：

- **收入** = `top_ups` 中 `status=success` 的订单，按 `complete_time` 归日，单位**分**。
  注意订阅订单的 `Quota` 为 0（它是"买来的额度"而非"入账额度"），
  所以收入统一用 `MoneyCents` 换算，而不是 `Quota`。
- **消耗** = `logs` 中 `type=2`（消费）的行，按 `created_at` 归日，单位**额度**。
- **毛利** = `(收入 - 消耗折钱) / 收入`。这是**账面口径**：用户充了钱没消费的部分
  仍算作已确认收入，所以新站点毛利会虚高，需结合「未消耗余额」一起看。

字段命名约定：金额一律 `_cents`（整数分），消耗一律 `_quota`（额度），
比例一律 0–1 小数。**不存在** `total_income_quota` 这种混口径字段——
早期控制器里曾声明过它而 model 返回的是 `total_income_cents`，
两处对不上会让前端拿到空值，现已统一为 struct 单一来源。

返回字段：`days`、`total_income_cents`、`total_consumed_quota`、
`total_consumed_cents`、`gross_margin_cents`、`gross_margin`、`arpu_cents`、
`paying_users`、`consuming_users`、`active_subscriptions`、
`auto_renew_subscriptions`、`renew_rate`、`daily[]`、`top_models[]`、`top_users[]`、`note`。

日期按天铺满（无数据的日期也返回 0），避免前端折线图断档。
`daily[].income_cents` 是分，前端画图前会折算成额度，否则两条线不可比。

---

## 6. 后台任务

`monitor.StartSubscriptionWorker()`，仅主节点启动（`NODE_TYPE != slave`）：

| 间隔 | 动作 |
|---|---|
| 1 分钟 | 重置到期额度周期 → 处理到期订阅（续费 / 宽限 / 回落分组） |
| 5 分钟 | 超时未支付订单置为 `expired` |

启动后延迟 30 秒再开始，避免与数据库迁移抢锁；每轮处理量上限 100，防止扫全表拖库。

---

## 7. 安全清单

- [x] 支付密钥只走环境变量，不入 `options` 表（该表会被 `/api/option` 读取）
- [x] 回调必须验签；webhook 密钥为空时整体拒绝
- [x] 订单号字符集限制 `[0-9A-Za-z]`，防止注入与网关兼容问题
- [x] 回调幂等 + 行锁 + 条件 UPDATE，防重放与并发双充
- [x] 网关隔离：A 网关回调不能核销 B 网关订单
- [x] 钱包上限由 SQL 条件保证，非应用层判断
- [x] 自动续费/改偏好均校验订阅归属，不能操作他人订阅
- [x] 佣金比例快照落库，改配置不篡改历史
- [x] 佣金只按**真实消费**计提，注册与充值不产生佣金（防小号套利）
- [x] 自己邀请自己不计佣金
- [x] 密钥不写日志：回调日志只记 `trade_no` 与 `trade_status`

---

## 8. 测试

```bash
go test ./model/ ./controller/ ./router/ ./common/random/
```

| 文件 | 覆盖 |
|---|---|
| `model/pricing_test.go` | 按量/按次/阶梯/缓存计费、规则匹配、时间窗、校验、旧倍率回落 |
| `model/topup_test.go` | 下单校验、**重复回调不重复入账**、网关不匹配、钱包上限、超时过期 |
| `model/subscription_test.go` | 周期计算、余额购买、跨多订阅扣减、到期/续费/退款 |
| `model/aff_test.go` | 只按消费计提、**比例快照不可篡改**、自邀拒绝、门槛边界、转正原子性 |
| `model/revenue_test.go` | 收入用分而非额度、订阅订单 Quota=0 仍计收入、日期无断档、除零、状态过滤 |
| `controller/payment_test.go` | EPay 签名向量、篡改检测、验签 |
| `router/commercial_test.go` | 路由注册完整性、**支付回调免鉴权**、受保护路由拒绝匿名 |

`router` 测试起了一个内存 SQLite 并照搬 `main.go` 的两处全局装配
（session 中间件、关闭 Redis），否则会在 `sessions.Default` / `rdb.LLen` 上
因为环境缺失而 panic，把真正的回归信号淹掉。

测试用 `github.com/glebarez/sqlite`（纯 Go，无 CGO），因此 `go test` 在没装 gcc 的机器上也能跑。

> 已知例外：`common/image` 的 `TestDecode` 需要联网下载 Wikimedia 图片，
> 离线环境必然失败，与本子系统无关，未做修改。

---

## 9. 前端

| 路由 | 页面 | 可见性 |
|---|---|---|
| `/pricing` | 模型价格墙 | 公开（无需登录，已加入 `PUBLIC_ROUTES`） |
| `/wallet` | 充值中心 | 登录 |
| `/subscription` | 套餐订阅 | 登录 |
| `/aff` | 推广中心 | 登录 |
| `/revenue` | 营收看板 | 管理员 |
| `/pricing/manage` | 定价规则管理 | 管理员 |

所有取数都在 `onMounted`，不使用 SSR；分页沿用 `usePagedTable` 游标式
（`?p=0/1/2`，`ITEMS_PER_PAGE=10`）；未引入图表库，营收折线图用内联 SVG
（两条序列共用同一 max 轴，否则收入与消耗的相对高低会失真）。

---

## 10. 端到端验证（真机跑过，不是只编译）

单测覆盖不到「启动能不能跑起来」，因此这套流程用真实进程 + 真实 HTTP 验证过。
下面是可复现的最小验证步骤，改动后建议照跑一遍。

### 10.1 关键前提：无 gcc 也能启动

原实现用 `mattn/go-sqlite3`，它是 CGO 驱动。`CGO_ENABLED=0` 时它能**编译通过**，
但只是一个桩，要等 `main()` 里真的 Open 数据库才报：

```
Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub
```

然后 `FatalLog` 退出。也就是说「编译成功」≠「能启动」，而 Windows 默认安装、
精简容器镜像都没有 gcc——本地开发与很多部署会直接卡在这里，且报错信息对使用者毫无指向性。

现在按构建条件自动切驱动（`model/sqlite_cgo.go` / `model/sqlite_purego.go`）：

| 构建条件 | 驱动 | 说明 |
|---|---|---|
| `CGO_ENABLED=1` | `mattn/go-sqlite3` | 默认，性能更好 |
| `CGO_ENABLED=0` | `glebarez/sqlite`（纯 Go） | 无需 gcc，开箱即用 |

```bash
CGO_ENABLED=0 go build -o one-api .
./one-api        # 无需 gcc 即可启动
```

### 10.2 冒烟脚本

```bash
# 启动
PORT=53180 SQLITE_PATH=./e2e.db SESSION_SECRET=e2e \
AFF_COMMISSION_ENABLED=true AFF_COMMISSION_RATE=0.10 \
CGO_ENABLED=0 ./one-api
```

按顺序验证（每一步都有可观察的断言）：

| # | 操作 | 期望 |
|---|---|---|
| 1 | `GET /api/pricing` | 200，含 `rules` / `legacy_ratios` / `group_ratios` |
| 2 | 登录 `root/123456` | 200，`success:true` |
| 3 | `POST /api/pricing_rules/` 建一条 gpt-4o 规则（1.5 / 6.0） | 落库，`status:1` |
| 4 | `PUT /api/channel/` 指向 mock 上游 | 200 |
| 5 | `GET /api/user/aff` | **对象**，含 `aff_code`（非空） |
| 6 | 用该 `aff_code` 注册新用户 | `success:true` |
| 7 | 新用户发一次 `/v1/chat/completions`（mock 返回 1000 prompt + 500 completion） | 200 |
| 8 | 扣费 | **4500** = (1000×1.5 + 500×6.0) × 0.002/1000 × 500000 |
| 9 | 邀请人 `pending_quota` | **450** = 4500 × 10% |
| 10 | 消费日志 `content` | `按量计费：输入 1.5000 输出 6.0000 …`，**不是**旧倍率 `2.50` |
| 11 | `POST /api/user/aff_transfer` | 达门槛则到账，`pending` 归零、`history` 保留 |

第 8、10 步是这套验证的核心：**它同时证明了规则引擎真的接管了计费**。
只断言「请求成功」是不够的——旧倍率路径同样返回 200，但扣的是 7500 而不是 4500，
且日志会写着 `倍率：2.50 × 1.00 × 4.00`。两种结果在 HTTP 层完全一样，只有对账才能发现。

### 10.3 验证过程中发现并修掉的真实缺陷

| 缺陷 | 后果 | 修法 |
|---|---|---|
| 无 gcc 时默认 SQLite 驱动只是桩 | 编译通过但启动即退出 | 按 CGO 条件切换驱动 |
| `GET /api/user/aff` 被旧 `GetAffCode` 顶替 | 前端拿到裸字符串，推广页空白 | 删除被顶替的路由，加重复路由检测测试 |
| `GetAffInfo` 不生成 `aff_code` | 新用户推广链接是 `...?aff=`，邀请功能静默失效 | 在 `GetAffInfo` 内补生成 |
| 转正门槛硬编码 1 美元 | 小额分成站点无法调整 | 加 `AFF_MIN_TRANSFER_QUOTA` |

其中「路由被顶替」是最隐蔽的一类：gin 对同一 `method+path` 重复注册**不报错**，
静默保留先注册的那个。编译、单测、日志全部正常，只有真的发一次请求才会暴露。
因此 `router` 测试里加了 `TestNoDuplicateRoutes`，直接扫 route 表查重。
