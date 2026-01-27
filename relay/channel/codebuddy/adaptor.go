package codebuddy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const MaxRateLimitRetries = 3

func readResponseBodyWithTimeout(resp *http.Response, timeout time.Duration) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)

	go func() {
		data, err := io.ReadAll(resp.Body)
		resultCh <- readResult{data: data, err: err}
	}()

	select {
	case result := <-resultCh:
		return result.data, result.err
	case <-ctx.Done():
		resp.Body.Close()
		return nil, fmt.Errorf("read response body timeout after %v", timeout)
	}
}

type Adaptor struct {
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// Use v2 endpoint instead of v1
	return fmt.Sprintf("%s/v2/chat/completions", info.ChannelBaseUrl), nil
}

func generateUUID() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		time.Now().UnixNano()&0xffffffff,
		time.Now().UnixNano()>>32&0xffff,
		0x4000|(time.Now().UnixNano()>>48&0x0fff),
		0x8000|(time.Now().UnixNano()>>60&0x3fff),
		time.Now().UnixNano()^int64(time.Now().Nanosecond()))
}

func generateTraceId() string {
	return fmt.Sprintf("%016x%016x",
		time.Now().UnixNano(),
		time.Now().UnixNano()^int64(time.Now().Nanosecond()))
}

func generateSpanId() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano()^int64(time.Now().Nanosecond()))
}

func getUserIdFromApiKey(apiKey string) string {
	jwtPayload, err := parseJWTPayload(apiKey)
	if err == nil && jwtPayload != nil && jwtPayload.Sub != "" {
		return jwtPayload.Sub
	}
	return generateUUID()
}

func getHeaderOrGenerate(c *gin.Context, key string, generator func() string) string {
	if val := c.Request.Header.Get(key); val != "" {
		return val
	}
	return generator()
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	req.Set("Content-Type", "application/json")
	if info.IsStream {
		req.Set("Accept", "text/event-stream")
	}
	req.Set("Authorization", "Bearer "+info.ApiKey)

	traceId := getHeaderOrGenerate(c, "X-Request-ID", generateTraceId)
	parentSpanId := generateSpanId()
	spanId := generateSpanId()
	conversationId := getHeaderOrGenerate(c, "X-Conversation-ID", generateUUID)

	req.Set("X-Agent-Intent", getHeaderOrGenerate(c, "X-Agent-Intent", func() string { return "chat" }))
	req.Set("X-Conversation-ID", conversationId)
	req.Set("X-Conversation-Request-ID", getHeaderOrGenerate(c, "X-Conversation-Request-ID", generateUUID))
	req.Set("X-Conversation-Message-ID", getHeaderOrGenerate(c, "X-Conversation-Message-ID", generateUUID))
	req.Set("X-Requested-With", "XMLHttpRequest")
	req.Set("X-IDE-Type", getHeaderOrGenerate(c, "X-IDE-Type", func() string { return "CodeBuddyIDE" }))
	req.Set("X-IDE-Name", getHeaderOrGenerate(c, "X-IDE-Name", func() string { return "CodeBuddyIDE" }))
	req.Set("X-IDE-Version", getHeaderOrGenerate(c, "X-IDE-Version", func() string { return "4.3.3" }))
	req.Set("X-Product-Version", getHeaderOrGenerate(c, "X-Product-Version", func() string { return "4.3.3" }))
	req.Set("X-Request-Trace-Id", getHeaderOrGenerate(c, "X-Request-Trace-Id", generateUUID))
	req.Set("X-Env-ID", getHeaderOrGenerate(c, "X-Env-ID", func() string { return "production" }))
	req.Set("X-User-Id", getUserIdFromApiKey(info.ApiKey))
	req.Set("X-Product", getHeaderOrGenerate(c, "X-Product", func() string { return "SaaS" }))
	req.Set("User-Agent", getHeaderOrGenerate(c, "User-Agent", func() string { return "CodeBuddyIDE/4.3.3" }))

	now := time.Now().UnixMilli()
	req.Set("monitor_promptPrepareStartTime", fmt.Sprintf("%d", now))
	req.Set("monitor_httpSendTime", fmt.Sprintf("%d", now+6))

	req.Set("X-Request-ID", traceId)
	req.Set("b3", fmt.Sprintf("%s-%s-1-%s", traceId, spanId, parentSpanId))
	req.Set("X-B3-TraceId", traceId)
	req.Set("X-B3-ParentSpanId", parentSpanId)
	req.Set("X-B3-SpanId", spanId)
	req.Set("X-B3-Sampled", "1")

	for k, v := range info.HeadersOverride {
		if str, ok := v.(string); ok {
			req.Set(k, str)
		}
	}

	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}

	// Force stream mode - CodeBuddy only supports streaming
	request.Stream = true
	info.IsStream = true
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}

	// Convert Claude format to OpenAI format
	openAIRequest, err := service.ClaudeToOpenAIRequest(*request, info)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Claude request to OpenAI format: %w", err)
	}
	// Force stream mode - CodeBuddy only supports streaming
	openAIRequest.Stream = true
	info.IsStream = true
	return openAIRequest, nil
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	return a.doRequestWithRateLimitRetry(c, info, bodyBytes)
}

