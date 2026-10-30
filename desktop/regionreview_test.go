// 「地域标引」复核面板绑定测试：队列排序、状态口径、署名强制、改选、驳回留痕、
// 批量先干跑后执行且预览数不符即中止。全部离线临时语料。
package main

import (
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
)

// newRegionReviewApp 临时库 + 各类地域判定情形，并先跑一次机器写入。
func newRegionReviewApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	seed := []string{
		`INSERT INTO law(law_id,name,issuer,effect_level) VALUES('RR1','北京市河湖管理条例','北京市人大常委会','地方性法规')`,
		`INSERT INTO law(law_id,name,issuer,effect_level) VALUES('RR2','劳动和社会保障监察条例','石家庄市人力资源和社会保障局','地方性法规')`, // 仅机关命中：中置信
		`INSERT INTO law(law_id,name,issuer,effect_level) VALUES('RR3','中华人民共和国劳动法','全国人民代表大会常务委员会','法律')`,      // 国家级效力：不标
		`INSERT INTO law(law_id,name,issuer,effect_level) VALUES('RR4','石家庄市水污染防治条例','石家庄市人大常委会','地方性法规')`,
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v", err)
		}
	}
	m := &ingest.RegionMarker{DB: a.db}
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var high []ingest.RegionCandidate
	for _, c := range cands {
		if c.Confidence == ingest.RegionConfHigh {
			high = append(high, c)
		}
	}
	if _, n, err := m.Apply(high); err != nil || n != 2 {
		t.Fatalf("机器写入异常: n=%d err=%v", n, err)
	}
	return a
}

func TestRegionReviewQueue(t *testing.T) {
	a := newRegionReviewApp(t)
	st, err := a.RegionReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.SuggestPending != 2 || st.Confirmed != 0 || st.LeftBlank != 0 {
		t.Errorf("统计不符: %+v", st)
	}
	rows, err := a.RegionReviewList("", "suggest", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("机器建议队列应 2 条，实际 %d", len(rows))
	}
	for _, r := range rows {
		if r.State != "suggest" || r.Code == "" || r.RegionPath == "" {
			t.Errorf("机器建议行状态/地区不符: %+v", r)
		}
	}
	// 默认队列把机器建议排在未标注之前
	all, err := a.RegionReviewList("", "", 50, 0)
	if err != nil || len(all) == 0 {
		t.Fatalf("队列读取失败: %v", err)
	}
	if all[0].State != "suggest" {
		t.Errorf("首行应为机器建议待复核，实际 %q", all[0].State)
	}
	// 未标注行必须如实说明「为什么没写」
	d, err := a.RegionReviewDetail("RR2")
	if err != nil {
		t.Fatal(err)
	}
	if d.Row.State != "unmarked" || d.Row.Code != "" || d.Suggest.Confidence != "medium" || d.Suggest.Note == "" {
		t.Errorf("中置信未标注行说明缺失: %+v", d)
	}
	if len(d.Log) != 0 {
		t.Errorf("尚无复核记录时不应有记录: %v", d.Log)
	}
}

func TestRegionReviewJudge(t *testing.T) {
	a := newRegionReviewApp(t)
	// 无署名/无依据：拒绝
	if _, err := a.JudgeRegion([]string{"RR1"}, "110000", "confirm", "", "依据"); err == nil {
		t.Error("缺复核人仍落判成功")
	}
	if _, err := a.JudgeRegion([]string{"RR1"}, "110000", "confirm", "李四", ""); err == nil {
		t.Error("缺依据仍落判成功")
	}
	if _, err := a.JudgeRegion(nil, "110000", "confirm", "李四", "依据"); err == nil {
		t.Error("未选条目仍落判成功")
	}
	// 确认（沿用机器建议）
	sum, err := a.JudgeRegion([]string{"RR1"}, "110000", "confirm", "李四", "名称与发文机关一致")
	if err != nil || sum.Confirmed != 1 {
		t.Fatalf("确认失败: %v %+v", err, sum)
	}
	d, err := a.RegionReviewDetail("RR1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Row.State != "reviewed" || d.Row.Reviewer != "李四" {
		t.Errorf("确认后状态不符: %+v", d.Row)
	}
	if len(d.Log) < 2 {
		t.Errorf("复核记录应含机器建议与确认，实际 %d 条", len(d.Log))
	}
	// 改选地区
	if _, err := a.JudgeRegion([]string{"RR4"}, "130000", "confirm", "王五", "实际属地河北省"); err != nil {
		t.Fatal(err)
	}
	if d, _ = a.RegionReviewDetail("RR4"); d.Row.Code != "130000" {
		t.Errorf("改选未生效: %q", d.Row.Code)
	}
	// 驳回：留空但已复核
	if _, err := a.JudgeRegion([]string{"RR1"}, "", "reject", "王五", "名称前缀不可信"); err != nil {
		t.Fatal(err)
	}
	if d, _ = a.RegionReviewDetail("RR1"); d.Row.Code != "" || d.Row.State != "reviewed" {
		t.Errorf("驳回后应留空且已复核: %+v", d.Row)
	}
	st, _ := a.RegionReviewStats()
	if st.LeftBlank != 1 {
		t.Errorf("LeftBlank=%d，应为 1", st.LeftBlank)
	}
}

