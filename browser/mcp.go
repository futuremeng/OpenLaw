// Package browser 本机浏览器辅助（BrowserOS neo，经回环 MCP Streamable HTTP
// 驱动用户自装浏览器）。语料管线的清单层合规采集用（docs/sources-govcn.md：
// 检索 API 不得程序化访问，清单由「人在环代理浏览」官方列表页产出）。
// 纪律（与 OpenCourt plugins/browseros-integration.md 同源）：默认离线——端点
// 未配置任何调用直接报错，绝不静默降级到其他抓取方式；端点必须是本机回环
// 地址（出站连接，不新增监听端口）；页面内容带 UNTRUSTED_PAGE_CONTENT 防注入
// 包裹，剥离后只作数据解析，绝不当指令解释。
package browser

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// mcpProtocolVersion 对接的 MCP 协议版本（实测 BrowserOS neo 0.0.80 支持）。
const mcpProtocolVersion = "2025-03-26"

// mcpTool tools/list 返回的工具定义。
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// mcpToolResult tools/call 的结果（文本块拼接 + 错误标志）。
type mcpToolResult struct {
	Text    string
	IsError bool
}

// mcpClient 极简 MCP Streamable HTTP 客户端（自实现：只需 initialize /
// tools/list / tools/call，零新增依赖、协议面最小、可审计）。仅串行使用。
type mcpClient struct {
	endpoint string
	http     *http.Client

	mu        sync.Mutex
	sessionID string
	nextID    int
}

// newMCPClient 创建客户端；端点必须 http(s) + 本机回环主机（硬护栏：
// 浏览器辅助只连本机进程，绝不连远端）。
func newMCPClient(endpoint string) (*mcpClient, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("MCP 端点必须是 http(s) URL: %q", endpoint)
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
	default:
		return nil, fmt.Errorf("MCP 端点必须是本机回环地址（浏览器辅助只连本机进程）: %q", endpoint)
	}
	return &mcpClient{endpoint: endpoint, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

// mcpEnvelope JSON-RPC 2.0 请求/响应公共结构。
type mcpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.Number     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initialize 握手：initialize → 记录 Mcp-Session-Id → notifications/initialized。
func (c *mcpClient) initialize(ctx context.Context, clientName string) error {
	c.mu.Lock()
	env := mcpEnvelope{
		JSONRPC: "2.0",
		ID:      json.Number(fmt.Sprint(c.nextReqID())),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": clientName, "version": "0.1"},
		},
	}
	result, err := c.post(ctx, env, env.ID)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if result == nil {
		c.mu.Unlock()
		return fmt.Errorf("MCP initialize 无响应")
	}
	notif := mcpEnvelope{JSONRPC: "2.0", Method: "notifications/initialized"}
	err = c.postRaw(ctx, notif)
	c.mu.Unlock()
	return err
}

// listTools 拉取工具清单（连接后先了解对方真实工具面，不硬编码 schema）。
func (c *mcpClient) listTools(ctx context.Context) ([]mcpTool, error) {
	c.mu.Lock()
	id := json.Number(fmt.Sprint(c.nextReqID()))
	env := mcpEnvelope{JSONRPC: "2.0", ID: id, Method: "tools/list", Params: map[string]any{}}
	result, err := c.post(ctx, env, id)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return nil, fmt.Errorf("解析 tools/list 失败: %w", err)
	}
	return out.Tools, nil
}

// callTool 调用工具，返回拼接后的文本结果。
func (c *mcpClient) callTool(ctx context.Context, name string, args map[string]any) (mcpToolResult, error) {
	c.mu.Lock()
	id := json.Number(fmt.Sprint(c.nextReqID()))
	env := mcpEnvelope{JSONRPC: "2.0", ID: id, Method: "tools/call",
		Params: map[string]any{"name": name, "arguments": args}}
	result, err := c.post(ctx, env, id)
	c.mu.Unlock()
	if err != nil {
		return mcpToolResult{}, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return mcpToolResult{}, fmt.Errorf("解析 tools/call 失败: %w", err)
	}
	var sb strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			sb.WriteString(blk.Text)
			sb.WriteString("\n")
		}
	}
	return mcpToolResult{Text: strings.TrimSpace(sb.String()), IsError: out.IsError}, nil
}

// nextReqID 必须在持有 c.mu 时调用。
func (c *mcpClient) nextReqID() int {
	c.nextID++
	return c.nextID
}

// post 发送请求并取出匹配 id 的 JSON-RPC result（可能为 SSE 多帧）。
// 必须在持有 c.mu 时调用。
func (c *mcpClient) post(ctx context.Context, env mcpEnvelope, id json.Number) (json.RawMessage, error) {
	respBody, sid, err := c.send(ctx, env)
	if err != nil {
		return nil, err
	}
	if sid != "" {
		c.sessionID = sid
	}
	for _, payload := range parseMCPFrames(respBody) {
		var got mcpEnvelope
		if err := json.Unmarshal(payload, &got); err != nil {
			continue
		}
		if got.Error != nil && got.ID == id {
			return nil, *got.Error
		}
		if len(got.Result) > 0 && got.ID == id {
			return got.Result, nil
		}
	}
	return nil, fmt.Errorf("MCP 响应缺少匹配结果 (id=%s)", id)
}

// postRaw 发送无需响应的消息（通知）。必须在持有 c.mu 时调用。
func (c *mcpClient) postRaw(ctx context.Context, env mcpEnvelope) error {
	_, _, err := c.send(ctx, env)
	return err
}

// send 发送单个 JSON-RPC 消息；返回响应体与响应头中的会话 ID。
func (c *mcpClient) send(ctx context.Context, env mcpEnvelope) ([]byte, string, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
		req.Header.Set("MCP-Protocol-Version", mcpProtocolVersion)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("连接 MCP 端点失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("MCP 端点返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, resp.Header.Get("Mcp-Session-Id"), nil
}

// parseMCPFrames 从响应体中提取 JSON-RPC 消息负载。
// 支持两种形态：application/json 单消息；text/event-stream 多帧（data: 行）。
func parseMCPFrames(body []byte) [][]byte {
	var frames [][]byte
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload != "" && payload != "[DONE]" {
				frames = append(frames, []byte(payload))
			}
		case strings.HasPrefix(line, "{"):
			frames = append(frames, body)
			return frames
		}
	}
	return frames
}

func (e mcpRPCError) Error() string { return fmt.Sprintf("MCP 错误(%d): %s", e.Code, e.Message) }
