// confirm.go 测试：统计、风险抽样、清单落判、条件批量、审计留痕、溯源同步。
package ingest

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupConfirm(t *testing.T) (*Confirmer, string) {
	t.Helper()
	im, dir := setupImporter(t)
	mk := func(docID, lawID, name string, whole bool, pub string, inferred bool) {
		t.Helper()
		src := filepath.Join(t.TempDir(), docID+".docx")
		body := `<w:p><w:r><w:t>第一条　为测试，制定本条例。</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>第二条 本条例自2020年1月1日起施行。</w:t></w:r></w:p>`
		if whole {
			body = `<w:p><w:r><w:t>关于废止若干法规的决定。本决定自公布之日起施行。</w:t></w:r></w:p>`
		}
		writeTestDocx(t, src, `<w:document><w:body>`+body+`</w:body></w:document>`)
		date := docID[len(docID)-8:]
		ymd := date[:4] + "-" + date[4:6] + "-" + date[6:]
		_, err := im.Import(Request{
			DocID: docID, LawID: lawID, LawName: name,
			VersionNo: ymd, EffectiveDate: ymd, PublishDate: pub,
			SourceType: "flk 批量下载（用户提供存量）", PDFPath: src,
			WholeTextMode: whole, EffectiveDateInferred: inferred,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mk("flk_a_20200101", "flk_a", "甲条例", false, "2020-01-01", false)
	mk("flk_b_20200102", "flk_b", "乙决定", true, "", false)
	mk("flk_c_20200103", "flk_c", "丙条例", false, "2020-01-03", true)
	return &Confirmer{DataDir: dir, DB: im.DB}, dir
}

func TestConfirmStatsSampleApply(t *testing.T) {
	c, dir := setupConfirm(t)

	st, err := c.Stats(nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 || st.Pending != 3 || st.Confirmed != 0 {
		t.Fatalf("统计异常: %+v", st)
	}
	if st.Risk.WholeText != 1 || st.Risk.NoPublishDate != 1 || st.Risk.InferredDate != 1 {
		t.Fatalf("风险面统计异常: %+v", st.Risk)
	}

	// 抽样：高风险优先，清单可直接回填裁决列
	out := filepath.Join(t.TempDir(), "queue.csv")
	rs := RiskSet{"flk_c_20200103": {"inferred_date"}}
	n, err := c.Sample(out, 10, rs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("应入列 3，实际 %d", n)
	}
	f, _ := os.Open(out)
	recs, _ := csv.NewReader(f).ReadAll()
	f.Close()
	if len(recs) != 4 {
		t.Fatalf("清单应 1 表头 + 3 行，实际 %d", len(recs))
	}
	hdr := map[string]int{}
	for i, h := range recs[0] {
		hdr[h] = i
	}
	if recs[1][hdr["doc_id"]] != "flk_b_20200102" && recs[1][hdr["doc_id"]] != "flk_c_20200103" {
		t.Fatalf("高风险应排在最前，实际第一行 %s", recs[1][hdr["doc_id"]])
	}
	if !strings.Contains(recs[1][hdr["risk_flags"]], "whole_text|no_publish_date") &&
		!strings.Contains(recs[2][hdr["risk_flags"]], "whole_text|no_publish_date") {
		t.Fatalf("乙决定应带 whole_text|no_publish_date 标记: %s / %s", recs[1][hdr["risk_flags"]], recs[2][hdr["risk_flags"]])
	}

	// 落判：确认/驳回/不存在/无署名/未裁决
	in := filepath.Join(t.TempDir(), "decisions.csv")
	os.WriteFile(in, []byte("doc_id,verdict,reviewer,note\n"+
		"flk_a_20200101,confirm,张三,比对 flk 在线页一致\n"+
		"flk_b_20200102,reject,李四,文本截断\n"+
		"flk_x_99999999,confirm,张三,不存在\n"+
		"flk_c_20200103,confirm,,无署名必须失败\n"+
		",,,未裁决行跳过\n"), 0o644)
	sum, err := c.Apply(in)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Confirmed != 1 || sum.Rejected != 1 || sum.Skipped != 1 || len(sum.Failures) != 2 {
		t.Fatalf("落判统计异常: %+v", sum)
	}
	var s string
	if err := c.DB.QueryRow(`SELECT confirm_status FROM document WHERE doc_id='flk_a_20200101'`).Scan(&s); err != nil || s != "confirmed" {
		t.Fatalf("甲应为 confirmed，实际 %q (%v)", s, err)
	}
	if err := c.DB.QueryRow(`SELECT confirm_status FROM document WHERE doc_id='flk_c_20200103'`).Scan(&s); err != nil || s != "pending" {
		t.Fatalf("丙应保持 pending，实际 %q (%v)", s, err)
	}
	var reviewer, note string
	if err := c.DB.QueryRow(`SELECT reviewer, reason FROM review_log WHERE doc_id='flk_a_20200101'`).Scan(&reviewer, &note); err != nil ||
		reviewer != "张三" || !strings.Contains(note, "flk") {
		t.Fatalf("审计缺失或不完整: %q %q (%v)", reviewer, note, err)
	}
	// 溯源 JSON 与库同步
	raw, err := os.ReadFile(filepath.Join(dir, "provenance", "flk_a_20200101.json"))
	if err != nil {
		t.Fatal(err)
	}
	var prov map[string]any
	if err := json.Unmarshal(raw, &prov); err != nil || prov["confirm_status"] != "confirmed" {
		t.Fatalf("溯源 JSON confirm_status 应同步为 confirmed: %v (%v)", prov["confirm_status"], err)
	}
}

func TestConfirmBulk(t *testing.T) {
	c, _ := setupConfirm(t)
	n, err := c.BulkMatch(BulkFilter{SourceLike: "flk%"}, "confirm")
	if err != nil || n != 3 {
		t.Fatalf("dry-run 应命中 3，实际 %d (%v)", n, err)
	}
	if _, err := c.Bulk(BulkFilter{SourceLike: "flk%"}, "confirm", "", ""); err == nil {
		t.Fatal("批量确认必须署名+依据")
	}
	sum, err := c.Bulk(BulkFilter{SourceLike: "flk%"}, "confirm", "王五", "整批经 flk 在线页逐条比对")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Confirmed != 3 || len(sum.Failures) != 0 {
		t.Fatalf("批量统计异常: %+v", sum)
	}
	// 幂等重跑：confirm 只命中 pending，已全部 confirmed → 命中 0，绝不重复流转
	if m, err := c.BulkMatch(BulkFilter{SourceLike: "flk%"}, "confirm"); err != nil || m != 0 {
		t.Fatalf("重跑 dry-run 应命中 0，实际 %d (%v)", m, err)
	}
	sum2, err := c.Bulk(BulkFilter{SourceLike: "flk%"}, "confirm", "王五", "重跑")
	if err != nil || sum2.Skipped != 0 || sum2.Confirmed != 0 {
		t.Fatalf("重跑应零流转: %+v (%v)", sum2, err)
	}
	var cnt int
	if err := c.DB.QueryRow(`SELECT COUNT(*) FROM review_log WHERE reviewer='王五' AND reason LIKE '%整批经 flk%'`).Scan(&cnt); err != nil || cnt != 3 {
		t.Fatalf("批量审计应 3 条留痕，实际 %d (%v)", cnt, err)
	}
	// reset 回流
	if _, err := c.Bulk(BulkFilter{SourceLike: "flk%"}, "reset", "王五", "复核发现误确认"); err != nil {
		t.Fatal(err)
	}
	if err := c.DB.QueryRow(`SELECT COUNT(*) FROM document WHERE confirm_status='pending'`).Scan(&cnt); err != nil || cnt != 3 {
		t.Fatalf("reset 应全部回到 pending，实际 %d (%v)", cnt, err)
	}
}
