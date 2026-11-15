#!/bin/bash
# ============================================================================
# 本机验证脚本（Linux / WSL）
#
# 用途：不依赖 Docker，直接跑真实二进制，验证
#   1. 服务能启动、内嵌前端能被服务端提供
#   2. 定价规则引擎接管计费（而不是回落到旧倍率）
#   3. 邀请分成按实际消费计提、可转正
#   4. 订阅用余额支付时 cents -> quota 的换算正确
#
# 为什么要有这个脚本：`go build` 成功、`go test` 全绿都不能证明服务真的能跑。
# 本项目实际踩过的坑（无 gcc 时 SQLite 驱动只是桩、go:embed 只匹配到 .gitkeep
# 导致镜像里没有前端、gin 静默保留重复注册的旧路由）全都只在真实启动+
# 真实请求下才暴露。下面的断言都是为了抓住这一类问题。
#
# 用法：
#   ./scripts/verify-linux.sh /path/to/one-api-binary
#   不传参数时默认用 ./one-api
# ============================================================================
set -u

BIN="${1:-./one-api}"
PORT="${PORT:-53999}"
MOCK_PORT="${MOCK_PORT:-53998}"
WORK="${WORK:-/tmp/oneapi-verify}"
BASE="http://127.0.0.1:$PORT"

