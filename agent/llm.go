// Package agent 双智能体自审核（docs/design-agent-review.md）。
//
// 分工：分类智能体（classifier）读元数据与原文摘要，产出效力等级/发布机关/公布日期/
// 地域区划码的结构化建议与置信度；审核智能体（auditor）比对官方原文与元数据及分类建议，
// 裁决 confirm（可自动确认）或 escalate（转人工例外队列）。
//
// 纪律（与 AGENTS.md 写路径/诚实展示纪律一致）：
//   - 默认离线：智能体通道必须由用户显式配置（环境变量）并显式触发，未配置一律拒绝任务；
//   - fail-closed：任何调用失败、输出不合法、置信度不足，一律 escalate——宁可转人工，
//     绝不误确认；
//   - 落判仍经 ingest.Confirmer 同一 runner（confirm_source=agent），每次审核无论结论
//     都写 agent_review 留痕（分类与审核的原始 JSON、置信度、模型名）；
//   - 人工只出现在例外队列：纠正动作署名「本机用户（人工纠正）」，同样落 review_log。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Chat 单轮对话的最小抽象：测试注入桩即可离线跑；生产用 OpenAIChat。
type Chat interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// 智能体通道配置（技术标识，UI 主视图不得出现变量名——收进「技术细节」折叠）。
const (
	EnvEndpoint = "OPENLAW_LLM_ENDPOINT" // OpenAI 兼容基址，如 https://api.example.com/v1
	EnvKey      = "OPENLAW_LLM_KEY"      // 访问密钥（可空：本机服务如 Ollama 常无鉴权）
	EnvModel    = "OPENLAW_LLM_MODEL"    // 模型名（必填——留痕与署名都要如实记录）
)

// Guidance 未配置时的白话指引（界面直接展示；变量名与示例由前端收进技术细节折叠）。
const Guidance = "智能审核尚未开通：需要一个能读写中文的法律大模型服务（云端或本机均可），" +
	"由您或单位的技术人员在系统中登记服务地址与密钥后即可使用。未开通前，新条目一律保持「待复核」，" +
	"程序不会猜测或自动确认任何内容。"

// Config 读取智能体通道配置；endpoint 与 model 齐备才算可用。
func Config() (endpoint, key, model string, ok bool) {
	endpoint = strings.TrimRight(strings.TrimSpace(os.Getenv(EnvEndpoint)), "/")
	key = strings.TrimSpace(os.Getenv(EnvKey))
	model = strings.TrimSpace(os.Getenv(EnvModel))
	if endpoint == "" || model == "" {
		return endpoint, key, model, false
	}
	return endpoint, key, model, true
}

// OpenAIChat OpenAI 兼容 Chat Completions 客户端（覆盖 OpenAI/DeepSeek/通义/Ollama /v1 等）。
type OpenAIChat struct {
	Endpoint string // 基址（不含 /chat/completions）
	Key      string
	Model    string
	HTTP     *http.Client
}

// NewFromEnv 按环境变量构造；未配置返回 (nil, false)——调用方必须如实拒绝任务。
func NewFromEnv() (*OpenAIChat, bool) {
	ep, key, model, ok := Config()
	if !ok {
		return nil, false
	}
	return &OpenAIChat{Endpoint: ep, Key: key, Model: model,
		HTTP: &http.Client{Timeout: 120 * time.Second}}, true
}

// Complete 单轮调用；任何非 200 / 空回复都返回错误（上层按 fail-closed 转人工）。
func (c *OpenAIChat) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":       c.Model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("智能审核服务无响应: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("智能审核服务返回异常（状态 %d）: %.200s", resp.StatusCode, raw)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("智能审核服务响应无法解析: %.200s", raw)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("智能审核服务未给出内容")
	}
	return out.Choices[0].Message.Content, nil
}
