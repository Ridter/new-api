package codebuddy

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// JWTPayload 用于解析 JWT token 的 payload 部分
type JWTPayload struct {
	Sub string `json:"sub"` // 用户ID
	Iss string `json:"iss"` // issuer，包含企业ID
}

// parseJWTPayload 从 JWT token 中解析 payload（不验证签名）
func parseJWTPayload(token string) (*JWTPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT token format")
	}

	// 解码 payload (第二部分)
	payload := parts[1]
	// 添加 base64 padding
	if padding := len(payload) % 4; padding != 0 {
		payload += strings.Repeat("=", 4-padding)
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %v", err)
	}

	var result JWTPayload
	err = common.Unmarshal(decoded, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT payload: %v", err)
	}

	return &result, nil
}

// extractEnterpriseID 从 JWT issuer 中提取企业ID
// issuer 格式: https://xxx.sso.copilot.tencent.com/auth/realms/sso-{enterprise_id}
func extractEnterpriseID(issuer string) string {
	if idx := strings.LastIndex(issuer, "sso-"); idx != -1 {
		return issuer[idx+4:]
	}
	return ""
}

// CodeBuddyConfigResponse 表示 CodeBuddy /v3/config API 的响应结构
type CodeBuddyConfigResponse struct {
	Code      int                    `json:"code"`
	Msg       string                 `json:"msg"`
	RequestId string                 `json:"requestId"`
	Data      CodeBuddyConfigData    `json:"data"`
}

// CodeBuddyConfigData 表示配置数据
type CodeBuddyConfigData struct {
	Agents []CodeBuddyAgent `json:"agents"`
	Models []CodeBuddyModel `json:"models"`
}

// CodeBuddyAgent 表示 agent 配置
type CodeBuddyAgent struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Models      []string `json:"models"`
}

// CodeBuddyModel 表示模型配置
type CodeBuddyModel struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	DescriptionEn      string `json:"descriptionEn"`
	DescriptionZh      string `json:"descriptionZh"`
	MaxInputTokens     int    `json:"maxInputTokens"`
	MaxOutputTokens    int    `json:"maxOutputTokens"`
	SupportsImages     bool   `json:"supportsImages"`
	SupportsToolCall   bool   `json:"supportsToolCall"`
	SupportsReasoning  bool   `json:"supportsReasoning"`
	Vendor             string `json:"vendor"`
}

// AdditionalModels 是需要额外添加的固定模型列表
var AdditionalModels = []string{
	"claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
	"claude-sonnet-4-5",
	"claude-haiku-4-5",
	"claude-sonnet-4-20250514",
}

// FetchCodeBuddyModels 从 CodeBuddy API 获取模型列表
// 获取 agents 中 name 为 "craft" 的 models，并添加额外的固定模型
func FetchCodeBuddyModels(baseURL, apiKey string, headerOverride map[string]any) ([]string, error) {
	url := fmt.Sprintf("%s/v3/config", strings.TrimSuffix(baseURL, "/"))

	client := &http.Client{}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置必需的 User-Agent（API 要求包含 CodeBuddyIDE）
	request.Header.Set("User-Agent", "CodeBuddyIDE/1.0.0")

	// 设置 Authorization
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)

		// 从 JWT token 中解析 X-User-Id 和 X-Enterprise-Id
		jwtPayload, err := parseJWTPayload(apiKey)
		if err == nil {
			if jwtPayload.Sub != "" {
				request.Header.Set("X-User-Id", jwtPayload.Sub)
			}
			if enterpriseID := extractEnterpriseID(jwtPayload.Iss); enterpriseID != "" {
				request.Header.Set("X-Enterprise-Id", enterpriseID)
			}
		}
	}

	// 应用自定义 header 覆盖（渠道配置可以覆盖自动设置的值）
	for k, v := range headerOverride {
		if str, ok := v.(string); ok {
			if strings.Contains(str, "{api_key}") {
				str = strings.ReplaceAll(str, "{api_key}", apiKey)
			}
			request.Header.Set(k, str)
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, string(body))
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var configResp CodeBuddyConfigResponse
	err = common.Unmarshal(body, &configResp)
	if err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if configResp.Code != 0 {
		return nil, fmt.Errorf("API 返回错误: code=%d, msg=%s", configResp.Code, configResp.Msg)
	}

	// 查找 name 为 "craft" 的 agent
	var craftModels []string
	for _, agent := range configResp.Data.Agents {
		if agent.Name == "craft" {
			craftModels = agent.Models
			break
		}
	}

	if len(craftModels) == 0 {
		return nil, fmt.Errorf("未找到 craft agent 的模型列表")
	}

	// 使用 map 去重
	modelSet := make(map[string]bool)
	for _, model := range craftModels {
		modelSet[model] = true
	}

	// 添加额外的固定模型
	for _, model := range AdditionalModels {
		modelSet[model] = true
	}

	// 转换为切片
	result := make([]string, 0, len(modelSet))
	for model := range modelSet {
		result = append(result, model)
	}

	return result, nil
}

// FetchCodeBuddyModelsWithMetadata 获取模型列表及其元数据
func FetchCodeBuddyModelsWithMetadata(baseURL, apiKey string, headerOverride map[string]any) ([]CodeBuddyModel, error) {
	url := fmt.Sprintf("%s/v3/config", strings.TrimSuffix(baseURL, "/"))

	client := &http.Client{}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置必需的 User-Agent（API 要求包含 CodeBuddyIDE）
	request.Header.Set("User-Agent", "CodeBuddyIDE/1.0.0")

	// 设置 Authorization
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)

		// 从 JWT token 中解析 X-User-Id 和 X-Enterprise-Id
		jwtPayload, err := parseJWTPayload(apiKey)
		if err == nil {
			if jwtPayload.Sub != "" {
				request.Header.Set("X-User-Id", jwtPayload.Sub)
			}
			if enterpriseID := extractEnterpriseID(jwtPayload.Iss); enterpriseID != "" {
				request.Header.Set("X-Enterprise-Id", enterpriseID)
			}
		}
	}

	// 应用自定义 header 覆盖（渠道配置可以覆盖自动设置的值）
	for k, v := range headerOverride {
		if str, ok := v.(string); ok {
			if strings.Contains(str, "{api_key}") {
				str = strings.ReplaceAll(str, "{api_key}", apiKey)
			}
			request.Header.Set(k, str)
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, string(body))
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var configResp CodeBuddyConfigResponse
	err = common.Unmarshal(body, &configResp)
	if err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if configResp.Code != 0 {
		return nil, fmt.Errorf("API 返回错误: code=%d, msg=%s", configResp.Code, configResp.Msg)
	}

	// 查找 name 为 "craft" 的 agent 获取模型 ID 列表
	craftModelSet := make(map[string]bool)
	for _, agent := range configResp.Data.Agents {
		if agent.Name == "craft" {
			for _, model := range agent.Models {
				craftModelSet[model] = true
			}
			break
		}
	}

	// 添加额外的固定模型
	for _, model := range AdditionalModels {
		craftModelSet[model] = true
	}

	// 从 models 列表中筛选出 craft agent 使用的模型
	var result []CodeBuddyModel
	for _, model := range configResp.Data.Models {
		if craftModelSet[model.ID] {
			result = append(result, model)
			delete(craftModelSet, model.ID) // 标记已找到
		}
	}

	// 对于额外添加的模型，如果在 models 列表中没有找到，创建基本信息
	for modelID := range craftModelSet {
		result = append(result, CodeBuddyModel{
			ID:   modelID,
			Name: modelID,
		})
	}

	return result, nil
}
