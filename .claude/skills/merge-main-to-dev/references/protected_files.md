# 关键保护文件说明

本文档详细说明合并时需要特别保护的文件及其功能。

## Claude 适配器文件

### relay/channel/claude/adaptor.go
- **功能**: Claude API 的核心适配器实现
- **关键方法**: `Init`, `GetRequestURL`, `SetupRequestHeader`, `ConvertOpenAIRequest`, `DoRequest`, `DoResponse`
- **保护原因**: 包含自定义的请求/响应转换逻辑

### relay/channel/claude/dto.go
- **功能**: Claude API 的数据传输对象定义
- **关键结构**: 请求体、响应体、消息格式
- **保护原因**: 自定义的数据结构可能与上游不同

### relay/channel/claude/relay-claude.go
- **功能**: Claude 请求的中继处理逻辑
- **关键功能**: 流式响应处理、错误处理
- **保护原因**: 包含自定义的流处理和错误处理逻辑

### relay/channel/claude/constants.go
- **功能**: Claude 相关常量定义
- **关键内容**: 模型列表、API 版本、特殊配置
- **保护原因**: 可能包含自定义的模型配置

### relay/claude_handler.go
- **功能**: Claude 请求的顶层处理器
- **关键功能**: 请求路由、格式转换入口
- **保护原因**: 自定义的处理流程

## OpenAI 适配器文件

### relay/channel/openai/adaptor.go
- **功能**: OpenAI API 的核心适配器实现
- **关键方法**: 与 Claude adaptor 类似的接口实现
- **保护原因**: 自定义的请求处理逻辑

### relay/channel/openai/relay-openai.go
- **功能**: OpenAI 请求的中继处理
- **关键功能**: 流式响应、token 计数
- **保护原因**: 自定义的响应处理

### relay/channel/openai/helper.go
- **功能**: OpenAI 相关的辅助函数
- **关键功能**: 格式转换、数据处理
- **保护原因**: 自定义的转换逻辑

### relay/channel/openai/relay_responses.go
- **功能**: OpenAI 响应处理
- **关键功能**: 响应格式化、错误处理
- **保护原因**: 自定义的响应格式

## 核心服务文件

### service/convert.go
- **功能**: 核心格式转换服务
- **关键功能**: Claude ⇄ OpenAI 格式转换、Gemini ⇄ OpenAI 转换
- **保护原因**: 这是最关键的转换逻辑文件

## 其他重要文件

### relay/channel/gemini/relay-gemini.go
- **功能**: Gemini API 中继处理
- **保护原因**: 可能包含自定义的 Gemini 转换逻辑

### relay/channel/aws/adaptor.go
- **功能**: AWS Bedrock 适配器
- **保护原因**: 自定义的 AWS 集成逻辑

### relay/compatible_handler.go
- **功能**: 兼容性处理器
- **保护原因**: 自定义的兼容性处理逻辑

## 合并策略建议

1. **绝对保护**: `service/convert.go` - 核心转换逻辑，任何冲突都保留当前分支
2. **高度保护**: Claude 和 OpenAI 适配器文件 - 冲突时优先保留当前分支
3. **审慎合并**: 其他适配器文件 - 需要人工审查冲突内容
4. **可接受更新**: 非核心文件 - 可以接受 main 分支的更新
