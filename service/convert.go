package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func ClaudeToOpenAIRequest(claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	openAIRequest := dto.GeneralOpenAIRequest{
		Model:       claudeRequest.Model,
		MaxTokens:   claudeRequest.MaxTokens,
		Temperature: claudeRequest.Temperature,
		TopP:        claudeRequest.TopP,
		Stream:      claudeRequest.Stream,
	}

	isOpenRouter := info.ChannelType == constant.ChannelTypeOpenRouter

	if claudeRequest.Thinking != nil && claudeRequest.Thinking.Type == "enabled" {
		if isOpenRouter {
			reasoning := openrouter.RequestReasoning{
				MaxTokens: claudeRequest.Thinking.GetBudgetTokens(),
			}
			reasoningJSON, err := json.Marshal(reasoning)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
			}
			openAIRequest.Reasoning = reasoningJSON
		} else {
			// 对于非 OpenRouter 渠道，使用 reasoning_effort 参数
			// 根据 budget_tokens 动态确定 reasoning_effort 级别
			// 参考 claude-code-router 的 getThinkLevel 逻辑
			budgetTokens := claudeRequest.Thinking.GetBudgetTokens()
			openAIRequest.ReasoningEffort = getReasoningEffort(budgetTokens)

			// 注意：reasoning_effort 与 max_tokens 不能同时使用，会导致 500 错误
			// 因此需要清除 max_tokens
			openAIRequest.MaxTokens = 0

			// 如果原始模型名称有 -thinking 后缀，也添加到上游模型
			thinkingSuffix := "-thinking"
			if strings.HasSuffix(info.OriginModelName, thinkingSuffix) &&
				!strings.HasSuffix(openAIRequest.Model, thinkingSuffix) {
				openAIRequest.Model = openAIRequest.Model + thinkingSuffix
			}
		}
	}

	// Convert stop sequences
	if len(claudeRequest.StopSequences) == 1 {
		openAIRequest.Stop = claudeRequest.StopSequences[0]
	} else if len(claudeRequest.StopSequences) > 1 {
		openAIRequest.Stop = claudeRequest.StopSequences
	}

	// Convert tools
	tools, _ := common.Any2Type[[]dto.Tool](claudeRequest.Tools)
	openAITools := make([]dto.ToolCallRequest, 0)
	for _, claudeTool := range tools {
		openAITool := dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        claudeTool.Name,
				Description: claudeTool.Description,
				Parameters:  claudeTool.InputSchema,
			},
		}
		openAITools = append(openAITools, openAITool)
	}
	openAIRequest.Tools = openAITools

	// Convert messages
	openAIMessages := make([]dto.Message, 0)

	// Add system message if present
	if claudeRequest.System != nil {
		if claudeRequest.IsStringSystem() && claudeRequest.GetStringSystem() != "" {
			openAIMessage := dto.Message{
				Role: "system",
			}
			openAIMessage.SetStringContent(claudeRequest.GetStringSystem())
			openAIMessages = append(openAIMessages, openAIMessage)
		} else {
			systems := claudeRequest.ParseSystem()
			if len(systems) > 0 {
				openAIMessage := dto.Message{
					Role: "system",
				}
				isOpenRouterClaude := isOpenRouter && strings.HasPrefix(info.UpstreamModelName, "anthropic/claude")
				if isOpenRouterClaude {
					systemMediaMessages := make([]dto.MediaContent, 0, len(systems))
					for _, system := range systems {
						message := dto.MediaContent{
							Type:         "text",
							Text:         system.GetText(),
							CacheControl: system.CacheControl,
						}
						systemMediaMessages = append(systemMediaMessages, message)
					}
					openAIMessage.SetMediaContent(systemMediaMessages)
				} else {
					systemStr := ""
					for _, system := range systems {
						if system.Text != nil {
							systemStr += *system.Text
						}
					}
					openAIMessage.SetStringContent(systemStr)
				}
				openAIMessages = append(openAIMessages, openAIMessage)
			}
		}
	}
	for _, claudeMessage := range claudeRequest.Messages {
		openAIMessage := dto.Message{
			Role: claudeMessage.Role,
		}

		//log.Printf("claudeMessage.Content: %v", claudeMessage.Content)
		if claudeMessage.IsStringContent() {
			openAIMessage.SetStringContent(claudeMessage.GetStringContent())
		} else {
			content, err := claudeMessage.ParseContent()
			if err != nil {
				return nil, err
			}
			contents := content
			var toolCalls []dto.ToolCallRequest
			mediaMessages := make([]dto.MediaContent, 0, len(contents))

			for _, mediaMsg := range contents {
				switch mediaMsg.Type {
				case "text":
					message := dto.MediaContent{
						Type:         "text",
						Text:         mediaMsg.GetText(),
						CacheControl: mediaMsg.CacheControl,
					}
					mediaMessages = append(mediaMessages, message)
				case "image":
					// Handle image conversion (base64 to URL or keep as is)
					imageData := fmt.Sprintf("data:%s;base64,%s", mediaMsg.Source.MediaType, mediaMsg.Source.Data)
					//textContent += fmt.Sprintf("[Image: %s]", imageData)
					mediaMessage := dto.MediaContent{
						Type:     "image_url",
						ImageUrl: &dto.MessageImageUrl{Url: imageData},
					}
					mediaMessages = append(mediaMessages, mediaMessage)
				case "tool_use":
					toolCall := dto.ToolCallRequest{
						ID:   mediaMsg.Id,
						Type: "function",
						Function: dto.FunctionRequest{
							Name:      mediaMsg.Name,
							Arguments: toJSONString(mediaMsg.Input),
						},
					}
					toolCalls = append(toolCalls, toolCall)
				case "tool_result":
					// Add tool result as a separate message
					toolName := mediaMsg.Name
					if toolName == "" {
						toolName = claudeRequest.SearchToolNameByToolCallId(mediaMsg.ToolUseId)
					}
					oaiToolMessage := dto.Message{
						Role:       "tool",
						Name:       &toolName,
						ToolCallId: mediaMsg.ToolUseId,
					}
					//oaiToolMessage.SetStringContent(*mediaMsg.GetMediaContent().Text)
					if mediaMsg.IsStringContent() {
						oaiToolMessage.SetStringContent(mediaMsg.GetStringContent())
					} else {
						mediaContents := mediaMsg.ParseMediaContent()
						encodeJson, _ := common.Marshal(mediaContents)
						oaiToolMessage.SetStringContent(string(encodeJson))
					}
					openAIMessages = append(openAIMessages, oaiToolMessage)
				case "thinking":
					// Claude thinking 映射到 OpenAI 的 reasoning_content
					// 同时保留 signature 用于多轮对话的 extended thinking 透传
					if mediaMsg.Thinking != nil {
						openAIMessage.ReasoningContent = *mediaMsg.Thinking
					}
					// signature 需要单独保存用于透传
					if mediaMsg.Signature != nil {
						openAIMessage.Thinking = &dto.ThinkingContent{
							Content:   openAIMessage.ReasoningContent,
							Signature: *mediaMsg.Signature,
						}
					}
				}
			}

			if len(toolCalls) > 0 {
				openAIMessage.SetToolCalls(toolCalls)
			}

			if len(mediaMessages) > 0 {
				// For assistant messages with tool_calls, use string content format
				// to ensure compatibility with OpenAI API providers
				// Array format content may be ignored by some providers when tool_calls exist
				if claudeMessage.Role == "assistant" && len(toolCalls) > 0 {
					// Check if mediaMessages contains only text (no images)
					onlyText := true
					var textContent string
					for _, media := range mediaMessages {
						if media.Type == "text" {
							textContent += media.Text
						} else {
							onlyText = false
							break
						}
					}
					if onlyText && textContent != "" {
						openAIMessage.SetStringContent(textContent)
					} else {
						openAIMessage.SetMediaContent(mediaMessages)
					}
				} else {
					openAIMessage.SetMediaContent(mediaMessages)
				}
			}
		}
		if len(openAIMessage.ParseContent()) > 0 || len(openAIMessage.ToolCalls) > 0 {
			openAIMessages = append(openAIMessages, openAIMessage)
		}
	}

	openAIRequest.Messages = openAIMessages

	return &openAIRequest, nil
}