PASS=0
FAIL=0
ok()   { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad()  { echo "  ✗ $1"; FAIL=$((FAIL+1)); }
check(){ if [ "$2" = "$3" ]; then ok "$1 = $3"; else bad "$1: got '$2', want '$3'"; fi; }

jq_get() { python3 -c "import sys,json;d=json.load(sys.stdin);$1"; }
GET() { curl -s -b "$2" -c "$2" "$BASE$1"; }
POST() { curl -s -X POST -b "$3" -c "$3" -H 'Content-Type: application/json' -d "$2" "$BASE$1"; }
PUT() { curl -s -X PUT -b "$3" -c "$3" -H 'Content-Type: application/json' -d "$2" "$BASE$1"; }

cleanup() {
  pkill -x one-api 2>/dev/null
  pkill -f "$WORK/mock.py" 2>/dev/null
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
echo "==> 准备环境 ($WORK)"
mkdir -p "$WORK/data"
rm -f "$WORK/data/test.db" "$WORK"/ck-*

cat > "$WORK/mock.py" <<'PYEOF'
from http.server import BaseHTTPRequestHandler, HTTPServer
import json, sys

class H(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length', 0)))
        # 固定返回 1000 prompt + 500 completion，便于精确对账
        body = {
            "id": "chatcmpl-mock", "object": "chat.completion", "created": 1700000000,
            "model": "gpt-4o",
            "choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1000, "completion_tokens": 500, "total_tokens": 1500},
        }
        out = json.dumps(body).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(out)))
        self.end_headers()
        self.wfile.write(out)
    def log_message(self, *a): pass

HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
PYEOF

pkill -f "$WORK/mock.py" 2>/dev/null; sleep 1
nohup python3 "$WORK/mock.py" "$MOCK_PORT" > "$WORK/mock.log" 2>&1 &
sleep 2

pkill -x one-api 2>/dev/null; sleep 1
echo "==> 启动 one-api"
cd "$WORK"
PORT=$PORT \
SQLITE_PATH="$WORK/data/test.db" \
SESSION_SECRET=verify-secret \
AFF_COMMISSION_ENABLED=true \
AFF_COMMISSION_RATE=0.10 \
AFF_MIN_TRANSFER_QUOTA=1000 \
nohup "$(cd - >/dev/null; pwd)/$BIN" > "$WORK/run.log" 2>&1 &
sleep 14

if ! curl -s -m 3 -o /dev/null "$BASE/api/status"; then
  echo "FAILED to start. log:"; tail -20 "$WORK/run.log"; exit 1
fi

# ---------------------------------------------------------------------------
echo
echo "==> [1] 内嵌前端真的被服务端提供了吗"
curl -s -o "$WORK/idx.html" -w '%{http_code}' "$BASE/" > "$WORK/code.txt"
check "首页 HTTP" "$(cat "$WORK/code.txt")" "200"
ASSET=$(grep -o '/_nuxt/[A-Za-z0-9_.-]*\.js' "$WORK/idx.html" | head -1)
if [ -n "$ASSET" ]; then
  SZ=$(curl -s -o /dev/null -w '%{size_download}' "$BASE$ASSET")
  if [ "$SZ" -gt 1000 ]; then ok "hash 资源可下载 $ASSET ($SZ bytes)"
  else bad "资源 $ASSET 太小 ($SZ bytes)"; fi
else
  bad "index.html 里没有 /_nuxt/ 资源，前端未 embed"
fi

echo
echo "==> [2] 登录 + 建规则"
POST /api/user/login '{"username":"root","password":"123456"}' "$WORK/ck-root" > /dev/null
POST /api/pricing_rules/ '{"name":"verify","model":"gpt-4o","group":"","channel_type":-1,"billing_mode":"per_token","input_ratio":1.5,"output_ratio":6.0,"cache_hit_ratio":0.5,"status":1,"priority":100}' "$WORK/ck-root" > /dev/null
R=$(GET '/api/pricing_rules/?p=0' "$WORK/ck-root" | jq_get "print(len([x for x in (d.get('data') or []) if x['model']=='gpt-4o']))")
if [ "$R" -ge 1 ]; then ok "规则已生效 (gpt-4o 规则数=$R)"; else bad "规则未创建"; fi

echo
echo "==> [3] 邀请注册 + 佣金"
AFF=$(GET '/api/user/aff/' "$WORK/ck-root" | jq_get "print(d['data']['aff_code'])")
if [ -n "$AFF" ]; then ok "aff_code 已生成 ($AFF)"; else bad "aff_code 为空"; fi

POST /api/user/register "{\"username\":\"dave\",\"password\":\"test123456\",\"aff_code\":\"$AFF\"}" "$WORK/ck-root" > /dev/null
POST /api/user/login '{"username":"dave","password":"test123456"}' "$WORK/ck-dave" > /dev/null
PUT /api/user/ '{"id":2,"username":"dave","display_name":"dave","role":1,"status":1,"quota":10000000,"group":"default"}' "$WORK/ck-root" > /dev/null
POST /api/token/ '{"name":"k","remain_quota":0,"unlimited_quota":true,"expired_time":-1}' "$WORK/ck-dave" > /dev/null
KEY=$(GET '/api/token/?p=0' "$WORK/ck-dave" | jq_get "t=d.get('data') or [];print(t[0]['key'] if t else '')")

# 渠道指向 mock upstream
POST /api/channel/ "{\"name\":\"mock\",\"type\":1,\"key\":\"sk-test\",\"base_url\":\"http://127.0.0.1:$MOCK_PORT\",\"models\":\"gpt-4o\",\"group\":\"default\",\"status\":1}" "$WORK/ck-root" > /dev/null

Q0=$(GET /api/user/self "$WORK/ck-dave" | jq_get "print(d['data']['quota'])")
curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":5}'
sleep 3
Q1=$(GET /api/user/self "$WORK/ck-dave" | jq_get "print(d['data']['quota'])")
P1=$(GET '/api/user/aff/' "$WORK/ck-root" | jq_get "print(d['data']['pending_quota'])")

# (1000*1.5 + 500*6.0) * 0.002/1000 * 500000 = 4500 ; 10% -> 450
check "扣费额度" "$((Q0-Q1))" "4500"
check "佣金额度" "$P1" "450"

echo
echo "==> [4] 计费方式确实是规则而非旧倍率"
CONTENT=$(GET '/api/log/?p=0&type=2' "$WORK/ck-root" | jq_get "r=(d.get('data') or [{}])[0];print(r.get('content',''))")
case "$CONTENT" in
  按量计费*) ok "日志为规则计费: $CONTENT" ;;
  *) bad "日志仍是旧倍率: $CONTENT" ;;
esac

echo
echo "==> [5] 佣金转正"
for i in 1 2 3 4; do
  curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" \
    -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
    -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":5}'
