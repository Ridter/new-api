package codebuddy

// ModelList 是 CodeBuddy 渠道的默认模型列表
// 这个列表会在无法从 API 获取模型时作为后备使用
// 实际模型列表应通过 FetchCodeBuddyModels 从 API 动态获取
var ModelList = []string{
	// craft agent 模型
	"glm-4.7-ioa",
	"glm-4.6v-ioa",
	"kimi-k2-thinking",
	"claude-opus-4.5",
	"claude-haiku-4.5",
	"claude-4.5",
	"claude-4.0",
	"gpt-5.2",
	"gpt-5.1",
	"gpt-5.1-codex",
	"gpt-5.1-codex-max",
	"deepseek-v3-2-volc-ioa",
	"gemini-3.0-flash",
	"hunyuan-2.0-instruct-ioa",
	// 额外的固定模型
	"claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
	"claude-sonnet-4-5",
	"claude-haiku-4-5",
	"claude-sonnet-4-20250514",
}

var ChannelName = "CodeBuddy"