func generateStopBlock(index int, info *relaycommon.RelayInfo) *dto.ClaudeResponse {
	// Safety check: only generate stop block if a content block has been started
	if info != nil && !info.ClaudeConvertInfo.HasContentBlockStarted {
		return nil
	}
	return &dto.ClaudeResponse{
		Type:  "content_block_stop",
		Index: common.GetPointer[int](index),
	}
}

func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	var claudeResponses []*dto.ClaudeResponse

	// Initialize tool call index mapping if not exists
	if info.ClaudeConvertInfo.ToolCallIndexToContentIndex == nil {
		info.ClaudeConvertInfo.ToolCallIndexToContentIndex = make(map[int]int)
	}

	// Initialize CurrentContentBlockIndex if not set
	if info.SendResponseCount == 0 {
		info.ClaudeConvertInfo.CurrentContentBlockIndex = -1
	}

	// First response: Send message_start
	if info.SendResponseCount == 1 {
		msg := &dto.ClaudeMediaMessage{
			Id:    openAIResponse.Id,
			Model: openAIResponse.Model,
			Type:  "message",
			Role:  "assistant",
			Usage: &dto.ClaudeUsage{
				InputTokens:  info.GetEstimatePromptTokens(),
				OutputTokens: 0,
			},
		}
		msg.SetContent(make([]any, 0))
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Type:    "message_start",
			Message: msg,
		})

		// Handle first response with tool calls
		if openAIResponse.IsToolCall() {
			firstToolCall := openAIResponse.GetFirstToolCall()
			toolCallIndex := 0
			if firstToolCall.Index != nil {
				toolCallIndex = *firstToolCall.Index
			}

			// Map tool call index to content block index
			info.ClaudeConvertInfo.ToolCallIndexToContentIndex[toolCallIndex] = info.ClaudeConvertInfo.Index
			info.ClaudeConvertInfo.CurrentContentBlockIndex = info.ClaudeConvertInfo.Index
			info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeTools

			resp := &dto.ClaudeResponse{
				Type: "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Id:    firstToolCall.ID,
					Type:  "tool_use",
					Name:  firstToolCall.Function.Name,
					Input: map[string]interface{}{},
				},
			}
			resp.SetIndex(info.ClaudeConvertInfo.Index)
			claudeResponses = append(claudeResponses, resp)
			info.ClaudeConvertInfo.HasContentBlockStarted = true
		}

		// Handle first response with text content (non-standard OpenAI response)
		if len(openAIResponse.Choices) > 0 && len(openAIResponse.Choices[0].Delta.GetContentString()) > 0 {
			// Close previous block if needed
			// Note: In the first response, if we just started a tool call block above,
			// we should NOT close it here. Only close if there was a pre-existing block
			// from a previous response (which shouldn't happen for SendResponseCount == 1).
			// The check for LastMessagesType ensures we don't close a just-started tool block.
			if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 &&
				info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeText &&
				info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeTools {
				// Only close if it's a thinking block that was somehow started before
				if stopBlock := generateStopBlock(info.ClaudeConvertInfo.CurrentContentBlockIndex, info); stopBlock != nil {
					claudeResponses = append(claudeResponses, stopBlock)
				}
				info.ClaudeConvertInfo.Index++
			} else if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeTools {
				// If we just started a tool call block in this same response,
				// we need to increment the index for the text block but NOT close the tool block yet
				// (it will be closed when we switch away from it in subsequent responses)
				info.ClaudeConvertInfo.Index++
			}

			info.ClaudeConvertInfo.CurrentContentBlockIndex = info.ClaudeConvertInfo.Index
			info.ClaudeConvertInfo.HasTextContentStarted = true
			info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText

			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &info.ClaudeConvertInfo.Index,
				Type:  "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Type: "text",
					Text: common.GetPointer[string](""),
				},
			})
			info.ClaudeConvertInfo.HasContentBlockStarted = true
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &info.ClaudeConvertInfo.Index,
				Type:  "content_block_delta",
				Delta: &dto.ClaudeMediaMessage{
					Type: "text_delta",
					Text: common.GetPointer[string](openAIResponse.Choices[0].Delta.GetContentString()),
				},
			})
		}
		return claudeResponses
	}

	// Handle no choices (empty response or done)
	if len(openAIResponse.Choices) == 0 {
		if info.ClaudeConvertInfo.Done {
			// Close any open content block
			if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 {
				// For thinking blocks, send signature_delta first if available
				if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeThinking &&
					info.ClaudeConvertInfo.ThinkingSignature != "" {
					thinkingBlockIndex := info.ClaudeConvertInfo.CurrentContentBlockIndex
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: common.GetPointer[int](thinkingBlockIndex),
						Type:  "content_block_delta",
						Delta: &dto.ClaudeMediaMessage{
							Type:      "signature_delta",
							Signature: &info.ClaudeConvertInfo.ThinkingSignature,
						},
					})
				}
				if stopBlock := generateStopBlock(info.ClaudeConvertInfo.CurrentContentBlockIndex, info); stopBlock != nil {
					claudeResponses = append(claudeResponses, stopBlock)
				}
				info.ClaudeConvertInfo.CurrentContentBlockIndex = -1
			}

			// Send usage and stop
			oaiUsage := info.ClaudeConvertInfo.Usage
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type: "message_delta",
					Usage: &dto.ClaudeUsage{
						InputTokens:              oaiUsage.PromptTokens,
						OutputTokens:             oaiUsage.CompletionTokens,
						CacheCreationInputTokens: oaiUsage.PromptTokensDetails.CachedCreationTokens,
						CacheReadInputTokens:     oaiUsage.PromptTokensDetails.CachedTokens,
					},
					Delta: &dto.ClaudeMediaMessage{
						StopReason: common.GetPointer[string](stopReasonOpenAI2Claude(info.FinishReason)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
		}
		return claudeResponses
	}

	chosenChoice := openAIResponse.Choices[0]

	// Capture signature if present (even when finish_reason is set)
	// This ensures we don't miss the signature when it arrives with finish_reason
	thinkingSignature := chosenChoice.Delta.GetThinkingSignature()
	if thinkingSignature != "" {
		info.ClaudeConvertInfo.ThinkingSignature = thinkingSignature
	}

	// Check for finish reason
	// Note: When finish_reason is received, we should NOT return early if Done is false.
	// Instead, we should continue processing any remaining content (like signature) in this response,
	// and let HandleFinalResponse handle the final message_stop event.
	if chosenChoice.FinishReason != nil && *chosenChoice.FinishReason != "" {
		info.FinishReason = *chosenChoice.FinishReason
		// Don't return early here - continue processing to handle any content in this response
	}

	// Handle done state
	if info.ClaudeConvertInfo.Done {
		// Close any open content block
		if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 {
			// For thinking blocks, send signature_delta first if available
			if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeThinking &&
				info.ClaudeConvertInfo.ThinkingSignature != "" {
				thinkingBlockIndex := info.ClaudeConvertInfo.CurrentContentBlockIndex
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: common.GetPointer[int](thinkingBlockIndex),
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type:      "signature_delta",
						Signature: &info.ClaudeConvertInfo.ThinkingSignature,
					},
				})
			}
			// For tool blocks, close the last tool call block
			if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeTools &&
				info.ClaudeConvertInfo.LastToolCallIndex >= 0 {
				prevBlockIndex := info.ClaudeConvertInfo.ToolCallIndexToContentIndex[info.ClaudeConvertInfo.LastToolCallIndex]
				if stopBlock := generateStopBlock(prevBlockIndex, info); stopBlock != nil {
					claudeResponses = append(claudeResponses, stopBlock)
				}
			} else {
				if stopBlock := generateStopBlock(info.ClaudeConvertInfo.CurrentContentBlockIndex, info); stopBlock != nil {
					claudeResponses = append(claudeResponses, stopBlock)
				}
			}
			info.ClaudeConvertInfo.CurrentContentBlockIndex = -1
		}

		oaiUsage := info.ClaudeConvertInfo.Usage
		if oaiUsage != nil {
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_delta",
				Usage: &dto.ClaudeUsage{
					InputTokens:              oaiUsage.PromptTokens,
					OutputTokens:             oaiUsage.CompletionTokens,
					CacheCreationInputTokens: oaiUsage.PromptTokensDetails.CachedCreationTokens,
					CacheReadInputTokens:     oaiUsage.PromptTokensDetails.CachedTokens,
				},
				Delta: &dto.ClaudeMediaMessage{
					StopReason: common.GetPointer[string](stopReasonOpenAI2Claude(info.FinishReason)),
				},
			})
		}
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Type: "message_stop",
		})
		return claudeResponses
	}

	// Handle tool calls - IMPROVED: Support multiple tool calls with index mapping
	if len(chosenChoice.Delta.ToolCalls) > 0 {
		for _, toolCall := range chosenChoice.Delta.ToolCalls {
			toolCallIndex := 0
			if toolCall.Index != nil {
				toolCallIndex = *toolCall.Index
			}

			// Check if this is a new tool call
			contentBlockIndex, exists := info.ClaudeConvertInfo.ToolCallIndexToContentIndex[toolCallIndex]
			if !exists {
				// Close previous tool call block if switching to a new tool call
				if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeTools &&
					info.ClaudeConvertInfo.LastToolCallIndex >= 0 &&
					info.ClaudeConvertInfo.LastToolCallIndex != toolCallIndex {
					prevBlockIndex := info.ClaudeConvertInfo.ToolCallIndexToContentIndex[info.ClaudeConvertInfo.LastToolCallIndex]
					if stopBlock := generateStopBlock(prevBlockIndex, info); stopBlock != nil {
						claudeResponses = append(claudeResponses, stopBlock)
					}
				} else if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 &&
					info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeTools {
					// Close previous non-tool content block (thinking/text)
					// For thinking blocks, send signature_delta first if available
					if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeThinking &&
						info.ClaudeConvertInfo.ThinkingSignature != "" {
						thinkingBlockIndex := info.ClaudeConvertInfo.CurrentContentBlockIndex
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: common.GetPointer[int](thinkingBlockIndex),
							Type:  "content_block_delta",
							Delta: &dto.ClaudeMediaMessage{
								Type:      "signature_delta",
								Signature: &info.ClaudeConvertInfo.ThinkingSignature,
							},
						})
					}
					if stopBlock := generateStopBlock(info.ClaudeConvertInfo.CurrentContentBlockIndex, info); stopBlock != nil {
						claudeResponses = append(claudeResponses, stopBlock)
					}
				}

				// Start new tool use block
				// Only increment Index if we already have a content block
				if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 {
					info.ClaudeConvertInfo.Index++
				}
				contentBlockIndex = info.ClaudeConvertInfo.Index
				info.ClaudeConvertInfo.ToolCallIndexToContentIndex[toolCallIndex] = contentBlockIndex
				info.ClaudeConvertInfo.CurrentContentBlockIndex = contentBlockIndex
				info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeTools
				info.ClaudeConvertInfo.LastToolCallIndex = toolCallIndex

				toolCallID := toolCall.ID
				if toolCallID == "" {
					toolCallID = "call_" + string(rune(toolCallIndex))
				}
				toolCallName := toolCall.Function.Name
				if toolCallName == "" {
					toolCallName = "tool_" + string(rune(toolCallIndex))
				}

				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &contentBlockIndex,
					Type:  "content_block_start",
					ContentBlock: &dto.ClaudeMediaMessage{
						Id:    toolCallID,
						Type:  "tool_use",
						Name:  toolCallName,
						Input: map[string]interface{}{},
					},
				})
				info.ClaudeConvertInfo.HasContentBlockStarted = true
			}

			// Send tool arguments delta
			if toolCall.Function.Arguments != "" {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &contentBlockIndex,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type:        "input_json_delta",
						PartialJson: &toolCall.Function.Arguments,
					},
				})
			}
		}
		return claudeResponses
	}

	// Handle thinking content - IMPROVED: Support thinking with signature
	reasoning := chosenChoice.Delta.GetReasoningContent()

	// Note: thinkingSignature is already captured above (line 352-355) to handle
	// the case where signature arrives with finish_reason

	if reasoning != "" {
		// Close previous block if switching from non-thinking
		if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 &&
			info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeThinking {
			if stopBlock := generateStopBlock(info.ClaudeConvertInfo.CurrentContentBlockIndex, info); stopBlock != nil {
				claudeResponses = append(claudeResponses, stopBlock)
			}
			info.ClaudeConvertInfo.Index++
		}

		// Start thinking block if not started
		if !info.ClaudeConvertInfo.HasThinkingStarted {
			info.ClaudeConvertInfo.CurrentContentBlockIndex = info.ClaudeConvertInfo.Index
			info.ClaudeConvertInfo.HasThinkingStarted = true
			info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeThinking

			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &info.ClaudeConvertInfo.Index,
				Type:  "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Type:     "thinking",
					Thinking: common.GetPointer[string](""),
				},
			})
			info.ClaudeConvertInfo.HasContentBlockStarted = true
		}

		// Send thinking delta
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Index: &info.ClaudeConvertInfo.Index,
			Type:  "content_block_delta",
			Delta: &dto.ClaudeMediaMessage{
				Type:     "thinking_delta",
				Thinking: &reasoning,
			},
		})
		return claudeResponses
	}

	// Handle text content - IMPROVED: Precise content block switching
	textContent := chosenChoice.Delta.GetContentString()
	if textContent != "" {
		// Close previous block if switching from thinking/tools to text
		if info.ClaudeConvertInfo.CurrentContentBlockIndex >= 0 {
			isCurrentTextBlock := info.ClaudeConvertInfo.HasTextContentStarted &&
				info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeText
			if !isCurrentTextBlock {
				// For thinking blocks, send signature_delta first if available
				if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeThinking &&
					info.ClaudeConvertInfo.ThinkingSignature != "" {
					thinkingBlockIndex := info.ClaudeConvertInfo.CurrentContentBlockIndex
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: common.GetPointer[int](thinkingBlockIndex),
						Type:  "content_block_delta",
						Delta: &dto.ClaudeMediaMessage{
							Type:      "signature_delta",
							Signature: &info.ClaudeConvertInfo.ThinkingSignature,
						},
					})
				}
				// For tool blocks, close the last tool call block
				if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeTools &&
					info.ClaudeConvertInfo.LastToolCallIndex >= 0 {
					prevBlockIndex := info.ClaudeConvertInfo.ToolCallIndexToContentIndex[info.ClaudeConvertInfo.LastToolCallIndex]
					if stopBlock := generateStopBlock(prevBlockIndex, info); stopBlock != nil {
						claudeResponses = append(claudeResponses, stopBlock)
					}
				} else {
					if stopBlock := generateStopBlock(info.ClaudeConvertInfo.CurrentContentBlockIndex, info); stopBlock != nil {
						claudeResponses = append(claudeResponses, stopBlock)
					}
				}
				info.ClaudeConvertInfo.Index++
				info.ClaudeConvertInfo.HasTextContentStarted = false
			}
		}

		// Start text block if not started
		if !info.ClaudeConvertInfo.HasTextContentStarted {
			info.ClaudeConvertInfo.CurrentContentBlockIndex = info.ClaudeConvertInfo.Index
			info.ClaudeConvertInfo.HasTextContentStarted = true
			info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText

			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &info.ClaudeConvertInfo.Index,
				Type:  "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Type: "text",
					Text: common.GetPointer[string](""),
				},
			})
			info.ClaudeConvertInfo.HasContentBlockStarted = true
		}

		// Send text delta
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Index: &info.ClaudeConvertInfo.Index,
			Type:  "content_block_delta",
			Delta: &dto.ClaudeMediaMessage{
				Type: "text_delta",
				Text: common.GetPointer[string](textContent),
			},
		})
	}

	return claudeResponses
}