done
sleep 3
PEND=$(GET '/api/user/aff/' "$WORK/ck-root" | jq_get "print(d['data']['pending_quota'])")
W0=$(GET /api/user/self "$WORK/ck-root" | jq_get "print(d['data']['quota'])")
POST /api/user/aff_transfer '{}' "$WORK/ck-root" > /dev/null
W1=$(GET /api/user/self "$WORK/ck-root" | jq_get "print(d['data']['quota'])")
PEND2=$(GET '/api/user/aff/' "$WORK/ck-root" | jq_get "print(d['data']['pending_quota'])")
if [ "$PEND" -ge 1000 ]; then
  check "转正到账" "$((W1-W0))" "$PEND"
  check "转正后 pending" "$PEND2" "0"
else
  bad "累计佣金 $PEND 未达门槛 1000"
fi

echo
echo "==> [6] 订阅余额支付：cents -> quota 换算"
POST /api/subscription/plans/admin/ '{"title":"月付","price_cents":1999,"duration_unit":"month","duration_value":1,"total_quota":10000000,"quota_reset_period":"monthly","enabled":true}' "$WORK/ck-root" > /dev/null
PLAN=$(GET '/api/subscription/plans' "$WORK/ck-root" | jq_get "ps=d['data'].get('plans') or [];print(ps[0]['id'] if ps else '')")

# 关键：前面的代理请求已经消耗了 dave 的额度（5 次 × 4500 = 22500），
# 而套餐价 9,995,000 接近初始的 10,000,000，会因余额不足而支付失败。
# 这里先把余额补足，让这一步只验证「换算是否正确」，不与「余额是否够」混在一起。
PUT /api/user/ '{"id":2,"username":"dave","display_name":"dave","role":1,"status":1,"quota":20000000,"group":"default"}' "$WORK/ck-root" > /dev/null

B0=$(GET /api/user/self "$WORK/ck-dave" | jq_get "print(d['data']['quota'])")
PAYRESP=$(POST /api/subscription/balance/pay "{\"plan_id\":$PLAN}" "$WORK/ck-dave")
B1=$(GET /api/user/self "$WORK/ck-dave" | jq_get "print(d['data']['quota'])")
# 1999 分 = $19.99 ; $19.99 * 500000 = 9,995,000
if echo "$PAYRESP" | grep -q '"success":true'; then
  check "余额扣减" "$((B0-B1))" "9995000"
else
  bad "支付失败: $PAYRESP"
fi

echo
echo "==> [6b] 余额不足时必须明确报错（不能静默成功）"
# 注意：不能用 root 测——root 是 role=100 的管理员，额度视为无限，永远支付成功。
# 必须用一个真实额度不足的普通用户。
POST /api/user/register '{"username":"erin","password":"test123456"}' "$WORK/ck-root" > /dev/null
POST /api/user/login '{"username":"erin","password":"test123456"}' "$WORK/ck-erin" > /dev/null
PUT /api/user/ '{"id":3,"username":"erin","display_name":"erin","role":1,"status":1,"quota":100,"group":"default"}' "$WORK/ck-root" > /dev/null
POOR=$(POST /api/subscription/balance/pay "{\"plan_id\":$PLAN}" "$WORK/ck-erin")
case "$POOR" in
  *余额不足*) ok "余额不足时有明确提示" ;;
  *'"success":true'*) bad "余额不足却支付成功了: $POOR" ;;
  *) bad "余额不足时返回不明确: $POOR" ;;
esac

echo
echo "==> [7] 营收看板"
POST /api/user/login '{"username":"root","password":"123456"}' "$WORK/ck-root" > /dev/null
REV=$(GET '/api/admin/revenue?days=7' "$WORK/ck-root" | jq_get "print(d['data']['days'],len(d['data']['daily']),d['data']['total_income_cents'])")
check "看板 days/rows/income" "$REV" "7 7 1999"

# ---------------------------------------------------------------------------
echo
echo "============================================"
echo "  通过 $PASS 项，失败 $FAIL 项"
echo "============================================"
[ "$FAIL" -eq 0 ] || { echo; echo "服务日志末尾："; tail -20 "$WORK/run.log"; exit 1; }
