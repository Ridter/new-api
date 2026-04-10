package codebuddy

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ModelTokenLimits 模型的 token 限制信息
type ModelTokenLimits struct {
	MaxInputTokens  int
	MaxOutputTokens int
}

// modelTokenLimitsCache 全局内存缓存，key 是模型 ID
var modelTokenLimitsCache sync.Map

// cacheRefreshMutex 防止并发刷新
var cacheRefreshMutex sync.Mutex
var lastCacheRefreshTime time.Time

const cacheRefreshInterval = 1 * time.Hour

// tokenLimitMarginPercent 输入 token 校验的余量百分比
// 因为 estimatePromptTokens 是估算值，留 5% 余量避免误拦
const tokenLimitMarginPercent = 0.05

// GetModelTokenLimits 获取指定模型的 token 限制
func GetModelTokenLimits(modelID string) (*ModelTokenLimits, bool) {
	if v, ok := modelTokenLimitsCache.Load(modelID); ok {
		return v.(*ModelTokenLimits), true
	}
	return nil, false
}

// SetModelTokenLimits 手动设置指定模型的 token 限制（用于测试或手动覆盖）
func SetModelTokenLimits(modelID string, limits *ModelTokenLimits) {
	modelTokenLimitsCache.Store(modelID, limits)
}

// GetCachedModelCount 获取缓存中的模型数量（用于日志/调试）
func GetCachedModelCount() int {
	count := 0
	modelTokenLimitsCache.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// RefreshModelTokenLimitsCache 从 API 刷新模型限制缓存
// 该函数是线程安全的，内部使用互斥锁防止并发刷新
func RefreshModelTokenLimitsCache(baseURL, apiKey string, headerOverride map[string]any) error {
	cacheRefreshMutex.Lock()
	defer cacheRefreshMutex.Unlock()

	// 避免短时间内重复刷新
	if time.Since(lastCacheRefreshTime) < cacheRefreshInterval {
		return nil
	}

	models, err := FetchCodeBuddyModelsWithMetadata(baseURL, apiKey, headerOverride)
	if err != nil {
		return fmt.Errorf("刷新模型 token 限制缓存失败: %w", err)
	}

	updatedCount := 0
	for _, model := range models {
		if model.MaxInputTokens > 0 || model.MaxOutputTokens > 0 {
			modelTokenLimitsCache.Store(model.ID, &ModelTokenLimits{
				MaxInputTokens:  model.MaxInputTokens,
				MaxOutputTokens: model.MaxOutputTokens,
			})
			updatedCount++
		}
	}

	lastCacheRefreshTime = time.Now()
	common.SysLog(fmt.Sprintf("[CodeBuddy] 模型 token 限制缓存已刷新，共更新 %d 个模型", updatedCount))
	return nil
}

// NeedRefreshCache 检查缓存是否需要刷新
func NeedRefreshCache() bool {
	cacheRefreshMutex.Lock()
	defer cacheRefreshMutex.Unlock()
	return time.Since(lastCacheRefreshTime) >= cacheRefreshInterval
}

// ValidateInputTokens 校验输入 token 是否超过模型限制
// 如果缓存中没有该模型的限制信息，则放行（降级策略）
func ValidateInputTokens(modelID string, estimatedInputTokens int) error {
	if estimatedInputTokens <= 0 {
		return nil
	}

	limits, ok := GetModelTokenLimits(modelID)
	if !ok {
		return nil
	}

	if limits.MaxInputTokens <= 0 {
		return nil
	}

	// 添加余量：允许估算值在限制的 (1 + margin) 倍以内通过
	maxAllowed := int(float64(limits.MaxInputTokens) * (1.0 + tokenLimitMarginPercent))
	if estimatedInputTokens > maxAllowed {
		return fmt.Errorf(
			"estimated input tokens (%d) exceeds model %s max input limit (%d), request rejected to avoid upstream error",
			estimatedInputTokens, modelID, limits.MaxInputTokens,
		)
	}

	return nil
}

// ValidateMaxOutputTokens 校验用户设置的 max_tokens / max_completion_tokens 是否超过模型限制
// 如果缓存中没有该模型的限制信息，则放行（降级策略）
func ValidateMaxOutputTokens(modelID string, requestedMaxTokens uint) error {
	if requestedMaxTokens == 0 {
		return nil
	}

	limits, ok := GetModelTokenLimits(modelID)
	if !ok {
		return nil
	}

	if limits.MaxOutputTokens <= 0 {
		return nil
	}

	if int(requestedMaxTokens) > limits.MaxOutputTokens {
		return fmt.Errorf(
			"requested max_tokens (%d) exceeds model %s max output limit (%d), request rejected to avoid upstream error",
			requestedMaxTokens, modelID, limits.MaxOutputTokens,
		)
	}

	return nil
}