func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.ClaudeResponse {
	var stopReason string
	contents := make([]dto.ClaudeMediaMessage, 0)
	claudeResponse := &dto.ClaudeResponse{
		Id:    openAIResponse.Id,
		Type:  "message",
		Role:  "assistant",
		Model: openAIResponse.Model,
	}
	for _, choice := range openAIResponse.Choices {
		stopReason = stopReasonOpenAI2Claude(choice.FinishReason)
		// Handle reasoning_content -> Claude thinking (extended thinking passthrough)
		// OpenAI 的 reasoning_content 对应 Claude 的 thinking
		if choice.Message.ReasoningContent != "" || (choice.Message.Thinking != nil && choice.Message.Thinking.Content != "") {
			thinkingContent := dto.ClaudeMediaMessage{}
			thinkingContent.Type = "thinking"
			// 优先使用 reasoning_content，其次使用 Thinking.Content
			thinking := choice.Message.ReasoningContent
			if thinking == "" && choice.Message.Thinking != nil {
				thinking = choice.Message.Thinking.Content
			}
			thinkingContent.Thinking = &thinking
			// signature 用于多轮对话透传
			if choice.Message.Thinking != nil && choice.Message.Thinking.Signature != "" {
				signature := choice.Message.Thinking.Signature
				thinkingContent.Signature = &signature
			}
			contents = append(contents, thinkingContent)
		}
		if choice.Message.StringContent() != "" {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "text"
			claudeContent.SetText(choice.Message.StringContent())
			contents = append(contents, claudeContent)
		}
		// Handle tool calls
		toolCalls := choice.Message.ParseToolCalls()
		for _, toolUse := range toolCalls {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "tool_use"
			claudeContent.Id = toolUse.ID
			claudeContent.Name = toolUse.Function.Name
			var mapParams map[string]interface{}
			if err := common.Unmarshal([]byte(toolUse.Function.Arguments), &mapParams); err == nil {
				claudeContent.Input = mapParams
			} else {
				claudeContent.Input = toolUse.Function.Arguments
			}
			contents = append(contents, claudeContent)
		}
	}
	claudeResponse.Content = contents
	claudeResponse.StopReason = stopReason
	claudeResponse.Usage = &dto.ClaudeUsage{
		InputTokens:  openAIResponse.PromptTokens,
		OutputTokens: openAIResponse.CompletionTokens,
	}

	return claudeResponse
}

