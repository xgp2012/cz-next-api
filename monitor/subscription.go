package monitor

import (
	"strconv"
	"time"

	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
)

// ============================================================================
// 订阅周期扫描器
//
// 为什么需要一个后台 goroutine 而不是「请求时惰性判断」：
//   到期回退分组、自动续费扣款、额度重置这三件事即使没有请求也必须发生，
//   否则用户不访问时订阅会一直停在过期状态、分组也不会回落。
//
// 设计取舍：
//   - 单实例内串行处理，靠数据库行锁与条件 UPDATE 保证幂等，多节点同时跑
//     也只会有一个节点真正完成某个订阅的状态迁移（另一个 RowsAffected=0）。
//   - 每轮处理量有上限（batch），避免一次性扫全表把数据库拖死。
//   - 间隔 1 分钟：商业上 1 分钟的续费延迟完全可接受，换取极低的负载。
// ============================================================================

const (
	subscriptionTickInterval = time.Minute
	subscriptionBatchSize    = 100
	topUpExpireInterval      = 5 * time.Minute
)

// StartSubscriptionWorker 启动订阅与订单的周期任务
func StartSubscriptionWorker() {
	go func() {
		// 启动后先等一会，避免与数据库迁移抢锁
		time.Sleep(30 * time.Second)
		logger.SysLog("subscription worker started")
		ticker := time.NewTicker(subscriptionTickInterval)
		defer ticker.Stop()
		topUpTicker := time.NewTicker(topUpExpireInterval)
		defer topUpTicker.Stop()

		for {
			select {
			case <-ticker.C:
				runSubscriptionTick()
			case <-topUpTicker.C:
				runTopUpExpireTick()
			}
		}
	}()
}

func runSubscriptionTick() {
	// ① 重置到期的额度周期
	if n, err := model.ResetDueSubscriptions(subscriptionBatchSize); err != nil {
		logger.SysError("reset due subscriptions failed: " + err.Error())
	} else if n > 0 {
		logger.SysLog(fmtCount("subscriptions quota reset", n))
	}
	// ② 处理到期订阅（自动续费 / 宽限 / 回落分组）
	if n, err := model.ExpireDueSubscriptions(subscriptionBatchSize); err != nil {
		logger.SysError("expire due subscriptions failed: " + err.Error())
	} else if n > 0 {
		logger.SysLog(fmtCount("subscriptions processed", n))
	}
}

func runTopUpExpireTick() {
	// 超时未支付的订单标记为过期，避免 pending 堆积干扰对账
	expireMinutes := model.CurrentTopUpExpireMinutes()
	if expireMinutes <= 0 {
		expireMinutes = 30
	}
	n, err := model.ExpireStaleTopUps(time.Duration(expireMinutes) * time.Minute)
	if err != nil {
		logger.SysError("expire stale top ups failed: " + err.Error())
		return
	}
	if n > 0 {
		logger.SysLog(fmtCount("stale topup orders expired", int(n)))
	}
}

// fmtCount 拼一条可读的日志
func fmtCount(label string, n int) string {
	return label + ": " + strconv.Itoa(n)
}
