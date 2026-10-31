// agentreview_test.go 智能审核绑定测试：统计口径（智能体/人工/历史分开）、
// 例外队列（留痕原文提取白话）、人工纠正（固定署名 + 依据必填）、
// 未配置通道必须拒绝任务（默认离线纪律）。
package main

import (
	"strings"
	"testing"
)

func seedAgent(t *testing.T, a *App) {
	t.Helper()
	seed := []string{
		`INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		 VALUES('govcn_x1','escalate',0.5,'{"effect_level":"行政法规","confidence":0.9,"reason":"名称一致"}',
		 '{"verdict":"escalate","confidence":0.5,"reason":"施行日期与原文不一致（置信度未达门槛）"}','test-model','2026-10-30T00:00:00Z')`,
		`UPDATE document SET confirm_status='confirmed', confirm_source='agent' WHERE doc_id='govcn_x0'`,
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v", err)
		}
	}
}

func TestAgentStatsSeparatesSources(t *testing.T) {
	a := newTestApp(t)
	seedAgent(t, a)
	st, err := a.AgentStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.AgentConfirmed != 1 || st.LegacyConfirm != 1 || st.HumanConfirmed != 0 {
		t.Errorf("确认来源计数应分开：agent=1 legacy=1 human=0，实际 %+v", st)
	}
	if st.Escalated != 1 || st.Unreviewed != 0 {
		t.Errorf("例外/未审计数异常: %+v", st)
	}
	if st.LastRun == "" {
		t.Error("最近审核时间应如实给出")
	}
}

func TestExceptionListAndCorrect(t *testing.T) {
	a := newTestApp(t)
	seedAgent(t, a)
	list, err := a.ExceptionList("", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DocID != "govcn_x1" {
		t.Fatalf("例外队列应只含转人工条目: %+v", list)
	}
	if !strings.Contains(list[0].Reason, "施行日期") || !strings.Contains(list[0].Suggest, "行政法规") {
		t.Errorf("例外行必须给出白话依据与分类建议: %+v", list[0])
	}
	// 缺依据必须被拒（审计纪律不因降门槛而松）
	if err := a.CorrectDoc("govcn_x1", "confirm", ""); err == nil {
		t.Error("缺结论依据应被拒绝")
	}
	if err := a.CorrectDoc("govcn_x1", "confirm", "已与官方原文逐字核对"); err != nil {
		t.Fatal(err)
	}
	var st, src, reviewer string
	if err := a.db.QueryRow(`SELECT confirm_status, COALESCE(confirm_source,'') FROM document WHERE doc_id='govcn_x1'`).Scan(&st, &src); err != nil {
		t.Fatal(err)
	}
	if st != "confirmed" || src != "human" {
		t.Errorf("人工纠正应落 confirmed/human，实际 %s/%s", st, src)
	}
	if err := a.db.QueryRow(`SELECT reviewer FROM review_log WHERE doc_id='govcn_x1' ORDER BY review_id DESC LIMIT 1`).Scan(&reviewer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewer, "本机用户") {
		t.Errorf("人工纠正必须自动署名，实际 %q", reviewer)
	}
	// 纠正后不再出现在例外队列
	list, _ = a.ExceptionList("", 50, 0)
	if len(list) != 0 {
		t.Errorf("已纠正条目应离开例外队列: %+v", list)
	}
}

func TestStartReviewTaskRequiresChannel(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("OPENLAW_LLM_ENDPOINT", "")
	t.Setenv("OPENLAW_LLM_MODEL", "")
	if _, err := a.StartReviewTask(0); err == nil {
		t.Fatal("未配置智能审核通道必须拒绝任务（默认离线纪律）")
	}
	v := a.AgentStatus()
	if v["configured"] != false {
		t.Errorf("未配置时 configured 必须如实为 false: %v", v)
	}
	if v["guidance"] == nil || v["guidance"] == "" {
		t.Error("未配置必须给出可照做的白话指引")
	}
}