func stopReasonOpenAI2Claude(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "stop_sequence":
		return "stop_sequence"
	case "length":
		fallthrough
	case "max_tokens":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

func toJSONString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// getReasoningEffort 根据 budget_tokens 动态确定 reasoning_effort 级别
// 参考 claude-code-router 的 getThinkLevel 逻辑:
// - budget_tokens <= 0: "none" (不启用推理)
// - budget_tokens <= 1024: "low"
// - budget_tokens <= 8192: "medium"
// - budget_tokens > 8192: "high"
func getReasoningEffort(budgetTokens int) string {
	if budgetTokens <= 0 {
		return "none"
	}
	if budgetTokens <= 1024 {
		return "low"
	}
	if budgetTokens <= 8192 {
		return "medium"
	}
	return "high"
}

func GeminiToOpenAIRequest(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	openaiRequest := &dto.GeneralOpenAIRequest{
		Model:  info.UpstreamModelName,
		Stream: info.IsStream,
	}

	// 转换 messages
	var messages []dto.Message
	for _, content := range geminiRequest.Contents {
		message := dto.Message{
			Role: convertGeminiRoleToOpenAI(content.Role),
		}

		// 处理 parts
		var mediaContents []dto.MediaContent
		var toolCalls []dto.ToolCallRequest
		for _, part := range content.Parts {
			if part.Text != "" {
				mediaContent := dto.MediaContent{
					Type: "text",
					Text: part.Text,
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.InlineData != nil {
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data),
						Detail:   "auto",
						MimeType: part.InlineData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FileData != nil {
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      part.FileData.FileUri,
						Detail:   "auto",
						MimeType: part.FileData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FunctionCall != nil {
				// 处理 Gemini 的工具调用
				toolCall := dto.ToolCallRequest{
					ID:   fmt.Sprintf("call_%d", len(toolCalls)+1), // 生成唯一ID
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      part.FunctionCall.FunctionName,
						Arguments: toJSONString(part.FunctionCall.Arguments),
					},
				}
				toolCalls = append(toolCalls, toolCall)
			} else if part.FunctionResponse != nil {
				// 处理 Gemini 的工具响应，创建单独的 tool 消息
				toolMessage := dto.Message{
					Role:       "tool",
					ToolCallId: fmt.Sprintf("call_%d", len(toolCalls)), // 使用对应的调用ID
				}
				toolMessage.SetStringContent(toJSONString(part.FunctionResponse.Response))
				messages = append(messages, toolMessage)
			}
		}

		// 设置消息内容
		if len(toolCalls) > 0 {
			// 如果有工具调用，设置工具调用
			message.SetToolCalls(toolCalls)
		} else if len(mediaContents) == 1 && mediaContents[0].Type == "text" {
			// 如果只有一个文本内容，直接设置字符串
			message.Content = mediaContents[0].Text
		} else if len(mediaContents) > 0 {
			// 如果有多个内容或包含媒体，设置为数组
			message.SetMediaContent(mediaContents)
		}

		// 只有当消息有内容或工具调用时才添加
		if len(message.ParseContent()) > 0 || len(message.ToolCalls) > 0 {
			messages = append(messages, message)
		}
	}

	openaiRequest.Messages = messages

	if geminiRequest.GenerationConfig.Temperature != nil {
		openaiRequest.Temperature = geminiRequest.GenerationConfig.Temperature
	}
	if geminiRequest.GenerationConfig.TopP > 0 {
		openaiRequest.TopP = geminiRequest.GenerationConfig.TopP
	}
	if geminiRequest.GenerationConfig.TopK > 0 {
		openaiRequest.TopK = int(geminiRequest.GenerationConfig.TopK)
	}
	if geminiRequest.GenerationConfig.MaxOutputTokens > 0 {
		openaiRequest.MaxTokens = geminiRequest.GenerationConfig.MaxOutputTokens
	}
	// gemini stop sequences 最多 5 个，openai stop 最多 4 个
	if len(geminiRequest.GenerationConfig.StopSequences) > 0 {
		openaiRequest.Stop = geminiRequest.GenerationConfig.StopSequences[:4]
	}
	if geminiRequest.GenerationConfig.CandidateCount > 0 {
		openaiRequest.N = geminiRequest.GenerationConfig.CandidateCount
	}

	// 转换工具调用
	if len(geminiRequest.GetTools()) > 0 {
		var tools []dto.ToolCallRequest
		for _, tool := range geminiRequest.GetTools() {
			if tool.FunctionDeclarations != nil {
				// 将 Gemini 的 FunctionDeclarations 转换为 OpenAI 的 ToolCallRequest
				functionDeclarations, ok := tool.FunctionDeclarations.([]dto.FunctionRequest)
				if ok {
					for _, function := range functionDeclarations {
						openAITool := dto.ToolCallRequest{
							Type: "function",
							Function: dto.FunctionRequest{
								Name:        function.Name,
								Description: function.Description,
								Parameters:  function.Parameters,
							},
						}
						tools = append(tools, openAITool)
					}
				}
			}
		}
		if len(tools) > 0 {
			openaiRequest.Tools = tools
		}
	}

	// gemini system instructions
	if geminiRequest.SystemInstructions != nil {
		// 将系统指令作为第一条消息插入
		systemMessage := dto.Message{
			Role:    "system",
			Content: extractTextFromGeminiParts(geminiRequest.SystemInstructions.Parts),
		}
		openaiRequest.Messages = append([]dto.Message{systemMessage}, openaiRequest.Messages...)
	}

	return openaiRequest, nil
}

func convertGeminiRoleToOpenAI(geminiRole string) string {
	switch geminiRole {
	case "user":
		return "user"
	case "model":
		return "assistant"
	case "function":
		return "function"
	default:
		return "user"
	}
}

func extractTextFromGeminiParts(parts []dto.GeminiPart) string {
	var texts []string
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ResponseOpenAI2Gemini 将 OpenAI 响应转换为 Gemini 格式
func ResponseOpenAI2Gemini(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	geminiResponse := &dto.GeminiChatResponse{
		Candidates: make([]dto.GeminiChatCandidate, 0, len(openAIResponse.Choices)),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     openAIResponse.PromptTokens,
			CandidatesTokenCount: openAIResponse.CompletionTokens,
			TotalTokenCount:      openAIResponse.PromptTokens + openAIResponse.CompletionTokens,
		},
	}

	for _, choice := range openAIResponse.Choices {
		candidate := dto.GeminiChatCandidate{
			Index:         int64(choice.Index),
			SafetyRatings: []dto.GeminiChatSafetyRating{},
		}

		// 设置结束原因
		var finishReason string
		switch choice.FinishReason {
		case "stop":
			finishReason = "STOP"
		case "length":
			finishReason = "MAX_TOKENS"
		case "content_filter":
			finishReason = "SAFETY"
		case "tool_calls":
			finishReason = "STOP"
		default:
			finishReason = "STOP"
		}
		candidate.FinishReason = &finishReason

		// 转换消息内容
		content := dto.GeminiChatContent{
			Role:  "model",
			Parts: make([]dto.GeminiPart, 0),
		}

		// 处理工具调用
		toolCalls := choice.Message.ParseToolCalls()
		if len(toolCalls) > 0 {
			for _, toolCall := range toolCalls {
				// 解析参数
				var args map[string]interface{}
				if toolCall.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
						args = map[string]interface{}{"arguments": toolCall.Function.Arguments}
					}
				} else {
					args = make(map[string]interface{})
				}

				part := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						FunctionName: toolCall.Function.Name,
						Arguments:    args,
					},
				}
				content.Parts = append(content.Parts, part)
			}
		} else {
			// 处理文本内容
			textContent := choice.Message.StringContent()
			if textContent != "" {
				part := dto.GeminiPart{
					Text: textContent,
				}
				content.Parts = append(content.Parts, part)
			}
		}

		candidate.Content = content
		geminiResponse.Candidates = append(geminiResponse.Candidates, candidate)
	}

	return geminiResponse
}

