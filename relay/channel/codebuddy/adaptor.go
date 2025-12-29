package codebuddy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// 最大重试次数
const MaxSensitiveRetries = 10

// FinishReasonContentFilter 内容过滤的 finish_reason 标志
// 当上游返回此标志时，表示触发了敏感内容过滤
const FinishReasonContentFilter = "content_filter"

// ErrSensitiveContent 敏感内容错误
var ErrSensitiveContent = errors.New("sensitive content detected")

// saveSensitiveRequest 将触发检测的请求保存到文件
func saveSensitiveRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody []byte, response string, retryCount int) {
	// 确保目录存在
	logDir := filepath.Join(*common.LogDir, "codebuddy_sensitive")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logger.LogError(c, fmt.Sprintf("[CodeBuddy] 创建日志目录失败: %v", err))
		return
	}

	// 生成文件名: 时间戳_请求ID.json
	timestamp := time.Now().Format("20060102_150405")
	requestId := c.GetString("request_id")
	if requestId == "" {
		requestId = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	filename := fmt.Sprintf("%s_%s_retry%d.json", timestamp, requestId, retryCount)
	filePath := filepath.Join(logDir, filename)

	// 构建日志结构体
	logData := map[string]any{
		"timestamp":   time.Now().Format(time.RFC3339),
		"request_id":  requestId,
		"retry_count": retryCount,
		"max_retries": MaxSensitiveRetries,
		"user_id":     info.UserId,
		"user_group":  info.UserGroup,
		"response":    response,
	}

	// 尝试将请求体解析为 JSON 对象，如果失败则作为字符串保存
	var requestJSON any
	if err := json.Unmarshal(requestBody, &requestJSON); err != nil {
		requestJSON = string(requestBody)
	}
	logData["request"] = requestJSON

	// 序列化为 JSON
	logContent, err := json.MarshalIndent(logData, "", "  ")
	if err != nil {
		logger.LogError(c, fmt.Sprintf("[CodeBuddy] 序列化日志失败: %v", err))
		return
	}

	// 写入文件
	if err := os.WriteFile(filePath, logContent, 0644); err != nil {
		logger.LogError(c, fmt.Sprintf("[CodeBuddy] 保存日志失败: %v", err))
		return
	}

	logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 请求已保存到: %s", filePath))
}

type Adaptor struct {
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// Use v2 endpoint instead of v1
	return fmt.Sprintf("%s/v2/chat/completions", info.ChannelBaseUrl), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	// Custom headers are automatically applied via HeaderOverride in api_request.go
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

// KeyCodeBuddyUpstreamRequest 用于存储发送给上游的请求体（仅在敏感内容检测时使用）
const KeyCodeBuddyUpstreamRequest = "codebuddy_upstream_request"

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	// 读取请求体
	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// 保存请求体到 context，仅用于敏感内容检测时记录完整的上游请求
	c.Set(KeyCodeBuddyUpstreamRequest, bodyBytes)

	return channel.DoApiRequest(a, c, info, bytes.NewReader(bodyBytes))
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	// 检查客户端是否已断开连接
	select {
	case <-c.Request.Context().Done():
		resp.Body.Close()
		// 返回空的 Usage 而不是 nil，避免 claude_handler.go 中的类型断言 panic
		return &dto.Usage{}, nil
	default:
	}

	// 非阻塞流式处理：只检测第一个数据块的 finish_reason
	return a.streamWithContentFilterDetection(c, resp, info)
}

