package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/songquanpeng/one-api/common/helper"
	"github.com/songquanpeng/one-api/relay/constant/role"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
	"github.com/songquanpeng/one-api/relay/adaptor/openai"
	billingratio "github.com/songquanpeng/one-api/relay/billing/ratio"
	"github.com/songquanpeng/one-api/relay/channeltype"
	"github.com/songquanpeng/one-api/relay/controller/validator"
	"github.com/songquanpeng/one-api/relay/meta"
	relaymodel "github.com/songquanpeng/one-api/relay/model"
	"github.com/songquanpeng/one-api/relay/relaymode"
)

func getAndValidateTextRequest(c *gin.Context, relayMode int) (*relaymodel.GeneralOpenAIRequest, error) {
	textRequest := &relaymodel.GeneralOpenAIRequest{}
	err := common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}
	if relayMode == relaymode.Moderations && textRequest.Model == "" {
		textRequest.Model = "text-moderation-latest"
	}
	if relayMode == relaymode.Embeddings && textRequest.Model == "" {
		textRequest.Model = c.Param("model")
	}
	err = validator.ValidateTextRequest(textRequest, relayMode)
	if err != nil {
		return nil, err
	}
	return textRequest, nil
}

func getPromptTokens(textRequest *relaymodel.GeneralOpenAIRequest, relayMode int) int {
	switch relayMode {
	case relaymode.ChatCompletions:
		return openai.CountTokenMessages(textRequest.Messages, textRequest.Model)
	case relaymode.Completions:
		return openai.CountTokenInput(textRequest.Prompt, textRequest.Model)
	case relaymode.Moderations:
		return openai.CountTokenInput(textRequest.Input, textRequest.Model)
	}
	return 0
}

// resolvePriceForRequest 解析本次请求的价格。
//
// 注意：这里**同时**算好旧倍率，是因为计费日志与部分 adaptor 仍按倍率口径展示；
// 而实际形态由 model.PricingRule 是否命中决定（命中走规则，未命中走旧倍率）。
func resolvePriceForRequest(textRequest *relaymodel.GeneralOpenAIRequest, m *meta.Meta) model.CachedPrice {
	return model.ResolvePrice(model.PriceContext{
		Model:       textRequest.Model,
		Group:       m.Group,
		ChannelType: m.ChannelType,
	})
}

// getPreConsumedQuota 估算预扣额度。
//
// 旧路径公式保持不变：(PreConsumedQuota + promptTokens + maxTokens) × ratio
// 新路径（规则命中）：按规则的输入单价估算 prompt + maxTokens，
// 不再额外加 PreConsumedQuota 常量——因为按次计费/阶梯计费下这个常量没有语义。
func getPreConsumedQuota(textRequest *relaymodel.GeneralOpenAIRequest, promptTokens int, price model.CachedPrice) int64 {
	if price.IsRuleBased() {
		estimate := int64(promptTokens)
		if textRequest.MaxTokens != 0 {
			estimate += int64(textRequest.MaxTokens)
		}
		if q, ok := price.EstimatePreConsume(estimate); ok {
			return q
		}
		// 规则配置不完整时回落旧路径
	}
	preConsumedTokens := config.PreConsumedQuota + int64(promptTokens)
	if textRequest.MaxTokens != 0 {
		preConsumedTokens += int64(textRequest.MaxTokens)
	}
	return int64(float64(preConsumedTokens) * price.LegacyRatio())
}

func preConsumeQuota(ctx context.Context, textRequest *relaymodel.GeneralOpenAIRequest, promptTokens int, price model.CachedPrice, meta *meta.Meta) (int64, *relaymodel.ErrorWithStatusCode) {
	preConsumedQuota := getPreConsumedQuota(textRequest, promptTokens, price)

	userQuota, err := model.CacheGetUserQuota(ctx, meta.UserId)
	if err != nil {
		return preConsumedQuota, openai.ErrorWrapper(err, "get_user_quota_failed", http.StatusInternalServerError)
	}
	if userQuota-preConsumedQuota < 0 {
		return preConsumedQuota, openai.ErrorWrapper(errors.New("user quota is not enough"), "insufficient_user_quota", http.StatusForbidden)
	}
	err = model.CacheDecreaseUserQuota(meta.UserId, preConsumedQuota)
	if err != nil {
		return preConsumedQuota, openai.ErrorWrapper(err, "decrease_user_quota_failed", http.StatusInternalServerError)
	}
	if userQuota > 100*preConsumedQuota {
		// in this case, we do not pre-consume quota
		// because the user has enough quota
		preConsumedQuota = 0
		logger.Info(ctx, fmt.Sprintf("user %d has enough quota %d, trusted and no need to pre-consume", meta.UserId, userQuota))
	}
	if preConsumedQuota > 0 {
		err := model.PreConsumeTokenQuota(meta.TokenId, preConsumedQuota)
		if err != nil {
			return preConsumedQuota, openai.ErrorWrapper(err, "pre_consume_token_quota_failed", http.StatusForbidden)
		}
	}
	return preConsumedQuota, nil
}

