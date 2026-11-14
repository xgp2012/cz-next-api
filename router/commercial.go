package router

import (
	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/controller"
	"github.com/songquanpeng/one-api/middleware"
)

// SetCommercialRouter 注册商业化相关的路由：定价规则、订阅套餐、营收看板。
//
// 与 SetPaymentRouter 分开的原因：支付涉及密钥与回调，安全审计面完全不同；
// 这里都是纯读写的业务接口，鉴权走标准的 UserAuth / AdminAuth。
func SetCommercialRouter(apiRouter *gin.RouterGroup) {
	// ---- 价格墙（公开：登录前就能看到定价，这是转化漏斗的第一步）----
	apiRouter.GET("/pricing", controller.GetPricingWall)

	// ---- 定价规则管理（管理员）----
	pricingRoute := apiRouter.Group("/pricing_rules")
	pricingRoute.Use(middleware.AdminAuth())
	{
		pricingRoute.GET("/", controller.GetPricingRules)
		pricingRoute.POST("/", controller.CreatePricingRule)
		pricingRoute.PUT("/", controller.UpdatePricingRule)
		pricingRoute.DELETE("/:id", controller.DeletePricingRule)
	}

	// ---- 订阅（用户侧，需要登录）----
	subRoute := apiRouter.Group("/subscription")
	{
		// 套餐列表公开可见：未登录用户也应该能看到"有哪些套餐、多少钱"
		subRoute.GET("/plans", controller.GetSubscriptionPlans)

		userRoute := subRoute.Group("/self")
		userRoute.Use(middleware.UserAuth())
		{
			userRoute.GET("/", controller.GetSelfSubscription)
			userRoute.PUT("/preference", controller.UpdateBillingPreference)
			userRoute.PUT("/auto_renew", controller.ToggleSubscriptionAutoRenew)
		}
		subRoute.POST("/balance/pay", middleware.UserAuth(), controller.PaySubscriptionWithBalance)
	}

	// ---- 套餐管理（管理员）----
	planRoute := apiRouter.Group("/subscription/plans/admin")
	planRoute.Use(middleware.AdminAuth())
	{
		planRoute.GET("/", controller.GetAdminPlans)
		planRoute.POST("/", controller.CreateAdminPlan)
		planRoute.PUT("/", controller.UpdateAdminPlan)
		planRoute.DELETE("/:id", controller.DeleteAdminPlan)
	}
	apiRouter.POST("/subscription/invalidate", middleware.AdminAuth(), controller.InvalidateUserSubscription)

	// ---- 推广 / 邀请分成 ----
	affRoute := apiRouter.Group("/user/aff")
	affRoute.Use(middleware.UserAuth())
	{
		affRoute.GET("/", controller.GetAffInfo)
		affRoute.GET("/commissions", controller.GetAffCommissions)
		affRoute.GET("/invitees", controller.GetAffInvitees)
	}
	apiRouter.POST("/user/aff_transfer", middleware.UserAuth(), controller.TransferAffQuota)

	// ---- 营收看板（管理员）----
	apiRouter.GET("/admin/revenue", middleware.AdminAuth(), controller.GetRevenueStats)
}
