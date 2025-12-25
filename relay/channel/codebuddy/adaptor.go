package codebuddy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const SensitiveContentMessage = "系统检测到您当前输入的信息存在敏感内容"

// 最大重试次数
const MaxSensitiveRetries = 3

// 检测敏感内容的最大字节数（只检测响应开头部分）
const SensitiveCheckMaxBytes = 4096

// 敏感内容检测的最大累积内容长度（字符数）
// 敏感消息通常很短，超过这个长度就不再检测
const SensitiveCheckMaxContentLength = 200

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
		return nil, nil
	default:
	}

	// 使用流式检测：先预读取前 N 个字节来快速检测敏感内容
	// 如果敏感消息在开头，可以快速检测到
	peekReader := bufio.NewReaderSize(resp.Body, SensitiveCheckMaxBytes)
	peekedData, peekErr := peekReader.Peek(SensitiveCheckMaxBytes)

	// Peek 可能返回 EOF 或其他错误，但只要有数据就继续处理
	if peekErr != nil && peekErr != io.EOF && peekErr != bufio.ErrBufferFull {
		// 检查是否是客户端断开
		select {
		case <-c.Request.Context().Done():
			resp.Body.Close()
			return nil, nil
		default:
		}
		// 如果没有读取到任何数据，返回错误
		if len(peekedData) == 0 {
			resp.Body.Close()
			return nil, types.NewOpenAIError(peekErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}

	// 检查预读取的数据是否包含敏感内容（快速路径）
	if strings.Contains(string(peekedData), SensitiveContentMessage) {
		resp.Body.Close()
		return a.handleSensitiveRetry(c, info, string(peekedData))
	}

	// 检查客户端状态
	select {
	case <-c.Request.Context().Done():
		resp.Body.Close()
		return nil, nil
	default:
	}

	// peekReader 内部已经缓冲了数据，直接用它作为新的 Body
	resp.Body = &wrappedReadCloser{
		Reader: peekReader,
		Closer: resp.Body,
	}

	// 使用自定义流处理器，在流式传输过程中检测敏感内容
	return a.streamWithSensitiveDetection(c, resp, info)
}

// streamWithSensitiveDetection 流式处理响应，同时检测敏感内容
// 策略：先缓冲前 N 个字符的内容进行检测，检测通过后再一次性发送缓冲数据并继续流式传输
func (a *Adaptor) streamWithSensitiveDetection(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
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

	// 敏感内容检测相关
	var contentBuilder strings.Builder
	var sensitiveDetected bool
	var detectedContent string
	var mu sync.Mutex

	// 缓冲相关：在检测阶段缓冲数据，检测通过后再发送
	var bufferedItems []string
	var detectionPassed bool // 检测是否已通过（累积内容超过阈值且未检测到敏感内容）

	// 设置 SSE 响应头（延迟到确认安全后再设置）
	var headersSet bool

	helper.StreamScannerHandler(c, resp, info, func(data string) bool {
		mu.Lock()
		defer mu.Unlock()

		// 如果已经检测到敏感内容，停止处理
		if sensitiveDetected {
			return false
		}

		if len(data) > 0 {
			streamItems = append(streamItems, data)
		}

		// 检测阶段：累积内容并检测
		if !detectionPassed {
			// 解析流数据，提取内容用于敏感检测
			var streamResp dto.ChatCompletionsStreamResponse
			if err := common.Unmarshal(common.StringToByteSlice(data), &streamResp); err == nil {
				for _, choice := range streamResp.Choices {
					content := choice.Delta.GetContentString()
					if content != "" {
						contentBuilder.WriteString(content)
					}
				}
			}

			// 检测累积的内容是否包含敏感词
			if strings.Contains(contentBuilder.String(), SensitiveContentMessage) {
				sensitiveDetected = true
				detectedContent = contentBuilder.String()
				return false // 停止处理，准备重试
			}

			// 缓冲当前数据
			if len(data) > 0 {
				bufferedItems = append(bufferedItems, data)
			}

			// 检查是否超过检测阈值
			if contentBuilder.Len() >= SensitiveCheckMaxContentLength {
				// 检测通过，发送所有缓冲的数据
				detectionPassed = true

				// 现在才设置 SSE 响应头
				if !headersSet {
					helper.SetEventStreamHeaders(c)
					headersSet = true
				}

				// 发送所有缓冲的数据（除了最后一条，保持 lastStreamData 逻辑）
				for i, item := range bufferedItems {
					if i < len(bufferedItems)-1 {
						err := openai.HandleStreamFormat(c, info, item, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
						if err != nil {
							common.SysLog("error handling stream format: " + err.Error())
						}
					} else {
						lastStreamData = item
					}
				}
				bufferedItems = nil // 清空缓冲
			}
			return true
		}

		// 检测已通过，正常流式传输
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

	// 检查是否检测到敏感内容
	if sensitiveDetected {
		logger.LogWarn(c, fmt.Sprintf("[CodeBuddy] 流式检测到敏感内容: %s", detectedContent))
		return a.handleSensitiveRetry(c, info, detectedContent)
	}

	// 如果响应很短（未达到检测阈值就结束了），需要发送缓冲的数据
	if !detectionPassed && len(bufferedItems) > 0 {
		// 检测通过（响应结束且未检测到敏感内容）
		if !headersSet {
			helper.SetEventStreamHeaders(c)
		}

		// 发送所有缓冲的数据（除了最后一条）
		for i, item := range bufferedItems {
			if i < len(bufferedItems)-1 {
				err := openai.HandleStreamFormat(c, info, item, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
				if err != nil {
					common.SysLog("error handling stream format: " + err.Error())
				}
			} else {
				lastStreamData = item
			}
		}
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

		// 获取原始请求体并重新发起请求
		requestBody, bodyErr := common.GetRequestBody(c)
		if bodyErr != nil {
			return &dto.Usage{}, types.NewOpenAIError(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest)
		}

		// 重新发起请求
		c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		newResp, doErr := a.DoRequest(c, info, bytes.NewBuffer(requestBody))
		if doErr != nil {
			return &dto.Usage{}, types.NewOpenAIError(doErr, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}

		// 递归处理新响应
		return a.DoResponse(c, newResp.(*http.Response), info)
	}

	// 超过最大重试次数，返回错误
	logger.LogError(c, fmt.Sprintf("[CodeBuddy] 检测重试次数已达上限 (%d次)", MaxSensitiveRetries))
	return &dto.Usage{}, types.NewOpenAIError(
		fmt.Errorf("upstream sensitive content filter triggered after %d retries", MaxSensitiveRetries),
		types.ErrorCodeSensitiveWordsDetected,
		http.StatusBadGateway,
	)
}

// wrappedReadCloser 包装 Reader 和 Closer
type wrappedReadCloser struct {
	io.Reader
	io.Closer
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