// streamWithContentFilterDetection 非阻塞流式处理
// 策略：只检测第一个数据块的 finish_reason 是否为 "content_filter"
// 如果是，立即重试；否则直接透传所有数据，零延迟
func (a *Adaptor) streamWithContentFilterDetection(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
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

	// 第一个数据块检测标志
	var firstChunkProcessed bool
	var contentFilterDetected bool
	var detectedContent string

	// 设置 SSE 响应头标志
	var headersSet bool

	helper.StreamScannerHandler(c, resp, info, func(data string) bool {
		// 如果已经检测到 content_filter，停止处理
		if contentFilterDetected {
			return false
		}

		if len(data) > 0 {
			streamItems = append(streamItems, data)
		}

		// 只检测第一个数据块
		if !firstChunkProcessed {
			firstChunkProcessed = true

			// 解析第一个数据块，检测 finish_reason
			var streamResp dto.ChatCompletionsStreamResponse
			if err := common.Unmarshal(common.StringToByteSlice(data), &streamResp); err == nil {
				for _, choice := range streamResp.Choices {
					// 检测 content_filter
					if choice.FinishReason != nil && *choice.FinishReason == FinishReasonContentFilter {
						contentFilterDetected = true
						detectedContent = choice.Delta.GetContentString()
						return false // 停止处理，准备重试
					}
				}
			}

			// 第一个块没有 content_filter，设置响应头并开始流式传输
			if !headersSet {
				helper.SetEventStreamHeaders(c)
				headersSet = true
			}

			// 保存第一个数据块，等待下一个块到来时发送
			if len(data) > 0 {
				lastStreamData = data
			}
			return true
		}

		// 后续数据块：直接透传，零延迟
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

	// 检查是否检测到 content_filter
	if contentFilterDetected {
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 检测到 content_filter，内容: %s", detectedContent))
		return a.handleSensitiveRetry(c, info, detectedContent)
	}

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

// handleSensitiveRetry 处理敏感内容重试逻辑
func (a *Adaptor) handleSensitiveRetry(c *gin.Context, info *relaycommon.RelayInfo, detectedContent string) (any, *types.NewAPIError) {
	// 获取当前重试次数
	retryCount := c.GetInt("codebuddy_sensitive_retry")

	// 优先使用保存的上游请求体（转换后的 OpenAI 格式），如果没有则使用原始请求
	var upstreamRequestBody []byte
	if cached, exists := c.Get(KeyCodeBuddyUpstreamRequest); exists && cached != nil {
		if b, ok := cached.([]byte); ok {
			upstreamRequestBody = b
		}
	}
	if upstreamRequestBody == nil {
		// 回退到原始请求体
		upstreamRequestBody, _ = common.GetRequestBody(c)
	}
	if common.DebugEnabled {
		saveSensitiveRequest(c, info, upstreamRequestBody, detectedContent, retryCount)
	}
	if retryCount < MaxSensitiveRetries {
		// 增加重试计数
		c.Set("codebuddy_sensitive_retry", retryCount+1)
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 检测到敏感内容，正在重试 (%d/%d)", retryCount+1, MaxSensitiveRetries))

		// 每次重试都尝试切换到不同的 API Key
		if err := a.switchToNextKey(c, info); err != nil {
			logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 切换 Key 失败: %v，继续使用当前 Key", err))
		}

		// 优先使用保存的上游请求体（转换后的 OpenAI 格式）
		// 这是关键：必须使用转换后的格式，而不是原始的 Claude 格式
		var requestBody []byte
		if cached, exists := c.Get(KeyCodeBuddyUpstreamRequest); exists && cached != nil {
			if b, ok := cached.([]byte); ok {
				requestBody = b
				logger.LogInfo(c, "[CodeBuddy] 使用缓存的上游请求体进行重试")
			}
		}
		// 如果没有缓存，回退到原始请求体（这种情况不应该发生）
		if requestBody == nil {
			var bodyErr error
			requestBody, bodyErr = common.GetRequestBody(c)
			if bodyErr != nil {
				return &dto.Usage{}, types.NewOpenAIError(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest)
			}
			logger.LogWarn(c, "[CodeBuddy] 警告：未找到缓存的上游请求体，使用原始请求体")
		}

		// 重新发起请求
		newResp, doErr := a.DoRequest(c, info, bytes.NewReader(requestBody))
		if doErr != nil {
			logger.LogError(c, fmt.Sprintf("[CodeBuddy] 重试请求失败: %v", doErr))
			return &dto.Usage{}, types.NewOpenAIError(doErr, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}

		// 递归处理新响应
		return a.DoResponse(c, newResp.(*http.Response), info)
	}

	// 超过最大重试次数，返回错误
	logger.LogError(c, fmt.Sprintf("[CodeBuddy] 检测重试次数已达上限 (%d次)", MaxSensitiveRetries))

	// 对于 Claude 格式的请求，需要发送符合 Claude API 规范的完整事件序列
	// Claude API 要求: message_start → content_block_start → content_block_delta → content_block_stop → message_delta → message_stop
	// 只发送 error 事件会导致客户端因收到不完整的 SSE 流而断开连接
	if info.RelayFormat == types.RelayFormatClaude {
		// 确保 SSE 头部已设置
		helper.SetEventStreamHeaders(c)

		errorMessage := fmt.Sprintf("Sorry, the upstream service detected sensitive content. Request failed after %d retries. Please modify your question and try again.", MaxSensitiveRetries)
		blockIndex := 0

		// 1. message_start - 开始消息
		msgStart := &dto.ClaudeResponse{
			Type: "message_start",
			Message: &dto.ClaudeMediaMessage{
				Id:    fmt.Sprintf("msg_%d", time.Now().UnixNano()),
				Model: info.UpstreamModelName,
				Type:  "message",
				Role:  "assistant",
				Usage: &dto.ClaudeUsage{
					InputTokens:  info.GetEstimatePromptTokens(),
					OutputTokens: 0,
				},
			},
		}
		msgStart.Message.SetContent(make([]any, 0))
		_ = helper.ClaudeData(c, *msgStart)

		// 2. content_block_start - 开始内容块
		blockStart := &dto.ClaudeResponse{
			Index: &blockIndex,
			Type:  "content_block_start",
			ContentBlock: &dto.ClaudeMediaMessage{
				Type: "text",
				Text: common.GetPointer[string](""),
			},
		}
		_ = helper.ClaudeData(c, *blockStart)

		// 3. content_block_delta - 发送错误消息内容
		blockDelta := &dto.ClaudeResponse{
			Index: &blockIndex,
			Type:  "content_block_delta",
			Delta: &dto.ClaudeMediaMessage{
				Type: "text_delta",
				Text: common.GetPointer[string](errorMessage),
			},
		}
		_ = helper.ClaudeData(c, *blockDelta)

		// 4. content_block_stop - 结束内容块
		blockStop := &dto.ClaudeResponse{
			Index: &blockIndex,
			Type:  "content_block_stop",
		}
		_ = helper.ClaudeData(c, *blockStop)

		// 5. message_delta - 消息结束原因
		msgDelta := &dto.ClaudeResponse{
			Type: "message_delta",
			Delta: &dto.ClaudeMediaMessage{
				StopReason: common.GetPointer[string]("end_turn"),
			},
			Usage: &dto.ClaudeUsage{
				OutputTokens: 0,
			},
		}
		_ = helper.ClaudeData(c, *msgDelta)

		// 6. message_stop - 消息结束
		msgStop := &dto.ClaudeResponse{
			Type: "message_stop",
		}
		_ = helper.ClaudeData(c, *msgStop)
	}

	return &dto.Usage{}, types.NewOpenAIError(
		fmt.Errorf("upstream sensitive content filter triggered after %d retries", MaxSensitiveRetries),
		types.ErrorCodeSensitiveWordsDetected,
		http.StatusBadGateway,
	)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// switchToNextKey 切换到下一个可用的 API Key
// 用于敏感内容重试时尝试使用不同的 Key
func (a *Adaptor) switchToNextKey(c *gin.Context, info *relaycommon.RelayInfo) error {
	// 获取渠道信息
	channel, err := model.CacheGetChannel(info.ChannelId)
	if err != nil {
		return fmt.Errorf("获取渠道信息失败: %w", err)
	}

	// 获取下一个可用的 Key
	newKey, newIndex, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		return fmt.Errorf("获取下一个 Key 失败: %w", keyErr)
	}

	// 检查是否与当前 Key 相同（避免无效切换）
	if newKey == info.ApiKey {
		return errors.New("没有其他可用的 Key")
	}

	// 更新 info 中的 Key 信息
	oldIndex := info.ChannelMultiKeyIndex
	info.ApiKey = newKey
	info.ChannelMultiKeyIndex = newIndex

	logger.LogInfo(c, fmt.Sprintf("[CodeBuddy] 已切换 API Key: index %d -> %d", oldIndex, newIndex))
	return nil
}