func (a *Adaptor) doRequestWithRateLimitRetry(c *gin.Context, info *relaycommon.RelayInfo, bodyBytes []byte) (any, error) {
	resp, err := channel.DoApiRequest(a, c, info, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	logger.LogInfo(c, fmt.Sprintf("[CodeBuddy] DoRequest 响应状态码: %d", resp.StatusCode))

	// 检查是否是 429 限流响应
	if resp.StatusCode == http.StatusTooManyRequests {
		logger.LogInfo(c, "[CodeBuddy] 检测到 429 限流，开始处理...")
		return a.handleRateLimitInRequest(c, info, bodyBytes, resp)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		logger.LogWarn(c, "[CodeBuddy] 检测到 401 认证失败，开始处理...")
		return a.handleUnauthorizedInRequest(c, info, resp)
	}

	return resp, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	select {
	case <-c.Request.Context().Done():
		resp.Body.Close()
		return &dto.Usage{}, nil
	default:
	}

	return a.streamHandler(c, resp, info)
}

func (a *Adaptor) streamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	model := info.UpstreamModelName
	var responseId string
	var createAt int64 = 0
	var systemFingerprint string
	var containStreamUsage bool
	var responseTextBuilder strings.Builder
	var toolCount int
	var usageResult = &dto.Usage{}
	var streamItems []string
	var lastStreamData string

	var headersSet bool

	helper.StreamScannerHandler(c, resp, info, func(data string) bool {
		if len(data) > 0 {
			streamItems = append(streamItems, data)
		}

		if !headersSet {
			helper.SetEventStreamHeaders(c)
			headersSet = true
		}

		if lastStreamData != "" {
			err := openai.HandleStreamFormat(c, info, lastStreamData, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
			if err != nil {
				common.SysLog("error handling stream format: " + err.Error())
			}
		}

		if len(data) > 0 {
			lastStreamData = data
		}
		return true
	})

	// 处理最后的响应
	shouldSendLastResp := true
	if err := openai.HandleLastResponse(lastStreamData, &responseId, &createAt, &systemFingerprint, &model, &usageResult,
		&containStreamUsage, info, &shouldSendLastResp); err != nil {
		// 只在非空数据时记录错误
		if lastStreamData != "" && lastStreamData != " " {
			logger.LogError(c, fmt.Sprintf("error handling last response: %s, lastStreamData: [%s]", err.Error(), lastStreamData))
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		if shouldSendLastResp {
			_ = openai.HandleStreamFormat(c, info, lastStreamData, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
		}
	} else if info.RelayFormat == types.RelayFormatClaude {
		_ = openai.HandleStreamFormat(c, info, lastStreamData, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
	}

	// 处理token计算
	if err := openai.ProcessTokens(info.RelayMode, streamItems, &responseTextBuilder, &toolCount); err != nil {
		logger.LogError(c, "error processing tokens: "+err.Error())
	}

	if !containStreamUsage {
		usageResult = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usageResult.CompletionTokens += toolCount * 7
	}

	openai.HandleFinalResponse(c, info, lastStreamData, responseId, createAt, model, systemFingerprint, usageResult, containStreamUsage)

	return usageResult, nil
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func getNextMonthFirstDay() time.Time {
	now := time.Now()
	year, month, _ := now.Date()
	nextMonth := month + 1
	nextYear := year
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}
	return time.Date(nextYear, nextMonth, 1, 0, 0, 0, 0, now.Location())
}

// getRetryCount 获取当前重试次数
func (a *Adaptor) getRetryCount(c *gin.Context) int {
	return c.GetInt("codebuddy_ratelimit_retry")
}

// incRetryCount 增加重试计数，返回新的计数值
func (a *Adaptor) incRetryCount(c *gin.Context) int {
	count := a.getRetryCount(c) + 1
	c.Set("codebuddy_ratelimit_retry", count)
	return count
}

// canRetry 检查是否还可以重试
func (a *Adaptor) canRetry(c *gin.Context) bool {
	return a.getRetryCount(c) < MaxRateLimitRetries
}

// retryWithNewKey 切换 Key 并重试请求
func (a *Adaptor) retryWithNewKey(c *gin.Context, info *relaycommon.RelayInfo, bodyBytes []byte, errorBody string) (any, error) {
	if !a.canRetry(c) {
		logger.LogError(c, fmt.Sprintf("[CodeBuddy] 重试次数已达上限 (%d次)，触发渠道切换", MaxRateLimitRetries))
		return nil, types.NewError(
			fmt.Errorf("all keys rate limited: %s", errorBody),
			types.ErrorCodeChannelAllKeysRateLimited,
		)
	}

	if err := a.switchToNextAvailableKey(c, info); err != nil {
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 切换 Key 失败: %v，触发渠道切换", err))
		return nil, types.NewError(
			fmt.Errorf("no available keys: %s", errorBody),
			types.ErrorCodeChannelAllKeysRateLimited,
		)
	}

	retryCount := a.incRetryCount(c)
	logger.LogInfo(c, fmt.Sprintf("[CodeBuddy] 已切换到新 Key (index: %d)，正在重试 (%d/%d)",
		info.ChannelMultiKeyIndex, retryCount, MaxRateLimitRetries))

	return a.doRequestWithRateLimitRetry(c, info, bodyBytes)
}

// containsErrorCode 检查响应体中是否包含指定错误码
func containsErrorCode(body string, code int) bool {
	codeStr := fmt.Sprintf(`"code":%d`, code)
	codeStrWithSpace := fmt.Sprintf(`"code": %d`, code)
	return strings.Contains(body, codeStr) || strings.Contains(body, codeStrWithSpace)
}

// handleRateLimitInRequest 在 DoRequest 阶段处理 429 限流
func (a *Adaptor) handleRateLimitInRequest(c *gin.Context, info *relaycommon.RelayInfo, bodyBytes []byte, resp *http.Response) (any, error) {
	errorBody := a.readAndCloseResponseBody(c, resp)

	// 14013: 额度用尽，冷却到下月1日后切换 Key
	if containsErrorCode(errorBody, 14013) {
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 额度用尽 (14013)，Key index: %d: %s", info.ChannelMultiKeyIndex, errorBody))
		a.setKeyCooldown(info.ChannelId, info.ChannelMultiKeyIndex, getNextMonthFirstDay())
		return a.retryWithNewKey(c, info, bodyBytes, errorBody)
	}

	// 其他 429 错误直接透传
	logger.LogInfo(c, fmt.Sprintf("[CodeBuddy] 429 错误，直接透传: %s", errorBody))
	return nil, types.NewError(
		fmt.Errorf("rate limited: %s", errorBody),
		types.ErrorCodeRateLimited,
	)
}

// readAndCloseResponseBody 读取并关闭响应体
func (a *Adaptor) readAndCloseResponseBody(c *gin.Context, resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	defer resp.Body.Close()

	respBytes, err := readResponseBodyWithTimeout(resp, 10*time.Second)
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 读取响应体失败: %v", err))
		return ""
	}
	return string(respBytes)
}

// setKeyCooldown 设置指定 Key 的冷却时间
func (a *Adaptor) setKeyCooldown(channelId int, keyIndex int, resetTime time.Time) {
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		return
	}

	// 获取渠道轮询锁
	lock := model.GetChannelPollingLock(channelId)
	lock.Lock()
	defer lock.Unlock()

	// 初始化冷却时间 map
	if ch.ChannelInfo.MultiKeyCooldownUntil == nil {
		ch.ChannelInfo.MultiKeyCooldownUntil = make(map[int]int64)
	}

	// 设置冷却结束时间（Unix 时间戳）
	ch.ChannelInfo.MultiKeyCooldownUntil[keyIndex] = resetTime.Unix()

	// 持久化到数据库
	if err := ch.SaveChannelInfo(); err != nil {
		common.SysError(fmt.Sprintf("[CodeBuddy] 保存冷却信息失败: %v", err))
	}
}

// switchToNextAvailableKey 切换到下一个可用的 Key（跳过冷却中的 Key）
func (a *Adaptor) switchToNextAvailableKey(c *gin.Context, info *relaycommon.RelayInfo) error {
	ch, err := model.CacheGetChannel(info.ChannelId)
	if err != nil {
		return fmt.Errorf("获取渠道信息失败: %w", err)
	}

	// 获取所有 Key
	keys := ch.GetKeys()
	if len(keys) <= 1 {
		return errors.New("没有其他可用的 Key")
	}

	// 获取渠道轮询锁
	lock := model.GetChannelPollingLock(info.ChannelId)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().Unix()
	currentIndex := info.ChannelMultiKeyIndex

	// 查找下一个不在冷却中的 Key
	for i := 1; i < len(keys); i++ {
		nextIndex := (currentIndex + i) % len(keys)

		// 检查 Key 是否被禁用
		if ch.ChannelInfo.MultiKeyStatusList != nil {
			if status, ok := ch.ChannelInfo.MultiKeyStatusList[nextIndex]; ok {
				if status != common.ChannelStatusEnabled {
					continue
				}
			}
		}

		// 检查 Key 是否在冷却中
		if ch.ChannelInfo.MultiKeyCooldownUntil != nil {
			if cooldownUntil, ok := ch.ChannelInfo.MultiKeyCooldownUntil[nextIndex]; ok {
				if cooldownUntil > now {
					// 仍在冷却中
					logger.LogInfo(c, fmt.Sprintf("[CodeBuddy] Key index %d 仍在冷却中，跳过", nextIndex))
					continue
				}
			}
		}

		// 找到可用的 Key
		info.ApiKey = keys[nextIndex]
		info.ChannelMultiKeyIndex = nextIndex

		// 更新轮询索引
		ch.ChannelInfo.MultiKeyPollingIndex = (nextIndex + 1) % len(keys)

		logger.LogInfo(c, fmt.Sprintf("[CodeBuddy] 已切换 API Key: index %d -> %d", currentIndex, nextIndex))
		return nil
	}

	return errors.New("所有 Key 都在冷却中或被禁用")
}

func (a *Adaptor) handleUnauthorizedInRequest(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (any, error) {
	var errorBody string
	if resp.Body != nil {
		respBytes, err := readResponseBodyWithTimeout(resp, 10*time.Second)
		if err != nil {
			logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 读取 401 响应体超时或失败: %v", err))
		}
		errorBody = string(respBytes)
		resp.Body.Close()
	}

	if strings.Contains(errorBody, "invalid_format") {
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 请求格式错误（非认证问题），直接返回错误: %s", errorBody))
		return nil, types.NewError(
			fmt.Errorf("invalid request format: %s", errorBody),
			types.ErrorCodeBadRequestBody,
		)
	}

	logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 401 认证失败，Key index: %d，错误信息: %s", info.ChannelMultiKeyIndex, errorBody))

	a.disableCurrentKey(c, info, "401 Unauthorized: "+errorBody)

	return nil, types.NewError(
		fmt.Errorf("key unauthorized: %s", errorBody),
		types.ErrorCodeChannelInvalidKey,
	)
}

// disableCurrentKey 禁用当前使用的 Key
func (a *Adaptor) disableCurrentKey(c *gin.Context, info *relaycommon.RelayInfo, reason string) {
	ch, err := model.CacheGetChannel(info.ChannelId)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("[CodeBuddy] 获取渠道信息失败: %v", err))
		return
	}

	// 获取渠道轮询锁
	lock := model.GetChannelPollingLock(info.ChannelId)
	lock.Lock()
	defer lock.Unlock()

	// 初始化状态 map
	if ch.ChannelInfo.MultiKeyStatusList == nil {
		ch.ChannelInfo.MultiKeyStatusList = make(map[int]int)
	}
	if ch.ChannelInfo.MultiKeyDisabledReason == nil {
		ch.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
	}
	if ch.ChannelInfo.MultiKeyDisabledTime == nil {
		ch.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
	}

	// 设置 Key 状态为禁用
	keyIndex := info.ChannelMultiKeyIndex
	ch.ChannelInfo.MultiKeyStatusList[keyIndex] = common.ChannelStatusAutoDisabled
	ch.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
	ch.ChannelInfo.MultiKeyDisabledTime[keyIndex] = time.Now().Unix()

	logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 已禁用 Key index: %d，原因: %s", keyIndex, reason))
}
