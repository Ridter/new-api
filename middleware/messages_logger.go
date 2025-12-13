package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// MessageLogEntry 表示一条消息日志记录
type MessageLogEntry struct {
	Timestamp   string          `json:"timestamp"`
	RequestID   string          `json:"request_id"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response,omitempty"`
	RawResponse string          `json:"raw_response,omitempty"`
}

var (
	messagesLogDir     = "./data/messages"
	messagesLogEnabled = false
)

func init() {
	// 支持通过环境变量配置日志目录
	if dir := os.Getenv("MESSAGES_LOG_DIR"); dir != "" {
		messagesLogDir = dir
	}
	// 通过环境变量开启消息日志记录
	if enabled := os.Getenv("MESSAGES_LOG_ENABLED"); enabled == "true" || enabled == "1" {
		messagesLogEnabled = true
	}
}

// InitMessagesLogger 初始化消息日志记录器
func InitMessagesLogger() error {
	if err := os.MkdirAll(messagesLogDir, 0755); err != nil {
		return err
	}
	return nil
}

// CloseMessagesLogger 关闭消息日志（每个请求单独文件，无需关闭）
func CloseMessagesLogger() {
	// 每个请求单独文件，无需关闭
}

// loggingResponseWriter 包装 gin.ResponseWriter 以捕获响应体
// 使用互斥锁确保并发安全
type loggingResponseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
	mu   sync.Mutex
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	w.body.Write(b)
	w.mu.Unlock()
	return w.ResponseWriter.Write(b)
}

func (w *loggingResponseWriter) WriteString(s string) (int, error) {
	w.mu.Lock()
	w.body.WriteString(s)
	w.mu.Unlock()
	return w.ResponseWriter.WriteString(s)
}

// WriteHeader 捕获状态码
func (w *loggingResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
}

// WriteHeaderNow 实现 gin.ResponseWriter 接口
func (w *loggingResponseWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

// Status 实现 gin.ResponseWriter 接口
func (w *loggingResponseWriter) Status() int {
	return w.ResponseWriter.Status()
}

// Size 实现 gin.ResponseWriter 接口
func (w *loggingResponseWriter) Size() int {
	return w.ResponseWriter.Size()
}

// Written 实现 gin.ResponseWriter 接口
func (w *loggingResponseWriter) Written() bool {
	return w.ResponseWriter.Written()
}

// Flush 实现 http.Flusher 接口
func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 实现 http.Hijacker 接口
func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("ResponseWriter does not implement http.Hijacker")
}

// CloseNotify 实现 http.CloseNotifier 接口（已废弃但仍需实现）
func (w *loggingResponseWriter) CloseNotify() <-chan bool {
	if cn, ok := w.ResponseWriter.(http.CloseNotifier); ok {
		return cn.CloseNotify()
	}
	return nil
}

// Pusher 实现 http.Pusher 接口
func (w *loggingResponseWriter) Pusher() http.Pusher {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher
	}
	return nil
}

// getBody 线程安全地获取响应体副本
func (w *loggingResponseWriter) getBody() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Bytes()
}

// MessagesLogger 中间件记录 /v1/messages 请求的请求和响应
func MessagesLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否启用消息日志记录
		if !messagesLogEnabled {
			c.Next()
			return
		}

		// 读取请求体
		requestBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		// 恢复请求体以供后续处理
		c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))

		// 包装 ResponseWriter 以捕获响应
		rw := &loggingResponseWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
		}
		c.Writer = rw

		// 获取请求 ID（在处理前获取，确保可用）
		requestID := c.GetString(common.RequestIdKey)

		// 使用 defer 确保在响应完全结束后记录
		defer func() {
			// 线程安全地获取响应体
			responseBody := rw.getBody()

			// 构建日志条目
			var logEntry MessageLogEntry
			logEntry.Timestamp = time.Now().Format(time.RFC3339)
			logEntry.RequestID = requestID
			logEntry.Request = json.RawMessage(requestBody)

			// 判断响应是否为有效 JSON
			if json.Valid(responseBody) {
				logEntry.Response = json.RawMessage(responseBody)
			} else {
				// 非 JSON 响应（如 SSE 流式响应），作为原始字符串存储
				logEntry.RawResponse = string(responseBody)
			}

			// 异步写入日志文件
			go writeMessageLog(logEntry)
		}()

		// 处理请求
		c.Next()
	}
}

// writeMessageLog 将日志条目写入单独的文件
func writeMessageLog(entry MessageLogEntry) {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}

	// 生成文件名：时间戳_请求ID.json
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	requestID := entry.RequestID
	if requestID == "" {
		requestID = "unknown"
	}
	filename := fmt.Sprintf("%s/%s_%s.json", messagesLogDir, timestamp, requestID)

	_ = os.WriteFile(filename, data, 0644)
}

// SetMessagesLogDir 设置日志目录路径（用于配置）
func SetMessagesLogDir(dir string) {
	messagesLogDir = dir
}
