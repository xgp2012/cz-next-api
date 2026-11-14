package router

import (
	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/controller"
	"github.com/songquanpeng/one-api/middleware"
)

// SetPaymentRouter 注册充值 / 支付相关路由。
//
// 为什么单独抽一个函数而不是直接写进 SetApiRouter：
//  1. 支付是可选功能，独立成文件后「哪些接口会动钱」一目了然，便于安全审计；
//  2. 部署时若不需要在线充值，可以不调用它，攻击面随之消失；
//  3. 回调接口（notify）必须**不加** UserAuth：调用方是支付网关而不是登录用户，
//     它们靠签名而不是会话鉴权，把这一点集中在一处能避免误加中间件。
//
// 与既有路由风格保持一致：所有业务接口返回 HTTP 200 + {success,...}，
// 只有 UserAuth/AdminAuth 的鉴权失败才会返回非 200。
func SetPaymentRouter(apiRouter *gin.RouterGroup) {
	// ---- 用户侧（需要登录）----
	apiRouter.GET("/user/topup/info", middleware.UserAuth(), controller.GetTopUpInfo)
	apiRouter.GET("/user/topup/self", middleware.UserAuth(), controller.GetSelfTopUps)
	apiRouter.POST("/user/pay", middleware.UserAuth(), controller.CreateEpayOrder)
	apiRouter.POST("/user/amount", middleware.UserAuth(), controller.GetPayAmount)

	// ---- 支付网关回调（无会话鉴权，靠签名校验）----
	// EPay 的回调方式由网关配置决定，GET / POST 都要支持
	apiRouter.GET("/user/epay/notify", controller.EpayNotify)
	apiRouter.POST("/user/epay/notify", controller.EpayNotify)
	// 通用 webhook 网关：X-Pay-Signature = HMAC-SHA256(raw body, WebhookSecret)
	apiRouter.POST("/pay/notify", controller.WebhookNotify)

	// ---- 管理员侧 ----
	apiRouter.GET("/user/topup", middleware.AdminAuth(), controller.GetAllTopUpsHandler)
	apiRouter.POST("/user/topup/complete", middleware.AdminAuth(), controller.AdminCompleteTopUpHandler)
}