// StreamResponseOpenAI2Gemini 将 OpenAI 流式响应转换为 Gemini 格式
func StreamResponseOpenAI2Gemini(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	// 检查是否有实际内容或结束标志
	hasContent := false
	hasFinishReason := false
	for _, choice := range openAIResponse.Choices {
		if len(choice.Delta.GetContentString()) > 0 || (choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0) {
			hasContent = true
		}
		if choice.FinishReason != nil {
			hasFinishReason = true
		}
	}

	// 如果没有实际内容且没有结束标志，跳过。主要针对 openai 流响应开头的空数据
	if !hasContent && !hasFinishReason {
		return nil
	}

	geminiResponse := &dto.GeminiChatResponse{
		Candidates: make([]dto.GeminiChatCandidate, 0, len(openAIResponse.Choices)),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     info.GetEstimatePromptTokens(),
			CandidatesTokenCount: 0, // 流式响应中可能没有完整的 usage 信息
			TotalTokenCount:      info.GetEstimatePromptTokens(),
		},
	}

	if openAIResponse.Usage != nil {
		geminiResponse.UsageMetadata.PromptTokenCount = openAIResponse.Usage.PromptTokens
		geminiResponse.UsageMetadata.CandidatesTokenCount = openAIResponse.Usage.CompletionTokens
		geminiResponse.UsageMetadata.TotalTokenCount = openAIResponse.Usage.TotalTokens
	}

	for _, choice := range openAIResponse.Choices {
		candidate := dto.GeminiChatCandidate{
			Index:         int64(choice.Index),
			SafetyRatings: []dto.GeminiChatSafetyRating{},
		}

		// 设置结束原因
		if choice.FinishReason != nil {
			var finishReason string
			switch *choice.FinishReason {
			case "stop":
				finishReason = "STOP"
			case "length":
				finishReason = "MAX_TOKENS"
			case "content_filter":
				finishReason = "SAFETY"
			case "tool_calls":
				finishReason = "STOP"
			default:
				finishReason = "STOP"
			}
			candidate.FinishReason = &finishReason
		}

		// 转换消息内容
		content := dto.GeminiChatContent{
			Role:  "model",
			Parts: make([]dto.GeminiPart, 0),
		}

		// 处理工具调用
		if choice.Delta.ToolCalls != nil {
			for _, toolCall := range choice.Delta.ToolCalls {
				// 解析参数
				var args map[string]interface{}
				if toolCall.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
						args = map[string]interface{}{"arguments": toolCall.Function.Arguments}
					}
				} else {
					args = make(map[string]interface{})
				}

				part := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						FunctionName: toolCall.Function.Name,
						Arguments:    args,
					},
				}
				content.Parts = append(content.Parts, part)
			}
		} else {
			// 处理文本内容
			textContent := choice.Delta.GetContentString()
			if textContent != "" {
				part := dto.GeminiPart{
					Text: textContent,
				}
				content.Parts = append(content.Parts, part)
			}
		}

		candidate.Content = content
		geminiResponse.Candidates = append(geminiResponse.Candidates, candidate)
	}

	return geminiResponse
}