// postConsumeQuota 结算。
//
// 与改造前的差异：
//  1. 单价来自 price（规则或旧倍率），结算用**预扣时同一份快照**，避免中途改价导致对不上账。
//  2. 结算后触发邀请分成累加（异步失败不影响主流程）。
//  3. 日志内容统一为 QuotaResult.Detail，运营可直接看懂这次请求怎么算的钱。
func postConsumeQuota(ctx context.Context, usage *relaymodel.Usage, m *meta.Meta, textRequest *relaymodel.GeneralOpenAIRequest, price model.CachedPrice, preConsumedQuota int64, systemPromptReset bool) {
	if usage == nil {
		logger.Error(ctx, "usage is nil, which is unexpected")
		return
	}
	result := price.Settle(model.Usage{
		PromptTokens:     int64(usage.PromptTokens),
		CompletionTokens: int64(usage.CompletionTokens),
	})
	quota := result.Quota
	if quota < 0 {
		quota = 0
	}

	totalTokens := usage.PromptTokens + usage.CompletionTokens
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
	}
	quotaDelta := quota - preConsumedQuota
	err := model.PostConsumeTokenQuota(m.TokenId, quotaDelta)
	if err != nil {
		logger.Error(ctx, "error consuming token remain quota: "+err.Error())
	}
	err = model.CacheUpdateUserQuota(ctx, m.UserId)
	if err != nil {
		logger.Error(ctx, "error update user quota cache: "+err.Error())
	}

	logContent := result.Detail
	model.RecordConsumeLog(ctx, &model.Log{
		UserId:            m.UserId,
		ChannelId:         m.ChannelId,
		PromptTokens:      usage.PromptTokens,
		CompletionTokens:  usage.CompletionTokens,
		ModelName:         textRequest.Model,
		TokenName:         m.TokenName,
		Quota:             int(quota),
		Content:           logContent,
		IsStream:          m.IsStream,
		ElapsedTime:       helper.CalcElapsedTime(m.StartTime),
		SystemPromptReset: systemPromptReset,
	})
	model.UpdateUserUsedQuotaAndRequestCount(m.UserId, quota)
	model.UpdateChannelUsedQuota(m.ChannelId, quota)

	// 邀请分成：被邀请人消费后给邀请人计佣。失败只记日志，绝不影响主流程。
	if quota > 0 && model.CurrentCommissionConfig().Enabled {
		if err := model.AccrueConsumeCommission(ctx, m.UserId, quota, 0); err != nil {
			logger.Error(ctx, "failed to accrue invite commission: "+err.Error())
		}
	}
}

func getMappedModelName(modelName string, mapping map[string]string) (string, bool) {
	if mapping == nil {
		return modelName, false
	}
	mappedModelName := mapping[modelName]
	if mappedModelName != "" {
		return mappedModelName, true
	}
	return modelName, false
}

func isErrorHappened(meta *meta.Meta, resp *http.Response) bool {
	if resp == nil {
		if meta.ChannelType == channeltype.AwsClaude {
			return false
		}
		return true
	}
	if resp.StatusCode != http.StatusOK &&
		// replicate return 201 to create a task
		resp.StatusCode != http.StatusCreated {
		return true
	}
	if meta.ChannelType == channeltype.DeepL {
		// skip stream check for deepl
		return false
	}

	if meta.IsStream && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") &&
		// Even if stream mode is enabled, replicate will first return a task info in JSON format,
		// requiring the client to request the stream endpoint in the task info
		meta.ChannelType != channeltype.Replicate {
		return true
	}
	return false
}

func setSystemPrompt(ctx context.Context, request *relaymodel.GeneralOpenAIRequest, prompt string) (reset bool) {
	if prompt == "" {
		return false
	}
	if len(request.Messages) == 0 {
		return false
	}
	if request.Messages[0].Role == role.System {
		request.Messages[0].Content = prompt
		logger.Infof(ctx, "rewrite system prompt")
		return true
	}
	request.Messages = append([]relaymodel.Message{{
		Role:    role.System,
		Content: prompt,
	}}, request.Messages...)
	logger.Infof(ctx, "add system prompt")
	return true
}

// legacyRatioPair 供仍在使用 (modelRatio, groupRatio, completionRatio) 三元组的老代码使用
func legacyRatioPair(name string, channelType int, group string) (float64, float64, float64) {
	return billingratio.GetModelRatio(name, channelType), billingratio.GetGroupRatio(group), billingratio.GetCompletionRatio(name, channelType)
}

var _ = math.Ceil // 保留 math 引用，避免后续删改时反复调整 import
