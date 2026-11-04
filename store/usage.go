// usage.go 本机使用事件台账（schema v10 usage_event 的唯一写入口）。
//
// 为什么要记：产品需要数字来排优先级（首启是否走得完、检索是否经常空手而归、
// 例外队列积压多久），而这些数字在「默认离线」前提下只能在本机算。
// 隐私边界（不可放宽）：只记 kind/outcome/detail 三个短字段——
// **不记检索词、不记法名、不记案件内容、不记原文**；没有任何上传路径（本仓库不联网）。
// 台账写入失败绝不影响主流程（度量是辅助，不能变成故障面）。
package store

import (
	"errors"
	"strings"
	"time"
)

// 事件种类（界面与报表据此聚合；新增种类必须同时扩展 metrics 聚合口径）。
const (
	EventAppOpen     = "app_open"     // 应用启动
	EventSetupRoute  = "setup_route"  // 初始化选择了哪条路线（outcome=pack|collect）
	EventPackInstall = "pack_install" // 数据包安装（outcome=ok|partial|fail）
	EventSearch      = "search"       // 条文检索（outcome=ok|zero|fail，不含检索词）
	EventVerify      = "verify"       // 引用核验（outcome=结论，不含引用内容）
	EventReviewRun   = "review_run"   // 智能审核一轮（outcome=ok|fail）
	EventFetch       = "fetch"        // 从官方渠道获取一轮（outcome=ok|partial|fail）
)

// UsageLog 记一条本机事件。detail 必须是白话结论，不得包含检索词或案件内容。
func UsageLog(db *DB, kind, outcome, detail string) error {
	if db == nil {
		return errors.New("数据库未打开")
	}
	if strings.TrimSpace(kind) == "" {
		return errors.New("事件种类不能为空")
	}
	if len(detail) > 200 { // 细节只做线索，长文本属于误用（很可能塞进了内容）
		detail = detail[:200]
	}
	_, err := db.Exec(`INSERT INTO usage_event(kind, outcome, detail, created_at) VALUES(?,?,?,?)`,
		kind, outcome, detail, time.Now().UTC().Format(time.RFC3339))
	return err
}

// UsageAvailable 本机是否已有事件台账表（v9 及更早语料没有）。
// 度量层据此区分「尚未记录」与「记录为 0」——把没记录渲染成 0% 是造假。
func UsageAvailable(db *DB) bool {
	var n int
	if db == nil {
		return false
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='usage_event'`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