func TestRegionReviewBulk(t *testing.T) {
	a := newRegionReviewApp(t)
	n, err := a.RegionBulkPreview("", "confirm")
	if err != nil || n != 2 {
		t.Fatalf("干跑计数不符: n=%d err=%v", n, err)
	}
	// 预览数与实际不符：中止
	if _, err := a.RegionBulkApply("", "confirm", "李四", "批量确认", 99); err == nil {
		t.Error("预览数不符仍执行")
	}
	// 无署名：拒绝
	if _, err := a.RegionBulkApply("", "confirm", "", "依据", 2); err == nil {
		t.Error("批量缺复核人仍执行")
	}
	// 按省缩小范围：只确认河北省的机器建议
	if _, err := a.RegionBulkApply("13", "confirm", "李四", "批量确认", 1); err != nil {
		t.Fatalf("按省批量失败: %v", err)
	}
	var code, reviewer string
	if err := a.db.QueryRow(`SELECT region_code, region_reviewer FROM law WHERE law_id='RR4'`).
		Scan(&code, &reviewer); err != nil {
		t.Fatal(err)
	}
	if code != "130100" || reviewer != "李四" {
		t.Errorf("批量确认结果不符: code=%s reviewer=%s", code, reviewer)
	}
	// 已复核行不再进入批量范围
	if n, _ = a.RegionBulkPreview("13", "confirm"); n != 0 {
		t.Errorf("已复核行仍被批量匹配: %d", n)
	}
	// 清除标注批量
	if n, _ = a.RegionBulkPreview("", "clear"); n != 1 {
		t.Errorf("clear 干跑应为 1，实际 %d", n)
	}
	if _, err := a.RegionBulkApply("", "clear", "李四", "试清除", n); err != nil {
		t.Fatalf("clear 批量失败: %v", err)
	}
	if err := a.db.QueryRow(`SELECT COALESCE(region_code,'') FROM law WHERE law_id='RR1'`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "" {
		t.Errorf("clear 后应留空，实际 %q", code)
	}
}

// 空署名不得显示成已复核：region_reviewer 空串与 NULL 同义，队列与状态口径都必须
// 把它留在「机器建议 · 待复核」，不得渲染成已人工复核。
func TestRegionReviewBlankReviewerNotReviewed(t *testing.T) {
	a := newRegionReviewApp(t)
	if _, err := a.db.Exec(`INSERT INTO law(law_id,name,region_code,region_source,region_reviewer)
		VALUES('RR5','天津市河道管理条例','120000','auto','')`); err != nil {
		t.Fatal(err)
	}
	rows, err := a.RegionReviewList("", "suggest", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rows {
		if r.LawID != "RR5" {
			continue
		}
		found = true
		if r.State != "suggest" {
			t.Errorf("空署名行状态应为 suggest，实际 %q", r.State)
		}
	}
	if !found {
		t.Error("空署名行必须留在待复核队列")
	}
	reviewed, err := a.RegionReviewList("", "reviewed", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reviewed {
		if r.LawID == "RR5" {
			t.Error("空署名行不得出现在已复核队列")
		}
	}
	st, err := a.RegionReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Confirmed != 0 {
		t.Errorf("空署名行不得计入已人工确认，实际 %d", st.Confirmed)
	}
}
