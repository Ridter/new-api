package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ClaudeCountTokensRequest 定义 count_tokens 请求结构
type ClaudeCountTokensRequest struct {
	Model    string            `json:"model"`
	Messages []dto.ClaudeMessage `json:"messages"`
	System   any               `json:"system,omitempty"`
	Tools    any               `json:"tools,omitempty"`
}

// ClaudeCountTokensResponse 定义 count_tokens 响应结构
type ClaudeCountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// ClaudeCountTokens 处理 /v1/messages/count_tokens 请求
// 计算 Claude 格式消息的 token 数量
func ClaudeCountTokens(c *gin.Context) {
	var request ClaudeCountTokensRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Invalid request body: " + err.Error(),
			},
		})
		return
	}

	// 构建 ClaudeRequest 以复用现有的 token 计算逻辑
	claudeRequest := &dto.ClaudeRequest{
		Model:    request.Model,
		Messages: request.Messages,
		System:   request.System,
		Tools:    request.Tools,
	}

	// 获取 token 计算元数据
	meta := claudeRequest.GetTokenCountMeta()

	// 计算 token 数量
	tokenCount := service.CountTextToken(meta.CombineText, request.Model)

	// 添加 tools 的额外 token（每个 tool 约 8 个 token 的格式化开销）
	tokenCount += meta.ToolsCount * 8

	// 添加 messages 的格式化 token（每条消息约 3 个 token）
	tokenCount += meta.MessagesCount * 3

	c.JSON(http.StatusOK, ClaudeCountTokensResponse{
		InputTokens: tokenCount,
	})
}
