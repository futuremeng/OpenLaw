// Package sdk OpenLaw 进程内 SDK（开发文档 5、12、15）。
// OpenCourt 等上层应用通过本包调用，禁止直连数据库、禁止让模型自行生成法条。
// 统一使用数据类返回，status 固定五种，禁止靠字符串模糊匹配判断。
package sdk

// Status 结果状态（15 章）：ok / expired / broken_chain / not_found / need_manual。
type Status string

const (
	StatusOK          = Status("ok")
	StatusExpired     = Status("expired")
	StatusBrokenChain = Status("broken_chain")
	StatusNotFound    = Status("not_found")
	StatusNeedManual  = Status("need_manual")
)

// Trace 溯源信息（5.2）。所有对外输出必须携带 doc_id，无法携带的禁止展示（7 章）。
type Trace struct {
	DocID       string `json:"doc_id"`
	PDFPath     string `json:"pdf_path"`
	ChainOK     bool   `json:"chain_ok"`
	BreakReason string `json:"break_reason,omitempty"`
}

// Result 通用返回结构（5.2、15 章）。
type Result struct {
	Status    Status         `json:"status"`
	Data      map[string]any `json:"data"`
	Trace     *Trace         `json:"trace,omitempty"`
	Warnings  []string       `json:"warnings"`
	ErrorCode string         `json:"error_code,omitempty"`
}

// 便捷构造
func ok(data map[string]any, trace *Trace) Result {
	if data == nil {
		data = map[string]any{}
	}
	return Result{Status: StatusOK, Data: data, Trace: trace, Warnings: []string{}}
}

func fail(status Status, code, reason string) Result {
	return Result{Status: status, Data: map[string]any{}, Warnings: []string{reason}, ErrorCode: code}
}
