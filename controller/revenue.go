package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/model"
)

// ============================================================================
// 营收看板
//
// 与 New API 的差异：它的看板是「消耗」视角（quota_data 按模型/用户聚合），
// 完全不展示收入。商业运营最需要的其实是「收入 - 消耗 = 毛利」这条线，
// 所以这里以 TopUp 订单表为收入真源，以 logs 消费为成本真源，两者对齐到天。
//
// 聚合逻辑全部在 model.BuildRevenueStats，本文件只负责参数校验与响应封装，
// 避免"类型定义在这里、数据在别处"的割裂（早期版本就踩过这个坑：
// 控制器声明了 RevenueStats 结构体，而 model 返回的是 map，字段名对不上，
// 前端拿到的是空值）。
// ============================================================================

// GetRevenueStats 管理员营收看板
//
// 查询参数：days（1-90，默认 7）
func GetRevenueStats(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		v, err := strconv.Atoi(d)
		if err != nil || v <= 0 || v > 90 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "days 必须是 1 到 90 之间的整数"})
			return
		}
		days = v
	}
	stats, err := model.BuildRevenueStats(days)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	// 顺带把额度换算基准给前端，让它能把"额度"口径的字段也渲染出来，
	// 避免前端再硬编码 500000 这个常量。
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}
